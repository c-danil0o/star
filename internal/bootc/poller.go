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
	bootID := CurrentBootID()
	last := p.attempts.Last()

	if status.Digest == desired.Digest {
		p.reconcileBooted(desired, last, bootID)
		return
	}

	if last != nil && last.Digest == desired.Digest {
		switch last.Result {
		case magnetarapi.OsAttemptApplied:
			if last.BootID != "" && bootID == last.BootID {
				// The deployment is staged but this process still runs on
				// the pre-switch boot: the reboot has not landed yet. Do
				// not infer a rollback from the old digest here (that would
				// poison the attempt) and do not switch again.
				p.report(magnetarapi.OsPhaseSwitching, "deployment staged, waiting for reboot")
				return
			}
			if last.PreviousDigest == "" || status.Digest == last.PreviousDigest {
				// The switch was performed and the node rebooted, but it
				// came back on the pre-switch image: the health gate
				// (greenboot) rejected the new deployment and rolled back.
				// Park instead of looping.
				_ = p.attempts.Record(Attempt{
					Image:          desired.ImageRef,
					Digest:         desired.Digest,
					PreviousDigest: last.PreviousDigest,
					Result:         magnetarapi.OsAttemptRolledBack,
					Timestamp:      time.Now().Unix(),
					BootID:         last.BootID,
				})
				p.report(magnetarapi.OsPhaseRolledBack, "greenboot rolled back the desired digest, waiting for new desired image")
				return
			}
			// The node came back on some *other* image: an out-of-band
			// change (manual bootc switch), not a greenboot rollback. The
			// declared state wins — fall through and switch back.
			p.report(magnetarapi.OsPhaseDegraded, "drift detected: booted "+status.Digest+", reconciling to desired image")
			_ = p.attempts.Clear()
		case magnetarapi.OsAttemptRolledBack:
			p.report(magnetarapi.OsPhaseRolledBack, "previous attempt for desired digest was rolled back, waiting for new desired image")
			return
		case magnetarapi.OsAttemptFailed:
			_ = p.attempts.Clear()
		}
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
	RepairStaleBootState()
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
	// apart from "we never switched" (and from drift to a third image).
	// BootID pins the record to the boot that staged the deployment.
	_ = p.attempts.Record(Attempt{
		Image:          desired.ImageRef,
		Digest:         desired.Digest,
		PreviousDigest: status.Digest,
		Result:         magnetarapi.OsAttemptApplied,
		Timestamp:      time.Now().Unix(),
		BootID:         bootID,
	})
	if desired.RebootPolicy != "" && desired.RebootPolicy != "immediate" {
		p.report(magnetarapi.OsPhaseDegraded, "staged switch, reboot policy "+desired.RebootPolicy+" not supported yet")
		return
	}
	if err := Reboot(); err != nil {
		p.report(magnetarapi.OsPhaseDegraded, "reboot after switch failed: "+err.Error())
	}
}

// reconcileBooted handles the "booted digest matches the desired digest"
// case. Digest equality alone is not proof of health: a deployment that just
// passed greenboot's gate reports healthy, while a deployment whose gate has
// not run yet (or has failed) stays in switching until greenboot accepts it
// or rolls it back.
func (p *Poller) reconcileBooted(desired *DesiredImage, last *Attempt, bootID string) {
	if last != nil && last.Digest == desired.Digest {
		if last.Result == magnetarapi.OsAttemptApplied {
			rebooted := last.BootID == "" || bootID != last.BootID
			if rebooted && GreenbootPresent() {
				if value, ok := GrubEnvValue("boot_success"); ok && value != "1" {
					p.report(magnetarapi.OsPhaseSwitching, "booted desired image, waiting for greenboot health gate")
					return
				}
			}
		}
		_ = p.attempts.Clear()
	}
	p.report(magnetarapi.OsPhaseHealthy, "booted image matches desired state")
}

func StateDir() string {
	dir := os.Getenv("NODE_ID_DIR_PATH")
	if dir == "" {
		dir = "/var/lib/c12s"
	}
	return dir
}
