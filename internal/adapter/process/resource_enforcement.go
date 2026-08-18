//go:build linux || darwin

package process

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

const resourceCgroupRootEnv = "SHELLBEAM_RESOURCE_CGROUP_ROOT"

const (
	resourceCleanupBudget = 500 * time.Millisecond
	resourceCleanupPoll   = 10 * time.Millisecond
	// pids.max is the hard kernel boundary; this poll only determines how
	// quickly ShellBeam turns a proven rejected fork/clone into terminal job
	// teardown. Keep it coarser than cleanup polling to avoid hot idle loops.
	resourceMonitorPoll = 50 * time.Millisecond
)

type resourcePathKind uint8

const (
	pathMissing resourcePathKind = iota
	pathRegular
	pathDirectory
	pathSymlink
	pathOther
)

type resourceDirEntry struct {
	Name string
	Kind resourcePathKind
}

type resourceControlOps interface {
	resolve(string) (string, error)
	kind(string) (resourcePathKind, error)
	isCgroup2(string) (bool, error)
	readDir(string) ([]resourceDirEntry, error)
	readFile(string) ([]byte, error)
	writeFile(string, string) error
	mkdir(string) error
	remove(string) error
}

type resourceNameFunc func(prefix string) (string, error)

type resourceSpawnBinding interface {
	Close() error
}

type resourceExecutionDomain interface {
	bind(*exec.Cmd) (resourceSpawnBinding, error)
	startMonitoring()
	finish() (operation.ResourceLimitKind, error)
	abort() error
}

type resourceProvider interface {
	prepareExecution(operation.ResourceLimits) (resourceExecutionDomain, error)
	support() capability.ResourceEnforcementSupport
}

type linuxCgroupProvider struct {
	root     string
	ops      resourceControlOps
	nextName resourceNameFunc
}

type preparedResourceDomain struct {
	path     string
	provider *linuxCgroupProvider
	baseline resourceEventSnapshot
	limits   operation.ResourceLimits

	monitorMu      sync.Mutex
	monitorStarted bool
	monitorStopped bool
	monitorStop    chan struct{}
	monitorDone    chan struct{}
	breach         operation.ResourceLimitKind
}

type resourceEventSnapshot struct {
	MemoryMax          uint64
	MemoryOOM          uint64
	MemoryOOMKill      uint64
	MemoryOOMGroupKill uint64
	PidsMax            uint64
}

type resourceProviderError struct{ reason string }

func (e resourceProviderError) Error() string { return "resource enforcement unavailable: " + e.reason }

func resourceProviderFailure(reason string) error { return resourceProviderError{reason: reason} }

func resourceCleanupReasonFromError(err error) string {
	if err == nil {
		return ""
	}
	var providerErr resourceProviderError
	if errors.As(err, &providerErr) {
		switch providerErr.reason {
		case "final_events_unavailable", "cleanup_kill_failed", "cleanup_events_failed", "cleanup_events_invalid", "cleanup_timeout", "cleanup_remove_failed":
			return providerErr.reason
		}
	}
	return "cleanup_unknown"
}

// NewOwnerFromEnvironment qualifies the platform resource provider once. An
// absent configuration is not an error and performs no provider probing.
func NewOwnerFromEnvironment() (Owner, *capability.ResourceEnforcementSupport, error) {
	provider, support, err := newResourceProviderFromEnvironment()
	if err != nil {
		return Owner{}, nil, err
	}
	return Owner{resources: provider}, support, nil
}

func (p *linuxCgroupProvider) support() capability.ResourceEnforcementSupport {
	return capability.ResourceEnforcementSupport{
		Version: 1, Maturity: "experimental", Provider: "linux_cgroup_v2", Scope: "owned_process_tree", Placement: "pre_exec_atomic",
		MemoryBytes: capability.EnforcementHard, Processes: capability.EnforcementHard,
		CPUTimeMS: capability.EnforcementUnsupported, PersistentSessions: capability.EnforcementUnsupported,
	}
}

func qualifyResourceProvider(root string, ops resourceControlOps, nextName resourceNameFunc) (*linuxCgroupProvider, error) {
	if ops == nil || nextName == nil || root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, resourceProviderFailure("invalid_root")
	}
	resolved, err := ops.resolve(root)
	if err != nil || filepath.Clean(resolved) != root {
		return nil, resourceProviderFailure("unsafe_root")
	}
	kind, err := ops.kind(root)
	if err != nil || kind != pathDirectory {
		return nil, resourceProviderFailure("invalid_root")
	}
	isV2, err := ops.isCgroup2(root)
	if err != nil || !isV2 {
		return nil, resourceProviderFailure("cgroup_v2_unavailable")
	}
	if !resourceFileEquals(ops, root, "cgroup.type", "domain") {
		return nil, resourceProviderFailure("domain_unavailable")
	}
	procs, err := ops.readFile(filepath.Join(root, "cgroup.procs"))
	if err != nil || strings.TrimSpace(string(procs)) != "" {
		return nil, resourceProviderFailure("root_not_dedicated")
	}
	for _, name := range []string{"cgroup.kill", "cgroup.events", "memory.events", "pids.events", "cgroup.controllers", "cgroup.subtree_control"} {
		if !resourceRegularFile(ops, filepath.Join(root, name)) {
			return nil, resourceProviderFailure("control_file_unavailable")
		}
	}
	controllers, err := ops.readFile(filepath.Join(root, "cgroup.controllers"))
	if err != nil || !containsController(controllers, "memory") || !containsController(controllers, "pids") {
		return nil, resourceProviderFailure("controllers_unavailable")
	}
	enabled, err := ops.readFile(filepath.Join(root, "cgroup.subtree_control"))
	if err != nil || !containsController(enabled, "memory") || !containsController(enabled, "pids") {
		return nil, resourceProviderFailure("controllers_not_enabled")
	}
	entries, err := ops.readDir(root)
	if err != nil {
		return nil, resourceProviderFailure("root_unreadable")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, entry := range entries {
		switch entry.Kind {
		case pathRegular:
			continue
		case pathDirectory:
			if !validResourceName(entry.Name, "job-") {
				return nil, resourceProviderFailure("foreign_child")
			}
			if err := cleanupResourceDir(ops, filepath.Join(root, entry.Name)); err != nil {
				return nil, resourceProviderFailure("stale_cleanup_failed")
			}
		default:
			return nil, resourceProviderFailure("unsafe_child")
		}
	}
	probeName, err := nextName("probe-")
	if err != nil || !validResourceName(probeName, "probe-") {
		return nil, resourceProviderFailure("probe_name_failed")
	}
	probe := filepath.Join(root, probeName)
	if err := ops.mkdir(probe); err != nil {
		return nil, resourceProviderFailure("probe_create_failed")
	}
	probeLive := true
	defer func() {
		if probeLive {
			_ = cleanupResourceDir(ops, probe)
		}
	}()
	for _, name := range []string{"memory.max", "memory.oom.group", "pids.max", "cgroup.kill", "cgroup.events", "memory.events", "pids.events", "cgroup.type"} {
		if !resourceRegularFile(ops, filepath.Join(probe, name)) {
			return nil, resourceProviderFailure("probe_control_unavailable")
		}
	}
	if !resourceFileEquals(ops, probe, "cgroup.type", "domain") {
		return nil, resourceProviderFailure("probe_domain_unavailable")
	}
	for name, value := range map[string]string{"memory.max": "max", "memory.oom.group": "0", "pids.max": "max"} {
		if err := ops.writeFile(filepath.Join(probe, name), value); err != nil {
			return nil, resourceProviderFailure("probe_configure_failed")
		}
	}
	if _, err := readResourceEventSnapshot(ops, probe); err != nil {
		return nil, resourceProviderFailure("probe_events_unavailable")
	}
	if err := cleanupResourceDir(ops, probe); err != nil {
		return nil, resourceProviderFailure("probe_cleanup_failed")
	}
	probeLive = false
	return &linuxCgroupProvider{root: root, ops: ops, nextName: nextName}, nil
}

func (p *linuxCgroupProvider) prepareExecution(limits operation.ResourceLimits) (resourceExecutionDomain, error) {
	return p.prepare(limits)
}

func (d *preparedResourceDomain) bind(cmd *exec.Cmd) (resourceSpawnBinding, error) {
	if d == nil || d.provider == nil || cmd == nil {
		return nil, resourceProviderFailure("spawn_binding_invalid")
	}
	return bindResourceDomainToCommand(d.path, cmd)
}

func (d *preparedResourceDomain) startMonitoring() {
	if d == nil || d.provider == nil || d.limits.Processes <= 0 {
		return
	}
	d.monitorMu.Lock()
	if d.monitorStarted {
		d.monitorMu.Unlock()
		return
	}
	d.monitorStarted = true
	d.monitorStop = make(chan struct{})
	d.monitorDone = make(chan struct{})
	stop, done := d.monitorStop, d.monitorDone
	d.monitorMu.Unlock()
	go d.monitorProcessBreaches(stop, done)
}

func (d *preparedResourceDomain) monitorProcessBreaches(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(resourceMonitorPoll)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			data, err := d.provider.ops.readFile(filepath.Join(d.path, "pids.events"))
			if err != nil {
				// Losing the event channel means ShellBeam can no longer uphold the
				// terminal-on-budget-attempt contract. Fail closed by terminating
				// the owned tree; do not invent a resource kind without evidence.
				_ = d.provider.ops.writeFile(filepath.Join(d.path, "cgroup.kill"), "1")
				return
			}
			max, err := parseFlatCounter(data, "max")
			if err != nil {
				_ = d.provider.ops.writeFile(filepath.Join(d.path, "cgroup.kill"), "1")
				return
			}
			if max > d.baseline.PidsMax {
				d.freezeBreach(operation.ResourceLimitProcesses)
				_ = d.provider.ops.writeFile(filepath.Join(d.path, "cgroup.kill"), "1")
				return
			}
		}
	}
}

func (d *preparedResourceDomain) stopMonitoring() {
	if d == nil {
		return
	}
	d.monitorMu.Lock()
	if !d.monitorStarted {
		d.monitorMu.Unlock()
		return
	}
	if !d.monitorStopped {
		close(d.monitorStop)
		d.monitorStopped = true
	}
	done := d.monitorDone
	d.monitorMu.Unlock()
	<-done
}

func (d *preparedResourceDomain) freezeBreach(kind operation.ResourceLimitKind) {
	if kind == "" {
		return
	}
	d.monitorMu.Lock()
	defer d.monitorMu.Unlock()
	if kind == operation.ResourceLimitMemory || d.breach == "" {
		d.breach = kind
	}
}

func (d *preparedResourceDomain) frozenBreach() operation.ResourceLimitKind {
	d.monitorMu.Lock()
	defer d.monitorMu.Unlock()
	return d.breach
}

func (d *preparedResourceDomain) finish() (operation.ResourceLimitKind, error) {
	if d == nil || d.provider == nil {
		return "", resourceProviderFailure("finalize_invalid")
	}
	d.stopMonitoring()
	final, readErr := readResourceEventSnapshot(d.provider.ops, d.path)
	if readErr == nil {
		d.freezeBreach(classifyResourceBreach(d.baseline, final))
	}
	breach := d.frozenBreach()
	cleanupErr := cleanupResourceDir(d.provider.ops, d.path)
	if readErr != nil {
		return breach, resourceProviderFailure("final_events_unavailable")
	}
	if cleanupErr != nil {
		return breach, cleanupErr
	}
	return breach, nil
}

func (p *linuxCgroupProvider) prepare(limits operation.ResourceLimits) (_ *preparedResourceDomain, retErr error) {
	if p == nil || p.ops == nil || p.nextName == nil {
		return nil, resourceProviderFailure("provider_unavailable")
	}
	if err := limits.Validate(); err != nil {
		return nil, resourceProviderFailure("invalid_limits")
	}
	if limits.CPUTimeMS > 0 {
		return nil, resourceProviderFailure("cpu_time_unsupported")
	}
	name, err := p.nextName("job-")
	if err != nil || !validResourceName(name, "job-") {
		return nil, resourceProviderFailure("job_name_failed")
	}
	path := filepath.Join(p.root, name)
	if err := p.ops.mkdir(path); err != nil {
		return nil, resourceProviderFailure("job_create_failed")
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = cleanupResourceDir(p.ops, path)
		}
	}()
	if limits.MemoryBytes > 0 {
		if err := p.ops.writeFile(filepath.Join(path, "memory.max"), strconv.FormatInt(limits.MemoryBytes, 10)); err != nil {
			return nil, resourceProviderFailure("memory_limit_failed")
		}
		if err := p.ops.writeFile(filepath.Join(path, "memory.oom.group"), "1"); err != nil {
			return nil, resourceProviderFailure("memory_group_failed")
		}
	}
	if limits.Processes > 0 {
		if err := p.ops.writeFile(filepath.Join(path, "pids.max"), strconv.Itoa(limits.Processes)); err != nil {
			return nil, resourceProviderFailure("process_limit_failed")
		}
	}
	baseline, err := readResourceEventSnapshot(p.ops, path)
	if err != nil {
		return nil, resourceProviderFailure("event_baseline_failed")
	}
	cleanup = false
	return &preparedResourceDomain{path: path, provider: p, baseline: baseline, limits: limits}, nil
}

func (d *preparedResourceDomain) abort() error {
	if d == nil || d.provider == nil {
		return nil
	}
	d.stopMonitoring()
	return cleanupResourceDir(d.provider.ops, d.path)
}

type resourceAtomicPlacementProbe func(resourceExecutionDomain) error

func verifyResourceAtomicPlacement(provider *linuxCgroupProvider, probe resourceAtomicPlacementProbe) error {
	if provider == nil || probe == nil {
		return resourceProviderFailure("atomic_placement_unavailable")
	}
	domain, err := provider.prepare(operation.ResourceLimits{Processes: 4})
	if err != nil {
		return resourceProviderFailure("atomic_placement_unavailable")
	}
	if err := probe(domain); err != nil {
		_ = domain.abort()
		return resourceProviderFailure("atomic_placement_unavailable")
	}
	breach, err := domain.finish()
	if err != nil || breach != "" {
		return resourceProviderFailure("atomic_placement_unavailable")
	}
	return nil
}

func cleanupResourceDir(ops resourceControlOps, path string) error {
	if ops == nil || path == "" {
		return resourceProviderFailure("cleanup_invalid")
	}
	if err := ops.writeFile(filepath.Join(path, "cgroup.kill"), "1"); err != nil {
		return resourceProviderFailure("cleanup_kill_failed")
	}
	deadline := time.Now().Add(resourceCleanupBudget)
	for {
		data, err := ops.readFile(filepath.Join(path, "cgroup.events"))
		if err != nil {
			return resourceProviderFailure("cleanup_events_failed")
		}
		populated, err := parseFlatCounter(data, "populated")
		if err != nil {
			return resourceProviderFailure("cleanup_events_invalid")
		}
		if populated == 0 {
			break
		}
		if !time.Now().Before(deadline) {
			return resourceProviderFailure("cleanup_timeout")
		}
		time.Sleep(resourceCleanupPoll)
	}
	if err := ops.remove(path); err != nil {
		return resourceProviderFailure("cleanup_remove_failed")
	}
	return nil
}

func readResourceEventSnapshot(ops resourceControlOps, path string) (resourceEventSnapshot, error) {
	memory, err := ops.readFile(filepath.Join(path, "memory.events"))
	if err != nil {
		return resourceEventSnapshot{}, err
	}
	pids, err := ops.readFile(filepath.Join(path, "pids.events"))
	if err != nil {
		return resourceEventSnapshot{}, err
	}
	return parseResourceEventSnapshot(memory, pids)
}

func parseResourceEventSnapshot(memory, pids []byte) (resourceEventSnapshot, error) {
	mem, err := parseFlatCounters(memory)
	if err != nil {
		return resourceEventSnapshot{}, err
	}
	pid, err := parseFlatCounters(pids)
	if err != nil {
		return resourceEventSnapshot{}, err
	}
	for _, key := range []string{"max", "oom", "oom_kill"} {
		if _, ok := mem[key]; !ok {
			return resourceEventSnapshot{}, fmt.Errorf("missing memory counter")
		}
	}
	pidsMax, ok := pid["max"]
	if !ok {
		return resourceEventSnapshot{}, fmt.Errorf("missing pids counter")
	}
	return resourceEventSnapshot{
		MemoryMax: mem["max"], MemoryOOM: mem["oom"], MemoryOOMKill: mem["oom_kill"], MemoryOOMGroupKill: mem["oom_group_kill"], PidsMax: pidsMax,
	}, nil
}

func classifyResourceBreach(before, after resourceEventSnapshot) operation.ResourceLimitKind {
	if countersDecreased(before, after) {
		return ""
	}
	if after.MemoryMax > before.MemoryMax || after.MemoryOOM > before.MemoryOOM || after.MemoryOOMKill > before.MemoryOOMKill || after.MemoryOOMGroupKill > before.MemoryOOMGroupKill {
		return operation.ResourceLimitMemory
	}
	if after.PidsMax > before.PidsMax {
		return operation.ResourceLimitProcesses
	}
	return ""
}

func countersDecreased(before, after resourceEventSnapshot) bool {
	return after.MemoryMax < before.MemoryMax || after.MemoryOOM < before.MemoryOOM || after.MemoryOOMKill < before.MemoryOOMKill || after.MemoryOOMGroupKill < before.MemoryOOMGroupKill || after.PidsMax < before.PidsMax
}

func parseFlatCounter(data []byte, key string) (uint64, error) {
	values, err := parseFlatCounters(data)
	if err != nil {
		return 0, err
	}
	value, ok := values[key]
	if !ok {
		return 0, fmt.Errorf("counter missing")
	}
	return value, nil
}

func parseFlatCounters(data []byte) (map[string]uint64, error) {
	values := map[string]uint64{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] == "" {
			return nil, fmt.Errorf("invalid counter line")
		}
		if _, duplicate := values[fields[0]]; duplicate {
			return nil, fmt.Errorf("duplicate counter")
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid counter")
		}
		values[fields[0]] = value
	}
	return values, nil
}

func resourceRegularFile(ops resourceControlOps, path string) bool {
	kind, err := ops.kind(path)
	return err == nil && kind == pathRegular
}

func resourceFileEquals(ops resourceControlOps, root, name, want string) bool {
	data, err := ops.readFile(filepath.Join(root, name))
	return err == nil && strings.TrimSpace(string(data)) == want
}

func containsController(data []byte, want string) bool {
	for _, value := range strings.Fields(string(data)) {
		if strings.TrimPrefix(value, "+") == want {
			return true
		}
	}
	return false
}

func validResourceName(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) || len(name) <= len(prefix) || len(name) > 64 || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name[len(prefix):] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func newOpaqueResourceName(prefix string) (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", resourceProviderFailure("random_unavailable")
	}
	return prefix + hex.EncodeToString(raw[:]), nil
}
