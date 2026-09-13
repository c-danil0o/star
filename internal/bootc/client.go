package bootc

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

const grubEnvPath = "/boot/grub2/grubenv"

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
	// Stage the deployment without --apply: the caller records the attempt
	// BEFORE rebooting, so the loop guard knows a switch was performed even
	// if the reboot kills this process mid-flight.
	//
	// Rollback trigger arming is handled by systemd:
	// greenboot-set-rollback-trigger.service (RequiredBy=ostree-finalize-staged)
	// fires during the staging shutdown path and sets fallback=1 +
	// greenboot_next_deployment_id in grubenv. greenboot-rs 0.16 health-check
	// manages boot_counter entirely in userspace (3 failed checks → bootc
	// rollback). Verified E2E on Fedora 45 / greenboot-rs 0.16.
	ref := fmt.Sprintf("%s@%s", imageRef, digest)
	out, err := exec.Command("bootc", "switch", ref).CombinedOutput()
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

// CurrentBootID returns the kernel boot id, which changes on every boot.
// Empty string means "unknown" (callers treat that as "assume rebooted").
func CurrentBootID() string {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// GreenbootPresent reports whether greenboot's health gate is installed. The
// binary is not in PATH (/usr/libexec/greenboot/greenboot).
func GreenbootPresent() bool {
	_, err := os.Stat("/usr/libexec/greenboot/greenboot")
	return err == nil
}

// GrubEnvValue reads a single key from the GRUB environment block.
func GrubEnvValue(key string) (string, bool) {
	data, err := os.ReadFile(grubEnvPath)
	if err != nil {
		return "", false
	}
	prefix := key + "="
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\x00")
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix), true
		}
	}
	return "", false
}

// RepairStaleBootState clears an exhausted greenboot boot counter left over
// from a deployment whose rollback dead-ended (greenboot "manual intervention
// required": boot_counter=0/-1, no rollback trigger). The legacy GRUB snippet
// 08_greenboot.cfg uses a non-empty boot_counter with boot_success=0 to force
// default=1; after re-staging an image the second entry can be the failed
// deployment itself, so the node boots the broken image again no matter which
// image was just staged. Clearing the counter before a switch restores the
// normal boot entry. Best effort: on nodes without greenboot/grubenv this is
// a no-op.
func RepairStaleBootState() {
	value, ok := GrubEnvValue("boot_counter")
	if !ok || (value != "0" && value != "-1") {
		return
	}
	out, err := exec.Command("grub2-editenv", "-", "unset", "boot_counter").CombinedOutput()
	if err != nil {
		// /boot is frequently mounted read-only outside greenboot's write
		// window (and greenboot is not around to remount it now).
		if _, mErr := exec.Command("mount", "-o", "remount,rw", "/boot").CombinedOutput(); mErr != nil {
			log.Printf("[bootc] failed to clear stale boot_counter=%s: %s (%s)", value, err, string(out))
			return
		}
		if retryOut, retryErr := exec.Command("grub2-editenv", "-", "unset", "boot_counter").CombinedOutput(); retryErr != nil {
			log.Printf("[bootc] failed to clear stale boot_counter=%s: %s (%s)", value, retryErr, string(retryOut))
			return
		}
	}
	log.Printf("[bootc] cleared stale greenboot boot_counter=%s before switching", value)
}

func Reboot() error {
	return exec.Command("systemctl", "reboot").Run()
}
