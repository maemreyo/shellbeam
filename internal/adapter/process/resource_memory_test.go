//go:build linux || darwin

package process

import (
	"strings"
	"testing"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestResourceProviderQualificationRequiresWritableSwapControl(t *testing.T) {
	fs := newFakeCgroupFS("/cg/shellbeam")
	fs.failWriteSuffix = "/memory.swap.max"
	_, err := qualifyResourceProvider(fs.root, fs, fixedResourceName("probe-fixed", "job-fixed"))
	if err == nil || !strings.Contains(err.Error(), "probe_configure_failed") {
		t.Fatalf("qualification error=%v want probe_configure_failed", err)
	}
}

func TestResourceProviderMemoryLimitDisablesSwap(t *testing.T) {
	fs := newFakeCgroupFS("/cg/shellbeam")
	provider, err := qualifyResourceProvider(fs.root, fs, fixedResourceName("probe-fixed", "job-fixed"))
	if err != nil {
		t.Fatal(err)
	}
	domain, err := provider.prepare(operation.ResourceLimits{MemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = domain.abort() }()
	if !containsSuffix(fs.writes, "job-fixed/memory.swap.max=0") {
		t.Fatalf("memory-limited job did not disable swap: %#v", fs.writes)
	}
}
