package bootc

import (
	"log"
	"time"

	magnetarapi "github.com/c12s/magnetar/pkg/api"
)

const reportTimeout = 30 * time.Second

type Reporter struct {
	client    *magnetarapi.NodeUpdateAsyncClient
	nodeId    string
	attempts  *AttemptStore
	onUnknown func()
}

func NewReporter(client *magnetarapi.NodeUpdateAsyncClient, nodeId string, attempts *AttemptStore) *Reporter {
	return &Reporter{
		client:   client,
		nodeId:   nodeId,
		attempts: attempts,
	}
}

// SetUnknownNodeHandler is called when Magnetar answers that it has no record
// of this node (e.g. the control plane was reset while the node kept running).
func (r *Reporter) SetUnknownNodeHandler(handler func()) {
	r.onUnknown = handler
}

func (r *Reporter) Report(phase string, message string) {
	osStatus := &magnetarapi.OsStatus{Phase: phase}
	if status := ProbeStatus(); status != nil {
		osStatus.BootedImage = status.Image
		osStatus.BootedDigest = status.Digest
		osStatus.BootedVersion = status.Version
	}
	if last := r.attempts.Last(); last != nil {
		osStatus.LastAttempt = &magnetarapi.OsAttempt{
			Image:     last.Image,
			Digest:    last.Digest,
			Result:    last.Result,
			Timestamp: last.Timestamp,
		}
	}
	req := &magnetarapi.NodeUpdateReq{
		NodeId:   r.nodeId,
		OsStatus: osStatus,
	}
	done := make(chan struct{})
	err := r.client.Update(req, func(resp *magnetarapi.NodeUpdateResp) {
		if resp.GetNotFound() && r.onUnknown != nil {
			r.onUnknown()
		}
		close(done)
	})
	if err != nil {
		log.Printf("[reporter] update send failed: %s", err)
		return
	}
	select {
	case <-done:
	case <-time.After(reportTimeout):
		log.Println("[reporter] update reply timed out")
	}
	log.Printf("[reporter] reported phase=%s (%s)", phase, message)
}
