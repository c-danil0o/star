package startup

import (
	"log"
	"os"
	"os/exec"
	"time"

	magnetarapi "github.com/c12s/magnetar/pkg/api"
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

func syncNodeState(client *magnetarapi.NodeUpdateAsyncClient, nodeId string, capabilities []string) error {
	errChan := make(chan error)
	req := &magnetarapi.NodeUpdateReq{
		NodeId:       nodeId,
		Capabilities: capabilities,
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
