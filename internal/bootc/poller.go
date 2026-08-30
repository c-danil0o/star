package bootc

import (
	"context"
	"log"
	"os"
	"time"

	magnetarapi "github.com/c12s/magnetar/pkg/api"
)

type StatusCallback func(phase string, message string)

type Poller struct {
	interval      time.Duration
	desired       *DesiredStore
	attempts      *AttemptStore
	statusHandler StatusCallback
}

func NewPoller(interval time.Duration, stateDir string, statusHandler StatusCallback) *Poller {
	return &Poller{
		interval:      interval,
		desired:       NewDesiredStore(stateDir),
		attempts:      NewAttemptStore(stateDir),
		statusHandler: statusHandler,
	}
}

func (p *Poller) Desired() *DesiredStore {
	return p.desired
}

func (p *Poller) Attempts() *AttemptStore {
	return p.attempts
}

func (p *Poller) SetStatusHandler(handler StatusCallback) {
	p.statusHandler = handler
}

func (p *Poller) report(phase string, message string) {
	if p.statusHandler != nil {
		p.statusHandler(phase, message)
	} else {
		log.Printf("os status [%s]: %s", phase, message)
	}
}

func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	p.reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.reconcile()
		}
	}
}

func (p *Poller) reconcile() {
	desired := p.desired.Get()
	if desired == nil {
		return
	}
	status := ProbeStatus()
	if status == nil {
		p.report(magnetarapi.OsPhaseDegraded, "bootc status unavailable")
		return
	}
	if status.Digest == desired.Digest {
		last := p.attempts.Last()
		if last != nil && last.Digest == desired.Digest && last.Result != magnetarapi.OsAttemptApplied {
			_ = p.attempts.Clear()
		}
		p.report(magnetarapi.OsPhaseHealthy, "booted image matches desired state")
		return
	}
	last := p.attempts.Last()
	if last != nil && last.Digest == desired.Digest {
		if last.Result == magnetarapi.OsAttemptRolledBack {
			p.report(magnetarapi.OsPhaseRolledBack, "previous attempt for desired digest was rolled back, waiting for new desired image")
			return
		}
		if last.Result == magnetarapi.OsAttemptApplied {
			// The switch was performed and the node rebooted, but it came
			// back on the old image: the health gate (greenboot) rejected
			// the new deployment and rolled back. Park instead of looping.
			_ = p.attempts.Record(Attempt{
				Image:     desired.ImageRef,
				Digest:    desired.Digest,
				Result:    magnetarapi.OsAttemptRolledBack,
				Timestamp: time.Now().Unix(),
			})
			p.report(magnetarapi.OsPhaseRolledBack, "greenboot rolled back the desired digest, waiting for new desired image")
			return
		}
	}
	if last != nil && last.Digest == desired.Digest && last.Result == magnetarapi.OsAttemptFailed {
		_ = p.attempts.Clear()
	}
	if err := Verify(desired.ImageRef, desired.Digest); err != nil {
		_ = p.attempts.Record(Attempt{
			Image:     desired.ImageRef,
			Digest:    desired.Digest,
			Result:    magnetarapi.OsAttemptFailed,
			Timestamp: time.Now().Unix(),
		})
		p.report(magnetarapi.OsPhaseDegraded, "verification failed: "+err.Error())
		return
	}
	p.report(magnetarapi.OsPhaseSwitching, "switching to "+desired.ImageRef+"@"+desired.Digest)
	if err := Switch(desired.ImageRef, desired.Digest); err != nil {
		_ = p.attempts.Record(Attempt{
			Image:     desired.ImageRef,
			Digest:    desired.Digest,
			Result:    magnetarapi.OsAttemptFailed,
			Timestamp: time.Now().Unix(),
		})
		p.report(magnetarapi.OsPhaseDegraded, err.Error())
		return
	}
	// record before rebooting (see Switch doc): this is what lets the next
	// boot tell "we switched and came back on the old image" (rollback)
	// apart from "we never switched"
	_ = p.attempts.Record(Attempt{
		Image:     desired.ImageRef,
		Digest:    desired.Digest,
		Result:    magnetarapi.OsAttemptApplied,
		Timestamp: time.Now().Unix(),
	})
	if desired.RebootPolicy != "" && desired.RebootPolicy != "immediate" {
		p.report(magnetarapi.OsPhaseDegraded, "staged switch, reboot policy "+desired.RebootPolicy+" not supported yet")
		return
	}
	if err := Reboot(); err != nil {
		p.report(magnetarapi.OsPhaseDegraded, "reboot after switch failed: "+err.Error())
	}
}

func StateDir() string {
	dir := os.Getenv("NODE_ID_DIR_PATH")
	if dir == "" {
		dir = "/var/lib/c12s"
	}
	return dir
}
