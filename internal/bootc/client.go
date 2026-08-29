package bootc

import (
	"encoding/json"
	"fmt"
	"os/exec"
)

type Status struct {
	Image   string
	Digest  string
	Version string
}

type bootcStatusOutput struct {
	Booted *bootcStatusEntry `json:"booted"`
	Status *struct {
		Booted *bootcStatusEntry `json:"booted"`
	} `json:"status"`
}

type bootcStatusEntry struct {
	Image *struct {
		Image  string `json:"image"`
		Digest string `json:"digest"`
	} `json:"image"`
	Version string `json:"version"`
}

func ProbeStatus() *Status {
	out, err := exec.Command("bootc", "status", "--json").Output()
	if err != nil {
		return nil
	}
	parsed := &bootcStatusOutput{}
	if err := json.Unmarshal(out, parsed); err != nil {
		return nil
	}
	booted := parsed.Booted
	if booted == nil && parsed.Status != nil {
		booted = parsed.Status.Booted
	}
	if booted == nil {
		return nil
	}
	status := &Status{Version: booted.Version}
	if booted.Image != nil {
		status.Image = booted.Image.Image
		status.Digest = booted.Image.Digest
	}
	return status
}

func Switch(imageRef, digest string) error {
	ref := fmt.Sprintf("%s@%s", imageRef, digest)
	out, err := exec.Command("bootc", "switch", "--apply", ref).CombinedOutput()
	if err != nil {
		return fmt.Errorf("bootc switch failed: %s", string(out))
	}
	return nil
}

func Rollback() error {
	out, err := exec.Command("bootc", "rollback").CombinedOutput()
	if err != nil {
		return fmt.Errorf("bootc rollback failed: %s", string(out))
	}
	return Reboot()
}

func Reboot() error {
	return exec.Command("systemctl", "reboot").Run()
}
