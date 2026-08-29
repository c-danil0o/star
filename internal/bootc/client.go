package bootc

import (
	"encoding/json"
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
