package environment

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentFingerprintDeterministicAndOrderIndependent(t *testing.T) {
	path := PathFingerprint("/usr/bin:/bin:")
	first := FingerprintInput{
		Platform:  Platform{OS: "darwin", Architecture: "arm64"},
		Execution: ExecutionContext{Mode: "shell", Identity: "/bin/zsh"},
		Path:      path,
		VariablePresence: []VariablePresence{
			{Name: "TERM", Present: true},
			{Name: "CI", Present: false},
		},
		ToolchainManager: &ToolchainManager{Kind: "declared", Identity: "asdf|volta"},
	}
	second := first
	second.VariablePresence = []VariablePresence{
		{Name: "CI", Present: false},
		{Name: "TERM", Present: true},
	}
	a, err := EnvironmentFingerprint(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := EnvironmentFingerprint(second)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("fingerprints differ for equivalent normalized facts: %q != %q", a, b)
	}

	changed := second
	changed.VariablePresence = []VariablePresence{
		{Name: "CI", Present: true},
		{Name: "TERM", Present: true},
	}
	c, err := EnvironmentFingerprint(changed)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("changed relevant presence did not change fingerprint")
	}
}

func TestPathFingerprintIsDeterministicAndDoesNotExposeRawPath(t *testing.T) {
	got := PathFingerprint("/usr/bin:/bin:")
	again := PathFingerprint("/usr/bin:/bin:")
	if got != again {
		t.Fatalf("path fingerprint unstable: %#v != %#v", got, again)
	}
	if got.EntryCount != 3 {
		t.Fatalf("entry_count=%d want=3", got.EntryCount)
	}
	empty := PathFingerprint("")
	if empty.EntryCount != 0 {
		t.Fatalf("empty entry_count=%d want=0", empty.EntryCount)
	}
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "/usr/bin") || strings.Contains(string(data), "/bin") {
		t.Fatalf("raw path leaked in observation: %s", data)
	}
	if len(got.Digest) != 64 {
		t.Fatalf("digest=%q", got.Digest)
	}
}

func TestPresenceAndPathTypesCannotRepresentRawValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  reflect.Type
	}{
		{"presence", reflect.TypeOf(VariablePresence{})},
		{"path", reflect.TypeOf(PathObservation{})},
	} {
		for i := 0; i < tc.typ.NumField(); i++ {
			name := strings.ToLower(tc.typ.Field(i).Name)
			if strings.Contains(name, "value") || strings.Contains(name, "raw") {
				t.Fatalf("%s exposes unsafe field %q", tc.name, tc.typ.Field(i).Name)
			}
		}
	}
}

func TestToolchainFingerprintSortedAndIgnoresDiagnosticCode(t *testing.T) {
	first := []ToolchainObservation{
		{Kind: "node", RequestedIdentity: "node", ObservedIdentity: "node", Version: "v24.1.0", Quality: ProbeComplete, DiagnosticCode: "first"},
		{Kind: "go", RequestedIdentity: "go", ObservedIdentity: "go", Version: "go1.26.5", Quality: ProbeComplete},
	}
	second := []ToolchainObservation{
		{Kind: "go", RequestedIdentity: "go", ObservedIdentity: "go", Version: "go1.26.5", Quality: ProbeComplete},
		{Kind: "node", RequestedIdentity: "node", ObservedIdentity: "node", Version: "v24.1.0", Quality: ProbeComplete, DiagnosticCode: "different"},
	}
	a, err := ToolchainFingerprint(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ToolchainFingerprint(second)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("diagnostic/order changed fingerprint: %q != %q", a, b)
	}
	second[1].Version = "v24.2.0"
	c, err := ToolchainFingerprint(second)
	if err != nil {
		t.Fatal(err)
	}
	if c == a {
		t.Fatal("toolchain version change did not change fingerprint")
	}
}

func TestBindingCompatibilityRequiresMatchingVersions(t *testing.T) {
	base := Binding{
		SnapshotID:                    "env_" + strings.Repeat("a", 64),
		EnvironmentFingerprint:        strings.Repeat("b", 64),
		EnvironmentFingerprintVersion: FingerprintVersion,
		ToolchainFingerprint:          strings.Repeat("c", 64),
		ToolchainFingerprintVersion:   ToolchainFingerprintVersion,
		CapturedAt:                    time.Now().UTC(),
	}
	if !base.CompatibleWith(base) {
		t.Fatal("same versioned binding is not compatible")
	}
	other := base
	other.EnvironmentFingerprintVersion++
	if base.CompatibleWith(other) {
		t.Fatal("incompatible environment fingerprint versions compared as compatible")
	}
	other = base
	other.ToolchainFingerprintVersion++
	if base.CompatibleWith(other) {
		t.Fatal("incompatible toolchain fingerprint versions compared as compatible")
	}
}

func TestSnapshotValidateRejectsMalformedAndOversizedFacts(t *testing.T) {
	now := time.Now().UTC()
	path := PathFingerprint("/bin")
	fp, err := EnvironmentFingerprint(FingerprintInput{
		Platform:  Platform{OS: "linux", Architecture: "amd64"},
		Execution: ExecutionContext{Mode: "shell", Identity: "/bin/sh"},
		Path:      path,
	})
	if err != nil {
		t.Fatal(err)
	}
	tfp, err := ToolchainFingerprint([]ToolchainObservation{
		{Kind: "go", RequestedIdentity: "go", ObservedIdentity: "go", Version: "go1.26.5", Quality: ProbeComplete},
	})
	if err != nil {
		t.Fatal(err)
	}
	valid := Snapshot{
		SchemaVersion:               SnapshotSchemaVersion,
		SnapshotID:                  "env_" + strings.Repeat("d", 64),
		CapturedAt:                  now,
		Quality:                     QualityComplete,
		EnvironmentFingerprint:      fp,
		FingerprintVersion:          FingerprintVersion,
		ToolchainFingerprint:        tfp,
		ToolchainFingerprintVersion: ToolchainFingerprintVersion,
		Platform:                    Platform{OS: "linux", Architecture: "amd64"},
		Execution:                   ExecutionContext{Mode: "shell", Identity: "/bin/sh"},
		Path:                        path,
		Toolchains: []ToolchainObservation{
			{Kind: "go", RequestedIdentity: "go", ObservedIdentity: "go", Version: "go1.26.5", Quality: ProbeComplete},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}

	bad := valid
	bad.EnvironmentFingerprint = "not-a-digest"
	if err := bad.Validate(); err == nil {
		t.Fatal("malformed environment fingerprint accepted")
	}

	bad = valid
	bad.VariablePresence = make([]VariablePresence, MaxRelevantVariables+1)
	for i := range bad.VariablePresence {
		bad.VariablePresence[i] = VariablePresence{Name: "X" + strings.Repeat("A", i%4), Present: true}
	}
	if err := bad.Validate(); err == nil {
		t.Fatal("oversized variable presence accepted")
	}

	bad = valid
	bad.Quality = "excellent"
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown quality accepted")
	}
}

func TestToolchainFingerprintAcceptsBoundedUnsupportedUnavailableObservation(t *testing.T) {
	observations := []ToolchainObservation{
		{Kind: "go", RequestedIdentity: "host", ObservedIdentity: "/usr/bin/go", Version: "go1.26.5", Quality: ProbeComplete},
		{Kind: "ruby", RequestedIdentity: "version=3.4", Quality: ProbeUnavailable, DiagnosticCode: "toolchain_probe_unsupported"},
	}
	if _, err := ToolchainFingerprint(observations); err != nil {
		t.Fatalf("unsupported unavailable observation rejected: %v", err)
	}
	bad := append([]ToolchainObservation(nil), observations...)
	bad[1] = ToolchainObservation{Kind: "ruby", RequestedIdentity: "version=3.4", ObservedIdentity: "/usr/bin/ruby", Version: "3.4", Quality: ProbeComplete}
	if _, err := ToolchainFingerprint(bad); err == nil {
		t.Fatal("unsupported complete observation accepted")
	}
	oversized := make([]ToolchainObservation, MaxToolchainObservations+1)
	for i := range oversized {
		oversized[i] = ToolchainObservation{Kind: fmt.Sprintf("unsupported_%02d", i), RequestedIdentity: "declared", Quality: ProbeUnavailable, DiagnosticCode: "toolchain_probe_unsupported"}
	}
	if _, err := ToolchainFingerprint(oversized); err == nil {
		t.Fatal("oversized toolchain observation set accepted")
	}
}
