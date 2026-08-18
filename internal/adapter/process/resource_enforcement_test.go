//go:build linux || darwin

package process

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestResourceEventCountersParseAndClassifyDeterministically(t *testing.T) {
	before, err := parseResourceEventSnapshot(
		[]byte("low 0\nhigh 0\nmax 1\noom 2\noom_kill 1\noom_group_kill 0\n"),
		[]byte("max 3\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	afterMemory, err := parseResourceEventSnapshot(
		[]byte("low 0\nhigh 0\nmax 2\noom 2\noom_kill 1\noom_group_kill 0\n"),
		[]byte("max 3\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := classifyResourceBreach(before, afterMemory); got != operation.ResourceLimitMemory {
		t.Fatalf("memory breach=%q", got)
	}
	afterProcesses, err := parseResourceEventSnapshot(
		[]byte("low 0\nhigh 0\nmax 1\noom 2\noom_kill 1\noom_group_kill 0\n"),
		[]byte("max 4\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := classifyResourceBreach(before, afterProcesses); got != operation.ResourceLimitProcesses {
		t.Fatalf("process breach=%q", got)
	}
	both, err := parseResourceEventSnapshot(
		[]byte("low 0\nhigh 0\nmax 2\noom 3\noom_kill 2\noom_group_kill 1\n"),
		[]byte("max 4\n"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := classifyResourceBreach(before, both); got != operation.ResourceLimitMemory {
		t.Fatalf("simultaneous breach priority=%q", got)
	}
	if got := classifyResourceBreach(before, before); got != "" {
		t.Fatalf("unchanged counters classified as %q", got)
	}
}

func TestResourceEventCountersRejectMalformedOrAmbiguousFacts(t *testing.T) {
	cases := []struct {
		memory string
		pids   string
	}{
		{"max nope\noom 0\noom_kill 0\n", "max 0\n"},
		{"max 0\nmax 1\noom 0\noom_kill 0\n", "max 0\n"},
		{"max 0\noom 0\noom_kill 0\n", ""},
		{"max 0\noom 0\n", "max 0\n"},
	}
	for _, tc := range cases {
		if _, err := parseResourceEventSnapshot([]byte(tc.memory), []byte(tc.pids)); err == nil {
			t.Fatalf("malformed counters accepted memory=%q pids=%q", tc.memory, tc.pids)
		}
	}
}

func TestResourceProviderQualificationIsClosedAndCleansOwnedStaleChildren(t *testing.T) {
	fs := newFakeCgroupFS("/cg/shellbeam")
	fs.addChild("job-stale", pathDirectory)
	provider, err := qualifyResourceProvider(fs.root, fs, fixedResourceName("probe-fixed", "job-fixed"))
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil {
		t.Fatal("qualified provider missing")
	}
	want := capability.ResourceEnforcementSupport{
		Version: 1, Maturity: "experimental", Provider: "linux_cgroup_v2", Scope: "owned_process_tree", Placement: "pre_exec_atomic",
		MemoryBytes: capability.EnforcementHard, Processes: capability.EnforcementHard, CPUTimeMS: capability.EnforcementUnsupported, PersistentSessions: capability.EnforcementUnsupported,
	}
	if got := provider.support(); !reflect.DeepEqual(got, want) {
		t.Fatalf("support=%#v want=%#v", got, want)
	}
	if fs.hasDir(filepath.Join(fs.root, "job-stale")) {
		t.Fatal("stale owned job was not removed")
	}
	if fs.hasDir(filepath.Join(fs.root, "probe-fixed")) {
		t.Fatal("qualification probe leaked")
	}
	for _, wantWrite := range []string{
		"job-stale/cgroup.kill=1", "probe-fixed/memory.max=max", "probe-fixed/memory.oom.group=0", "probe-fixed/pids.max=max", "probe-fixed/cgroup.kill=1",
	} {
		if !containsSuffix(fs.writes, wantWrite) {
			t.Fatalf("missing qualification write %q in %#v", wantWrite, fs.writes)
		}
	}
}

func TestResourceProviderQualificationRejectsUnsafeOrUnqualifiedRoots(t *testing.T) {
	tests := []struct {
		name string
		edit func(*fakeCgroupFS) string
	}{
		{"relative", func(fs *fakeCgroupFS) string { return "relative/root" }},
		{"resolved_path_changed", func(fs *fakeCgroupFS) string { fs.resolved = "/different"; return fs.root }},
		{"not_cgroup2", func(fs *fakeCgroupFS) string { fs.cgroup2 = false; return fs.root }},
		{"missing_memory_controller", func(fs *fakeCgroupFS) string {
			fs.files[filepath.Join(fs.root, "cgroup.controllers")] = "pids\n"
			return fs.root
		}},
		{"controller_not_enabled", func(fs *fakeCgroupFS) string {
			fs.files[filepath.Join(fs.root, "cgroup.subtree_control")] = "memory\n"
			return fs.root
		}},
		{"foreign_child", func(fs *fakeCgroupFS) string { fs.addChild("other", pathDirectory); return fs.root }},
		{"symlink_child", func(fs *fakeCgroupFS) string { fs.addChild("job-link", pathSymlink); return fs.root }},
		{"root_has_process", func(fs *fakeCgroupFS) string {
			fs.files[filepath.Join(fs.root, "cgroup.procs")] = "123\n"
			return fs.root
		}},
		{"missing_kill", func(fs *fakeCgroupFS) string { delete(fs.kinds, filepath.Join(fs.root, "cgroup.kill")); return fs.root }},
		{"threaded_root", func(fs *fakeCgroupFS) string {
			fs.files[filepath.Join(fs.root, "cgroup.type")] = "threaded\n"
			return fs.root
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeCgroupFS("/cg/shellbeam")
			root := tc.edit(fs)
			_, err := qualifyResourceProvider(root, fs, fixedResourceName("probe-fixed", "job-fixed"))
			if err == nil {
				t.Fatal("unqualified root accepted")
			}
			if strings.Contains(err.Error(), fs.root) || strings.Contains(err.Error(), "/different") {
				t.Fatalf("qualification error leaked private path: %v", err)
			}
		})
	}
}

func TestResourceProviderPrepareConfiguresOnlyRequestedHardLimitsAndAbortsCleanly(t *testing.T) {
	fs := newFakeCgroupFS("/cg/shellbeam")
	provider, err := qualifyResourceProvider(fs.root, fs, fixedResourceName("probe-fixed", "job-fixed"))
	if err != nil {
		t.Fatal(err)
	}
	domain, err := provider.prepare(operation.ResourceLimits{MemoryBytes: 64 << 20, Processes: 7})
	if err != nil {
		t.Fatal(err)
	}
	if domain == nil || !strings.HasSuffix(domain.path, "/job-fixed") {
		t.Fatalf("domain=%#v", domain)
	}
	for _, wantWrite := range []string{"job-fixed/memory.max=67108864", "job-fixed/memory.oom.group=1", "job-fixed/pids.max=7"} {
		if !containsSuffix(fs.writes, wantWrite) {
			t.Fatalf("missing prepare write %q in %#v", wantWrite, fs.writes)
		}
	}
	if err := domain.abort(); err != nil {
		t.Fatal(err)
	}
	if fs.hasDir(filepath.Join(fs.root, "job-fixed")) {
		t.Fatal("aborted domain leaked")
	}
}

func TestResourceProviderPrepareFailureCleansPartialDomain(t *testing.T) {
	fs := newFakeCgroupFS("/cg/shellbeam")
	provider, err := qualifyResourceProvider(fs.root, fs, fixedResourceName("probe-fixed", "job-fixed"))
	if err != nil {
		t.Fatal(err)
	}
	fs.failWriteSuffix = "/pids.max"
	if _, err := provider.prepare(operation.ResourceLimits{Processes: 4}); err == nil {
		t.Fatal("prepare unexpectedly succeeded")
	}
	if fs.hasDir(filepath.Join(fs.root, "job-fixed")) {
		t.Fatal("failed prepare leaked domain")
	}
}

func TestResourceProcessBreachMonitorKillsJobBeforeFinalization(t *testing.T) {
	fs := newFakeCgroupFS("/cg/shellbeam")
	provider, err := qualifyResourceProvider(fs.root, fs, fixedResourceName("probe-fixed", "job-fixed"))
	if err != nil {
		t.Fatal(err)
	}
	domain, err := provider.prepare(operation.ResourceLimits{Processes: 4})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a fork/clone attempt being refused by pids.max before monitoring
	// starts. The first monitor observation must terminate the whole job rather
	// than waiting for the root process to decide whether to recover from EAGAIN.
	fs.files[filepath.Join(domain.path, "pids.events")] = "max 1\n"
	fs.killCh = make(chan string, 1)
	domain.startMonitoring()
	select {
	case killed := <-fs.killCh:
		if killed != filepath.Join(domain.path, "cgroup.kill") {
			t.Fatalf("monitor killed %q", killed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("pids.max breach was not terminal before finalization")
	}
	breach, err := domain.finish()
	if err != nil {
		t.Fatal(err)
	}
	if breach != operation.ResourceLimitProcesses {
		t.Fatalf("breach=%q", breach)
	}
	if fs.hasDir(domain.path) {
		t.Fatal("finished process-breach domain leaked")
	}
}

func TestResourceAtomicPlacementQualificationFailsClosedAndCleansProbeJob(t *testing.T) {
	fs := newFakeCgroupFS("/cg/shellbeam")
	provider, err := qualifyResourceProvider(fs.root, fs, fixedResourceName("probe-fixed", "job-placement"))
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	err = verifyResourceAtomicPlacement(provider, func(domain resourceExecutionDomain) error {
		called++
		return errors.New("clone3 blocked")
	})
	if err == nil {
		t.Fatal("failed atomic placement probe was accepted")
	}
	if called != 1 {
		t.Fatalf("atomic probe calls=%d", called)
	}
	if strings.Contains(err.Error(), fs.root) {
		t.Fatalf("atomic probe error leaked cgroup root: %v", err)
	}
	if fs.hasDir(filepath.Join(fs.root, "job-placement")) {
		t.Fatal("failed atomic placement probe leaked job cgroup")
	}
}

func TestResourceAtomicPlacementQualificationAcceptsOnlyCompletedProbe(t *testing.T) {
	fs := newFakeCgroupFS("/cg/shellbeam")
	provider, err := qualifyResourceProvider(fs.root, fs, fixedResourceName("probe-fixed", "job-placement"))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyResourceAtomicPlacement(provider, func(resourceExecutionDomain) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if fs.hasDir(filepath.Join(fs.root, "job-placement")) {
		t.Fatal("successful atomic placement probe leaked job cgroup")
	}
}

func fixedResourceName(names ...string) resourceNameFunc {
	i := 0
	return func(prefix string) (string, error) {
		if i >= len(names) {
			return "", errors.New("name_exhausted")
		}
		name := names[i]
		i++
		if !strings.HasPrefix(name, prefix) {
			return "", fmt.Errorf("unexpected prefix %q for %q", prefix, name)
		}
		return name, nil
	}
}

func containsSuffix(values []string, suffix string) bool {
	for _, value := range values {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

type fakeCgroupFS struct {
	root            string
	resolved        string
	cgroup2         bool
	kinds           map[string]resourcePathKind
	files           map[string]string
	writes          []string
	failWriteSuffix string
	killCh          chan string
}

func newFakeCgroupFS(root string) *fakeCgroupFS {
	fs := &fakeCgroupFS{root: root, resolved: root, cgroup2: true, kinds: map[string]resourcePathKind{}, files: map[string]string{}}
	fs.kinds[root] = pathDirectory
	for name, value := range map[string]string{
		"cgroup.controllers":     "memory pids\n",
		"cgroup.subtree_control": "memory pids\n",
		"cgroup.events":          "populated 0\nfrozen 0\n",
		"cgroup.procs":           "",
		"cgroup.type":            "domain\n",
		"memory.events":          "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\noom_group_kill 0\n",
		"pids.events":            "max 0\n",
		"cgroup.kill":            "",
	} {
		path := filepath.Join(root, name)
		fs.kinds[path] = pathRegular
		fs.files[path] = value
	}
	return fs
}

func (f *fakeCgroupFS) resolve(path string) (string, error) { return f.resolved, nil }
func (f *fakeCgroupFS) kind(path string) (resourcePathKind, error) {
	kind, ok := f.kinds[path]
	if !ok {
		return pathMissing, errors.New("missing")
	}
	return kind, nil
}
func (f *fakeCgroupFS) isCgroup2(string) (bool, error) { return f.cgroup2, nil }
func (f *fakeCgroupFS) readDir(path string) ([]resourceDirEntry, error) {
	prefix := strings.TrimSuffix(path, "/") + "/"
	out := []resourceDirEntry{}
	for candidate, kind := range f.kinds {
		if !strings.HasPrefix(candidate, prefix) {
			continue
		}
		rest := strings.TrimPrefix(candidate, prefix)
		if rest == "" || strings.Contains(rest, "/") || kind == pathRegular {
			continue
		}
		out = append(out, resourceDirEntry{Name: rest, Kind: kind})
	}
	return out, nil
}
func (f *fakeCgroupFS) readFile(path string) ([]byte, error) {
	value, ok := f.files[path]
	if !ok {
		return nil, errors.New("missing")
	}
	return []byte(value), nil
}
func (f *fakeCgroupFS) writeFile(path, value string) error {
	if f.failWriteSuffix != "" && strings.HasSuffix(path, f.failWriteSuffix) {
		return errors.New("write_failed")
	}
	if _, ok := f.kinds[path]; !ok {
		return errors.New("missing")
	}
	f.writes = append(f.writes, path+"="+value)
	f.files[path] = value
	if f.killCh != nil && strings.HasSuffix(path, "/cgroup.kill") && value == "1" {
		select {
		case f.killCh <- path:
		default:
		}
	}
	return nil
}
func (f *fakeCgroupFS) mkdir(path string) error {
	if _, exists := f.kinds[path]; exists {
		return errors.New("exists")
	}
	f.kinds[path] = pathDirectory
	for name, value := range map[string]string{
		"memory.max":       "max",
		"memory.oom.group": "0",
		"pids.max":         "max",
		"cgroup.kill":      "",
		"cgroup.events":    "populated 0\nfrozen 0\n",
		"memory.events":    "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\noom_group_kill 0\n",
		"pids.events":      "max 0\n",
		"cgroup.type":      "domain\n",
	} {
		child := filepath.Join(path, name)
		f.kinds[child] = pathRegular
		f.files[child] = value
	}
	return nil
}
func (f *fakeCgroupFS) remove(path string) error {
	prefix := strings.TrimSuffix(path, "/") + "/"
	for candidate := range f.kinds {
		if strings.HasPrefix(candidate, prefix) {
			delete(f.kinds, candidate)
			delete(f.files, candidate)
		}
	}
	delete(f.kinds, path)
	delete(f.files, path)
	return nil
}
func (f *fakeCgroupFS) addChild(name string, kind resourcePathKind) {
	path := filepath.Join(f.root, name)
	f.kinds[path] = kind
	if kind == pathDirectory {
		_ = f.populateChild(path)
	}
}
func (f *fakeCgroupFS) populateChild(path string) error {
	for name, value := range map[string]string{
		"cgroup.kill":   "",
		"cgroup.events": "populated 0\nfrozen 0\n",
		"memory.events": "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\noom_group_kill 0\n",
		"pids.events":   "max 0\n",
	} {
		child := filepath.Join(path, name)
		f.kinds[child] = pathRegular
		f.files[child] = value
	}
	return nil
}
func (f *fakeCgroupFS) hasDir(path string) bool { return f.kinds[path] == pathDirectory }
