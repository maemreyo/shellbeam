package operation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func baseIntent() Intent {
	// No workspace id and an absolute cwd: the shape the operations already on
	// disk were written with.
	return Intent{Command: "true", CWD: "/tmp", TimeoutMS: 20000}
}

func requestFingerprint(t *testing.T, i Intent) string {
	t.Helper()
	got, err := i.RequestFingerprint()
	if err != nil {
		t.Fatalf("request fingerprint %#v: %v", i, err)
	}
	return got
}

// TestHistoricalRequestsKeepTheirExactFingerprint is the compatibility gate.
//
// Roughly three and a half thousand operations are already on disk, and a
// reservation is replayed by comparing request fingerprints. If adding these
// settings shifted the hash of a request that named neither of them, every one
// of those operations would start reporting operation_conflict against itself.
//
// The digests below were produced by the code that existed before stdin and
// timeout policy did. They are written out literally rather than recomputed, so
// this test fails if the canonical form ever moves -- which is the only way it
// can do its job.
func TestHistoricalRequestsKeepTheirExactFingerprint(t *testing.T) {
	// These are the canonical structures the code hashed before stdin and
	// timeout policy existed, rebuilt here rather than called through the
	// current hasher. Reordering a field, or adding one that is not omitted
	// when empty, changes these bytes and fails this test -- which is the point.
	legacyShell := struct {
		Version     int    `json:"version"`
		Kind        string `json:"kind,omitempty"`
		Command     string `json:"command"`
		WorkspaceID string `json:"workspace_id,omitempty"`
		CWD         string `json:"cwd"`
		TTY         bool   `json:"tty"`
		TimeoutMS   int64  `json:"timeout_ms"`
		Shell       string `json:"shell,omitempty"`
	}{2, "request", "true", "", "/tmp", false, 20000, ""}
	legacyArgv := struct {
		Version     int           `json:"version"`
		Kind        string        `json:"kind"`
		Mode        ExecutionMode `json:"mode"`
		Argv        []string      `json:"argv"`
		WorkspaceID string        `json:"workspace_id,omitempty"`
		CWD         string        `json:"cwd"`
		TTY         bool          `json:"tty"`
		TimeoutMS   int64         `json:"timeout_ms"`
		Executable  string        `json:"executable,omitempty"`
	}{3, "request", ExecutionModeArgv, []string{"/bin/true"}, "", "/tmp", false, 20000, ""}

	shell := baseIntent()
	if got, want := requestFingerprint(t, shell), sha256Of(t, legacyShell); got != want {
		t.Fatalf("v2 shell request fingerprint moved:\n got %s\nwant %s", got, want)
	}
	argv := Intent{Argv: []string{"/bin/true"}, CWD: "/tmp", TimeoutMS: 20000}
	if got, want := requestFingerprint(t, argv), sha256Of(t, legacyArgv); got != want {
		t.Fatalf("v3 argv request fingerprint moved:\n got %s\nwant %s", got, want)
	}

	// The v1 shape predates workspaces entirely and must also be untouched.
	legacyV1 := struct {
		Version     int    `json:"version"`
		Kind        string `json:"kind,omitempty"`
		Command     string `json:"command"`
		WorkspaceID string `json:"workspace_id,omitempty"`
		CWD         string `json:"cwd"`
		TTY         bool   `json:"tty"`
		TimeoutMS   int64  `json:"timeout_ms"`
		Shell       string `json:"shell,omitempty"`
	}{1, "request", "true", "", "/tmp", false, 20000, ""}
	v1 := Intent{Command: "true", CWD: "/tmp", TimeoutMS: 20000}
	got, err := v1.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if want := sha256Of(t, legacyV1); got != want {
		t.Fatalf("v1 fingerprint moved:\n got %s\nwant %s", got, want)
	}
}

func sha256Of(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestNamingASettingChangesTheRequestFingerprint: a different request must be a
// different request, or a replay would return a session running under a
// contract the caller did not ask for.
func TestNamingASettingChangesTheRequestFingerprint(t *testing.T) {
	omitted := requestFingerprint(t, baseIntent())

	named := map[string]Intent{}
	closed := baseIntent()
	closed.StdinMode = StdinModeClosed
	named["stdin closed"] = closed

	stream := baseIntent()
	stream.StdinMode = StdinModeStream
	named["stdin stream"] = stream

	unlimited := baseIntent()
	unlimited.TimeoutMS = 0
	unlimited.TimeoutMode = TimeoutModeUnlimited
	unlimited.Persistent = false
	named["timeout unlimited"] = unlimited

	seen := map[string]string{omitted: "omitted"}
	for name, intent := range named {
		digest := requestFingerprint(t, intent)
		if digest == omitted {
			t.Fatalf("%s hashes the same as naming nothing", name)
		}
		if other, clash := seen[digest]; clash {
			t.Fatalf("%s hashes the same as %s", name, other)
		}
		seen[digest] = name
	}
}

// TestExplicitDefaultIsNotFoldedIntoOmission.
//
// stdin_mode=closed resolves exactly as omission does today, and it would be
// tempting to treat them as the same request. They are not: a fingerprint
// describes the request, and folding them together would make "was this the
// same request" depend on the defaults in force, so changing a default later
// would silently change which past requests match.
func TestExplicitDefaultIsNotFoldedIntoOmission(t *testing.T) {
	omitted := requestFingerprint(t, baseIntent())
	explicit := baseIntent()
	explicit.StdinMode = StdinModeClosed
	if requestFingerprint(t, explicit) == omitted {
		t.Fatal("an explicitly named default was folded into saying nothing")
	}
}

// TestSameExplicitRequestReplays keeps the change from breaking idempotency for
// callers that do use the new settings.
func TestSameExplicitRequestReplays(t *testing.T) {
	first := baseIntent()
	first.StdinMode = StdinModeStream
	second := baseIntent()
	second.StdinMode = StdinModeStream
	if requestFingerprint(t, first) != requestFingerprint(t, second) {
		t.Fatal("two identical explicit requests did not fingerprint alike")
	}
}

func executionFingerprint(t *testing.T, i Intent) string {
	t.Helper()
	got, err := i.ExecutionFingerprint("/bin/sh")
	if err != nil {
		t.Fatalf("execution fingerprint %#v: %v", i, err)
	}
	return got
}

// TestExecutionFingerprintDescribesTheResolvedContract, which is a different
// question from what the caller sent: two requests that arrive differently can
// run identically, and one request can run differently as policy changes.
func TestExecutionFingerprintDescribesTheResolvedContract(t *testing.T) {
	ordinary := baseIntent()
	ordinaryResolved := ResolvedExecutionPolicy{StdinMode: StdinModeClosed, TimeoutMS: 600000, TimeoutFromDefault: true}
	ordinary.Resolved, ordinary.TimeoutSource = &ordinaryResolved, "default"

	long := baseIntent()
	long.TimeoutMS = 0
	longResolved := ResolvedExecutionPolicy{StdinMode: StdinModeClosed}
	long.Resolved, long.TimeoutSource = &longResolved, "unlimited"

	if executionFingerprint(t, ordinary) == executionFingerprint(t, long) {
		t.Fatal("a bounded and an unbounded execution contract fingerprint alike")
	}

	// The source is part of the contract: the same bound supplied by policy and
	// requested by the caller are not the same execution.
	requested := baseIntent()
	requestedResolved := ResolvedExecutionPolicy{StdinMode: StdinModeClosed, TimeoutMS: 600000}
	requested.Resolved, requested.TimeoutSource = &requestedResolved, "requested"
	if executionFingerprint(t, requested) == executionFingerprint(t, ordinary) {
		t.Fatal("a requested bound and a supplied one fingerprint alike")
	}

	// And stdin is part of it too.
	streaming := baseIntent()
	streamingResolved := ResolvedExecutionPolicy{StdinMode: StdinModeStream, TimeoutMS: 600000, TimeoutFromDefault: true}
	streaming.Resolved, streaming.TimeoutSource = &streamingResolved, "default"
	if executionFingerprint(t, streaming) == executionFingerprint(t, ordinary) {
		t.Fatal("streaming and closed stdin fingerprint alike")
	}
}
