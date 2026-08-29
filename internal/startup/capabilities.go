package startup

import (
	"log"
	"os"
	"os/exec"
	"time"

	magnetarapi "github.com/c12s/magnetar/pkg/api"
	"github.com/c12s/star/internal/bootc"
)

const libvirtSocketPath = "/var/run/libvirt/libvirt-sock"

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
	_, err := os.Stat(libvirtSocketPath)
	return err == nil
}

func syncNodeState(client *magnetarapi.NodeUpdateAsyncClient, nodeId string, capabilities []string, osStatus *magnetarapi.OsStatus) error {
	errChan := make(chan error)
	req := &magnetarapi.NodeUpdateReq{
		NodeId:       nodeId,
		Capabilities: capabilities,
		OsStatus:     osStatus,
	}
	err := client.Update(req, func(resp *magnetarapi.NodeUpdateResp) {
		errChan <- nil
	})
	if err != nil {
		return err
	}
	select {
	case err = <-errChan:
		return err
	case <-time.After(30 * time.Second):
		log.Println("node state sync timed out")
		return nil
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
