package startup

import (
	"log"
	"os"
	"os/exec"
	"time"

	magnetarapi "github.com/c12s/magnetar/pkg/api"
	"github.com/c12s/star/internal/bootc"
)

// libvirtSocketPaths covers both daemon layouts: the monolithic libvirtd
// socket and the modular virtqemud socket (default on modern Fedora and on
// this lab host). The VM reconciler shells out to virsh/virt-install, which
// reach either daemon transparently.
var libvirtSocketPaths = []string{
	"/var/run/libvirt/libvirt-sock",
	"/run/libvirt/virtqemud-sock",
}

func discoverCapabilities() []string {
	capabilities := []string{magnetarapi.CapabilityConfig, magnetarapi.CapabilityGossip}
	if commandExists("bootc") {
		capabilities = append(capabilities, magnetarapi.CapabilityOsUpdate)
	}
	if libvirtSocketExists() {
		capabilities = append(capabilities, magnetarapi.CapabilityRunVms)
	}
	return capabilities
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func libvirtSocketExists() bool {
	for _, path := range libvirtSocketPaths {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

type syncResult struct {
	found bool
	err   error
}

func syncNodeState(client *magnetarapi.NodeUpdateAsyncClient, nodeId string, capabilities []string, osStatus *magnetarapi.OsStatus) (bool, error) {
	ch := make(chan syncResult, 1)
	req := &magnetarapi.NodeUpdateReq{
		NodeId:       nodeId,
		Capabilities: capabilities,
		OsStatus:     osStatus,
	}
	err := client.Update(req, func(resp *magnetarapi.NodeUpdateResp) {
		ch <- syncResult{found: !resp.GetNotFound(), err: nil}
	})
	if err != nil {
		return false, err
	}
	select {
	case res := <-ch:
		return res.found, res.err
	case <-time.After(30 * time.Second):
		// no answer (CP down or storage error) proves nothing: keep the node
		// id, re-registering would orphan the node's org, policies and VMs
		log.Println("node state sync timed out, keeping node id")
		return true, nil
	}
}

func buildOsStatus() *magnetarapi.OsStatus {
	bootcStatus := bootc.ProbeStatus()
	if bootcStatus == nil {
		return nil
	}
	return &magnetarapi.OsStatus{
		BootedImage:   bootcStatus.Image,
		BootedDigest:  bootcStatus.Digest,
		BootedVersion: bootcStatus.Version,
		Phase:         magnetarapi.OsPhaseHealthy,
	}
}
