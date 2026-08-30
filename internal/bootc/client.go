package bootc

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
)

type Status struct {
	Image   string
	Digest  string
	Version string
}

// bootc status --json returns the org.containers.bootc/v1 BootcHost schema.
// The booted deployment lives at .status.booted.image with the following shape:
//
//	"image": {
//	    "image": {"image": "<ref>", "transport": "registry"},
//	    "imageDigest": "sha256:...",
//	    "version": "..."
//	}
type bootcStatusOutput struct {
	Status *struct {
		Booted *bootcStatusEntry `json:"booted"`
	} `json:"status"`
}

type bootcStatusEntry struct {
	Image *bootcImageStatus `json:"image"`
}

type bootcImageStatus struct {
	Image       *bootcImageRef `json:"image"`
	ImageDigest string         `json:"imageDigest"`
	Version     string         `json:"version"`
}

type bootcImageRef struct {
	Image     string `json:"image"`
	Transport string `json:"transport"`
}

func ProbeStatus() *Status {
	out, err := exec.Command("bootc", "status", "--json").CombinedOutput()
	if err != nil {
		log.Printf("[bootc] status failed: %s (stderr: %s)", err, string(out))
		return nil
	}
	parsed := &bootcStatusOutput{}
	if err := json.Unmarshal(out, parsed); err != nil {
		log.Printf("[bootc] status JSON parse failed: %s", err)
		return nil
	}
	if parsed.Status == nil || parsed.Status.Booted == nil {
		log.Println("[bootc] status: no booted deployment found")
		return nil
	}
	booted := parsed.Status.Booted
	if booted.Image == nil {
		return nil
	}
	status := &Status{
		Digest:  booted.Image.ImageDigest,
		Version: booted.Image.Version,
	}
	if booted.Image.Image != nil {
		status.Image = booted.Image.Image.Image
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
