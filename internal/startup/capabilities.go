package startup

import (
	"os"
	"os/exec"

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
