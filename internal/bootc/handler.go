package bootc

import (
	"fmt"
	"log"

	agent_queue "github.com/c12s/agent_queue/pkg/api"
	"github.com/golang/protobuf/proto"
	natsgo "github.com/nats-io/nats.go"
)

func OSUpdateSubject(nodeId string) string {
	return fmt.Sprintf("%s.os_update", nodeId)
}

type Handler struct {
	conn    *natsgo.Conn
	nodeId  string
	desired *DesiredStore
	sub     *natsgo.Subscription
}

func NewHandler(conn *natsgo.Conn, nodeId string, desired *DesiredStore) *Handler {
	return &Handler{
		conn:    conn,
		nodeId:  nodeId,
		desired: desired,
	}
}

func (h *Handler) Serve() error {
	sub, err := h.conn.Subscribe(OSUpdateSubject(h.nodeId), func(msg *natsgo.Msg) {
		cmd := &agent_queue.OSUpdateCmd{}
		if err := proto.Unmarshal(msg.Data, cmd); err != nil {
			log.Printf("[os_update] invalid command: %s", err)
			return
		}
		if cmd.Digest == "" || cmd.ImageRef == "" {
			log.Println("[os_update] command missing image_ref or digest, ignoring")
			return
		}
		log.Printf("[os_update] desired image set: %s@%s (reboot policy: %s)", cmd.ImageRef, cmd.Digest, cmd.RebootPolicy)
		h.desired.Set(DesiredImage{
			ImageRef:     cmd.ImageRef,
			Digest:       cmd.Digest,
			RebootPolicy: cmd.RebootPolicy,
		})
		if len(msg.Reply) > 0 {
			ack, err := proto.Marshal(&agent_queue.OSUpdateAck{})
			if err == nil {
				_ = h.conn.Publish(msg.Reply, ack)
			}
		}
	})
	if err != nil {
		return err
	}
	h.sub = sub
	return nil
}

func (h *Handler) GracefulStop() {
	if h.sub != nil {
		_ = h.sub.Unsubscribe()
	}
}
