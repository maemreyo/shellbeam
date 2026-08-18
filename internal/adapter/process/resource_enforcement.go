//go:build linux || darwin

package process

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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
	if err := probeResourceProviderRoot(root, ops, nextName); err != nil {
		return nil, err
	}
	return &linuxCgroupProvider{root: root, ops: ops, nextName: nextName}, nil
}

func probeResourceProviderRoot(root string, ops resourceControlOps, nextName resourceNameFunc) error {
	probeName, err := nextName("probe-")
	if err != nil || !validResourceName(probeName, "probe-") {
		return resourceProviderFailure("probe_name_failed")
	}
	probe := filepath.Join(root, probeName)
	if err := ops.mkdir(probe); err != nil {
		return resourceProviderFailure("probe_create_failed")
	}
	probeLive := true
	defer func() {
		if probeLive {
			_ = cleanupResourceDir(ops, probe)
		}
	}()
	for _, name := range []string{"memory.max", "memory.oom.group", "pids.max", "cgroup.kill", "cgroup.events", "memory.events", "pids.events", "cgroup.type"} {
		if !resourceRegularFile(ops, filepath.Join(probe, name)) {
			return resourceProviderFailure("probe_control_unavailable")
		}
	}
	if !resourceFileEquals(ops, probe, "cgroup.type", "domain") {
		return resourceProviderFailure("probe_domain_unavailable")
	}
	for name, value := range map[string]string{"memory.max": "max", "memory.oom.group": "0", "pids.max": "max"} {
		if err := ops.writeFile(filepath.Join(probe, name), value); err != nil {
			return resourceProviderFailure("probe_configure_failed")
		}
	}
	if _, err := readResourceEventSnapshot(ops, probe); err != nil {
		return resourceProviderFailure("probe_events_unavailable")
	}
	if err := cleanupResourceDir(ops, probe); err != nil {
		return resourceProviderFailure("probe_cleanup_failed")
	}
	probeLive = false
	return nil
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
