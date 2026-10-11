package vm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	magnetarapi "github.com/c12s/magnetar/pkg/api"
)

const desiredTimeout = 10 * time.Second

// Reconciler converges the host's libvirt VMs to the desired VM set stored on
// the host node record (phase-next.md §2.2/§2.4). Desired state is polled
// from Magnetar (standing declaration, survives reboots), observed state is
// reported back, and the diff is applied:
//
//	missing        -> cached qcow2 -> seed -> virt-install (boots immediately)
//	shut off       -> virsh start (auto-recovery)
//	not desired    -> virsh destroy + undefine + remove local files
//
// Safety rule: when the desired set cannot be fetched (CP down, timeout), the
// tick is skipped entirely — a transient failure must never destroy VMs.
type Reconciler struct {
	client         *magnetarapi.VmAsyncClient
	hostId         string
	cache          *Qcow2Cache
	manager        *Manager
	interval       time.Duration
	ipBase         string
	ipOffset       int
	natsAddress    string
	singularityURL string
}

type ReconcilerConfig struct {
	HostId         string
	Interval       time.Duration
	CacheDir       string
	StateDir       string
	IPBase         string
	IPOffset       int
	NATSAddress    string
	SingularityURL string
	ManagerConfig  ManagerConfig
}

func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	if cfg.HostId == "" {
		return nil, errors.New("vm reconciler: host id is required")
	}
	if cfg.NATSAddress == "" {
		return nil, errors.New("vm reconciler: NATS address is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.IPBase == "" {
		cfg.IPBase = "192.168.122"
	}
	client, err := magnetarapi.NewVmAsyncClient(cfg.NATSAddress)
	if err != nil {
		return nil, fmt.Errorf("vm reconciler: nats client: %w", err)
	}
	manager, err := NewManager(cfg.ManagerConfig, cfg.StateDir)
	if err != nil {
		return nil, err
	}
	return &Reconciler{
		client:         client,
		hostId:         cfg.HostId,
		cache:          NewQcow2Cache(cfg.CacheDir),
		manager:        manager,
		interval:       cfg.Interval,
		ipBase:         cfg.IPBase,
		ipOffset:       cfg.IPOffset,
		natsAddress:    cfg.NATSAddress,
		singularityURL: cfg.SingularityURL,
	}, nil
}

func (r *Reconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.reconcile()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.reconcile()
		}
	}
}

func (r *Reconciler) reconcile() {
	desired, err := r.getDesired()
	if err != nil {
		log.Printf("[vm] desired state unavailable, skipping tick: %s", err)
		return
	}
	managed, err := r.manager.List()
	if err != nil {
		log.Printf("[vm] cannot list libvirt domains, skipping tick: %s", err)
		return
	}

	desiredNames := make(map[string]bool, len(desired))
	for _, vm := range desired {
		desiredNames[vm.Name] = true
	}
	sort.Strings(managed)
	for _, name := range managed {
		if desiredNames[name] {
			continue
		}
		log.Printf("[vm] %s is not desired anymore, destroying", name)
		if err := r.manager.Destroy(name); err != nil {
			log.Printf("[vm] destroy %s failed: %s", name, err)
		}
	}

	statuses := make([]*magnetarapi.VmStatus, 0, len(desired))
	addresses := make(map[string]string, len(desired))
	for _, spec := range desired {
		if spec.Name == "" {
			log.Println("[vm] desired vm with empty name, ignoring")
			continue
		}
		if err := r.ensure(spec, addresses); err != nil {
			log.Printf("[vm] ensure %s failed: %s", spec.Name, err)
		}
		statuses = append(statuses, r.status(spec.Name))
	}
	if err := r.reportStatuses(statuses); err != nil {
		log.Printf("[vm] report statuses failed: %s", err)
	}
}

// ensure makes a single desired VM real: create it if missing, restart it if
// shut off, then verify it exists. The addresses map detects two desired VMs
// that would claim the same static address.
func (r *Reconciler) ensure(spec *magnetarapi.DesiredVm, addresses map[string]string) error {
	address, err := staticAddress(spec.Name, r.ipBase, r.ipOffset)
	if err != nil {
		return err
	}
	if other, taken := addresses[address]; taken {
		return fmt.Errorf("address %s already claimed by %s", address, other)
	}
	addresses[address] = spec.Name

	if r.manager.Exists(spec.Name) {
		state, err := r.manager.State(spec.Name)
		if err != nil {
			return err
		}
		if state == "shut off" {
			log.Printf("[vm] starting shut-off VM %s", spec.Name)
			return r.manager.Start(spec.Name)
		}
		return nil
	}

	log.Printf("[vm] creating VM %s", spec.Name)
	diskPath, err := r.cache.Ensure(spec.Qcow2Url, spec.Digest)
	if err != nil {
		return err
	}
	mac := deterministicMAC(spec.Name)
	seedPath, err := BuildSeed(SeedSpec{
		Dir:         r.manager.VMDir(spec.Name),
		Domain:      spec.Name,
		MAC:         mac,
		Address:     address,
		Gateway:     gatewayFromBase(r.ipBase),
		NATSAddress: r.natsAddress,
		Extras:      r.seedExtras(),
	})
	if err != nil {
		return err
	}
	return r.manager.Create(CreateSpec{
		Name:     spec.Name,
		DiskPath: diskPath,
		SeedPath: seedPath,
		MAC:      mac,
		VCPUs:    spec.Vcpus,
		MemoryMB: spec.MemoryMb,
	})
}

func (r *Reconciler) status(name string) *magnetarapi.VmStatus {
	state := "unknown"
	if s, err := r.manager.State(name); err == nil {
		state = s
	}
	return &magnetarapi.VmStatus{Name: name, State: state}
}

// getDesired fails on no answer (timeout, no responders): the caller then
// skips the tick, so a control-plane outage never reads as "no VMs".
func (r *Reconciler) getDesired() ([]*magnetarapi.DesiredVm, error) {
	ctx, cancel := context.WithTimeout(context.Background(), desiredTimeout)
	defer cancel()
	resp, err := r.client.GetDesiredVms(ctx, r.hostId)
	if err != nil {
		return nil, err
	}
	return resp.Vms, nil
}

func (r *Reconciler) reportStatuses(statuses []*magnetarapi.VmStatus) error {
	ctx, cancel := context.WithTimeout(context.Background(), desiredTimeout)
	defer cancel()
	_, err := r.client.SetVmStatuses(ctx, r.hostId, statuses)
	return err
}

// seedExtras are env vars passed into every VM's /var/lib/c12s/node.env.
func (r *Reconciler) seedExtras() map[string]string {
	extras := map[string]string{}
	if r.singularityURL != "" {
		extras["SINGULARITY_URL"] = r.singularityURL
	}
	return extras
}
