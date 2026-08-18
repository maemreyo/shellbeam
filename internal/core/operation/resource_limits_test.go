package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestResourceLimitsValidateAndNameKinds(t *testing.T) {
	valid := []ResourceLimits{
		{MemoryBytes: 64 << 20},
		{Processes: 8},
		{CPUTimeMS: 1000},
		{MemoryBytes: 64 << 20, Processes: 8, CPUTimeMS: 1000},
	}
	for _, limits := range valid {
		if err := limits.Validate(); err != nil {
			t.Fatalf("Validate(%#v)=%v", limits, err)
		}
		if limits.Empty() {
			t.Fatalf("non-empty limits reported empty: %#v", limits)
		}
	}
	for _, limits := range []ResourceLimits{
		{},
		{MemoryBytes: -1},
		{Processes: -1},
		{CPUTimeMS: -1},
	} {
		if err := limits.Validate(); err == nil {
			t.Fatalf("Validate(%#v) unexpectedly succeeded", limits)
		}
	}
	if !(ResourceLimits{}).Empty() {
		t.Fatal("zero resource limits should be empty internally")
	}
	if ResourceLimitMemory != "memory" || ResourceLimitProcesses != "processes" || ResourceLimitCPUTime != "cpu_time" {
		t.Fatalf("resource limit kinds moved: memory=%q processes=%q cpu=%q", ResourceLimitMemory, ResourceLimitProcesses, ResourceLimitCPUTime)
	}
}

func TestResourceLimitsBindRequestFingerprintDeterministically(t *testing.T) {
	base := baseIntent()
	legacy := requestFingerprint(t, base)

	first := base
	first.ResourceLimits = &ResourceLimits{MemoryBytes: 64 << 20, Processes: 8}
	second := base
	second.ResourceLimits = &ResourceLimits{Processes: 8, MemoryBytes: 64 << 20}
	got := requestFingerprint(t, first)
	if got == legacy {
		t.Fatal("named resource limits folded into omitted request")
	}
	if want := requestFingerprint(t, second); got != want {
		t.Fatalf("resource fingerprint depends on field construction order: %s != %s", got, want)
	}

	// Freeze the envelope independently of the production helper so changing its
	// field names/version/order is an intentional identity migration.
	envelope := struct {
		Version int            `json:"version"`
		Kind    string         `json:"kind"`
		Base    string         `json:"base_fingerprint"`
		Limits  ResourceLimits `json:"limits"`
	}{Version: 1, Kind: "request", Base: legacy, Limits: ResourceLimits{MemoryBytes: 64 << 20, Processes: 8}}
	b, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Fatalf("resource request envelope moved:\n got %s\nwant %s", got, want)
	}
}

func TestResourceLimitsChangeRequestAndExecutionIdentity(t *testing.T) {
	memory := baseIntent()
	memory.ResourceLimits = &ResourceLimits{MemoryBytes: 64 << 20}
	processes := baseIntent()
	processes.ResourceLimits = &ResourceLimits{Processes: 8}
	memoryMore := baseIntent()
	memoryMore.ResourceLimits = &ResourceLimits{MemoryBytes: 96 << 20}

	requestDigests := map[string]struct{}{}
	executionDigests := map[string]struct{}{}
	for _, intent := range []Intent{memory, processes, memoryMore} {
		r := requestFingerprint(t, intent)
		e := executionFingerprint(t, intent)
		if _, exists := requestDigests[r]; exists {
			t.Fatalf("different resource requests collided at %s", r)
		}
		if _, exists := executionDigests[e]; exists {
			t.Fatalf("different resource executions collided at %s", e)
		}
		requestDigests[r] = struct{}{}
		executionDigests[e] = struct{}{}
	}
}

func TestResourceLimitsInvalidIntentDoesNotFingerprint(t *testing.T) {
	for _, limits := range []ResourceLimits{{}, {MemoryBytes: -1}, {Processes: -1}, {CPUTimeMS: -1}} {
		intent := baseIntent()
		intent.ResourceLimits = &limits
		if _, err := intent.RequestFingerprint(); err == nil {
			t.Fatalf("invalid request limits fingerprinted: %#v", limits)
		}
		if _, err := intent.ExecutionFingerprint("/bin/sh"); err == nil {
			t.Fatalf("invalid execution limits fingerprinted: %#v", limits)
		}
	}
}

func TestLegacyV1RejectsResourceLimits(t *testing.T) {
	intent := baseIntent()
	intent.ResourceLimits = &ResourceLimits{MemoryBytes: 64 << 20}
	if _, err := intent.Fingerprint(); err == nil {
		t.Fatal("v1 fingerprint accepted resource limits")
	}
}
