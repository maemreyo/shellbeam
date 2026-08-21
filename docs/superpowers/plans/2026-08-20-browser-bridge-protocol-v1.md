# Browser Bridge Protocol V1 — ShellBeam Host Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the ShellBeam half of the Browser Continuity Attention Router: a connectionless, read-only native-messaging host exposing five closed verbs backed by fixed read plans, plus an explicitly installed Firefox manifest and a doctor check — with no new ShellBeam core authority, projection, or durable identity.

**Architecture:** A pure protocol package (`internal/core/browserbridge`) defines the closed verb enum, request/response types, bounds, and validation with no I/O. An application package (`internal/app/browserbridge`) executes one fixed read plan per verb against a narrow `DaemonReader` port and projects literal facts with authority, freshness, and coverage preserved. A separate tiny binary (`cmd/shellbeam-browser-host`) reads exactly one JSON message from stdin, runs one plan, writes one bounded JSON message, and exits; Firefox execs it with no arguments, which is why it is its own binary rather than a subcommand. The main binary gains `browser-host install|uninstall` for the native manifest and a `browser_bridge` doctor check.

**Tech Stack:** Go 1.26.6; stdlib `encoding/json`, `os`, `io`, `path/filepath`, `runtime`, `time`; existing `internal/adapter/ipc` HTTP-over-unix-socket client; existing `inspect.activity`, `inspect.sessions`, `inspect.events`, `inspect.verification`, `inspect.structured` daemon reads. No new runtime dependency. No changes to `internal/core` outside the new `browserbridge` package.

**Spec:** `docs/superpowers/specs/2026-08-20-browser-continuity-attention-router-design.md`

## Global Constraints

- Execution base is `ef89f27160baf43dd07facd85f7652765b278725` (`origin/main`, 2026-08-21), with the Browser Router design/docs replayed directly on top. The previous `12dc39257b4b76cfdbf6eeb95706335f2aee409b` snapshot is superseded. The current base contains 82 Go packages; re-establish a fresh `go test ./... -count=1` baseline before Task 1 and treat any failure as a blocker rather than as noise.
- TDD is mandatory: focused RED → minimal GREEN → focused regression → commit. Never write production code first and backfill tests.
- Revalidation on 2026-08-21 found the intervening `structuredresult`/IPC changes to be additive for this plan: `ipc.Client.CallV2`, `RequestV2.OperationID`, typed `RequestV2.TestStatus`, `RequestV2.MaxRecords`, `ResponseV2.Structured *structuredapp.InspectResult`, and the Task 6 `Producer`/`Completeness`/`Summary`/`Records` fields remain available. No Browser Bridge read-plan redesign is required.
- This plan implements the ShellBeam repository deliverables only (spec §26). The Firefox extension is a separate repository with its own plan; do not add TypeScript, web-extension tooling, or browser assets to this repo.
- **No ShellBeam core change outside the new package.** Do not add fields to `RequestV2`/`ResponseV2`, do not add MCP actions, do not add IPC actions, do not touch `internal/core/activity`, `internal/core/verification`, `internal/core/structuredresult`, or `internal/core/persistentsession`. The host is a client of existing reads (spec §27, I20).
- Keep one MCP tool, `local_shell`. The browser bridge is not an MCP surface and must not appear in the capability catalog.
- **`internal/app/bridge.Handler` SHALL NOT be reused or imported by any new package.** It forwards a caller-supplied action and would expose the entire ShellBeam surface including execution (spec §19).
- The verb set is closed and exactly: `hello`, `activity_facts`, `activity_events`, `verification_facts`, `structured_failure_facts`. No `action` field, no `argv`, no `command`, no `cwd`, no path, no caller-chosen session/operation/workspace id anywhere in the protocol (I12, I23).
- Every id after the caller's single `correlation_id` is derived by the host from the activity record. A caller can never name an operation, session, workspace, or path (I23).
- Responses carry literal facts only. No field may encode a policy judgment: `mechanical_blockers`, `should_continue`, `needs_attention`, `task_complete`, `work_complete`, `safe_to_finish`, `is_stuck`, `blocked` are all forbidden as response field names or values (I1, I11, spec §19).
- Session counts use ShellBeam's own `persistentsession.Lifecycle` names — `provisioning`, `live`, `terminal`, `lost`. Never rename them to `running`/`finalizing` (spec §19).
- `verification_facts` returns a per-workspace list and never aggregates across workspaces (spec §20, I11).
- Response bounds: target ≤ 65536 bytes (`TargetResponseBytes`), hard cap ≤ 262144 bytes (`MaxResponseBytes`). Native messaging caps app→extension messages at 1 MB; that is a transport failure threshold, not the API budget. Truncation is always explicit in `coverage` (spec §19).
- `hello` decoding is lenient about unknown request fields so a future host still answers an older `hello` and version mismatch stays distinguishable from a broken host. Every other verb decodes strictly with `DisallowUnknownFields` (spec §19).
- The host is stateless: no cursors, no watch state, no conversation identity, no session, no cache, no files written. It reads one message, writes one message, exits (spec §20).
- The host never receives or stores conversation content, transcript text, prompts, or model output.
- `shellbeam install` SHALL NOT write a native messaging manifest. Manifest installation is a separate, explicit, revocable command (I21, spec §21).
- The manifest pins `allowed_extensions` to exactly one extension id supplied by the operator; there is no default and no wildcard (spec §21).
- Production files target 150–300 lines, review above 350, hard cap 500; test files review above 600, hard cap 800; functions review above 60, hard cap 80.
- Broad gates: `go run ./tools/devctl check`, `go run ./tools/devctl test --dirty --base origin/main`, then one deliberate final `go test ./... -count=1`.
- No push and no PR in this plan unless explicitly requested.

## File Structure

**Create:**

- `internal/core/browserbridge/protocol.go` — closed verb enum, `Request`, `Response`, `Status`, fact types, bounds constants, validation. Pure types, no I/O, no daemon imports.
- `internal/core/browserbridge/protocol_test.go`
- `internal/core/browserbridge/bound.go` — `BoundResponse` marshals and, when over budget, degrades content and marks `coverage.truncated` with a reason.
- `internal/core/browserbridge/bound_test.go`
- `internal/app/browserbridge/ports.go` — the narrow `DaemonReader` port the read plans consume.
- `internal/app/browserbridge/facts_activity.go` — `activity_facts` and `activity_events` plans.
- `internal/app/browserbridge/facts_activity_test.go`
- `internal/app/browserbridge/facts_verification.go` — `verification_facts` plan, per-workspace.
- `internal/app/browserbridge/facts_verification_test.go`
- `internal/app/browserbridge/facts_structured.go` — `structured_failure_facts` bounded walk.
- `internal/app/browserbridge/facts_structured_test.go`
- `internal/app/browserbridge/host.go` — verb dispatch, `hello`, one-message stdio loop.
- `internal/app/browserbridge/host_test.go`
- `internal/app/browserbridge/framing.go` — native-messaging 32-bit native-byte-order length framing.
- `internal/app/browserbridge/framing_test.go`
- `internal/app/browserbridge/manifest.go` — native manifest render, install path resolution, write, remove.
- `internal/app/browserbridge/manifest_test.go`
- `internal/adapter/browserbridge/daemon_reader.go` — adapts `ipc.Client` to `DaemonReader`.
- `cmd/shellbeam-browser-host/main.go` — the host binary Firefox execs.
- `cmd/shellbeam/command_browser_host.go` — `browser-host install|uninstall`.
- `tests/contract/browser_bridge_boundary_test.go` — closed-enum, no-passthrough, no-judgment-field, bounds conformance.

**Modify:**

- `cmd/shellbeam/command.go` — add `browser-host` dispatch and extend `topLevelUsage`.
- `cmd/shellbeam/doctor.go` — add the `browser_bridge` check.

Boundary rule for this decomposition: `internal/core/browserbridge` imports nothing from `internal/app` or `internal/adapter`; `internal/app/browserbridge` imports core packages and its own port but never `internal/adapter/ipc`; only `internal/adapter/browserbridge` and the two `cmd` packages know the transport exists.

---

### Task 1: Protocol types and closed verb enum

**Files:**
- Create: `internal/core/browserbridge/protocol.go`
- Test: `internal/core/browserbridge/protocol_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `ProtocolVersion int = 1`; `SupportedVersions() []int`; `TargetResponseBytes`, `MaxResponseBytes`, `MaxActivityEvents`, `MaxFailingCasesPerOperation`, `MaxStructuredOperations`, `MaxVerificationWorkspaces` constants; `Verb` string type with `VerbHello`, `VerbActivityFacts`, `VerbActivityEvents`, `VerbVerificationFacts`, `VerbStructuredFailureFacts`; `Status` string type with `StatusOK`, `StatusFactsUnavailable`, `StatusDaemonUnreachable`, `StatusProtocolIncompatible`, `StatusInvalidRequest`; `Request{Verb, CorrelationID, Cursor, ProtocolVersion}`; `(Request).Validate() error`; `Coverage{HistoricalOperations, CompactedOperations, Truncated, TruncationReason}`; `SessionFacts{Provisioning, Live, Terminal, Lost}`; `ActivityFacts`; `EventFacts`; `WorkspaceVerification`; `OperationFailureFacts`; `FailingCase`; `Response`.

- [ ] **Step 1: Write the failing test**

```go
package browserbridge

import "testing"

func TestRequestValidateAcceptsExactlyTheClosedVerbSet(t *testing.T) {
	for _, verb := range []Verb{VerbActivityFacts, VerbActivityEvents, VerbVerificationFacts, VerbStructuredFailureFacts} {
		req := Request{ProtocolVersion: ProtocolVersion, Verb: verb, CorrelationID: "chatgpt-wt-01"}
		if err := req.Validate(); err != nil {
			t.Fatalf("verb %q rejected: %v", verb, err)
		}
	}
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbHello}).Validate(); err != nil {
		t.Fatalf("hello rejected: %v", err)
	}
}

func TestRequestValidateRejectsUnknownVerbAndActionLikeInput(t *testing.T) {
	for _, verb := range []Verb{"", "start", "inspect.activity", "write", "kill", "read_media"} {
		req := Request{ProtocolVersion: ProtocolVersion, Verb: verb, CorrelationID: "chatgpt-wt-01"}
		if err := req.Validate(); err == nil {
			t.Fatalf("verb %q accepted", verb)
		}
	}
}

func TestRequestValidateEnforcesPerVerbFields(t *testing.T) {
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbActivityFacts}).Validate(); err == nil {
		t.Fatal("missing correlation_id accepted")
	}
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbHello, CorrelationID: "x"}).Validate(); err == nil {
		t.Fatal("hello with correlation_id accepted")
	}
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbActivityFacts, CorrelationID: "x", Cursor: "c"}).Validate(); err == nil {
		t.Fatal("cursor accepted on a verb that has no cursor")
	}
	if err := (Request{ProtocolVersion: ProtocolVersion, Verb: VerbActivityEvents, CorrelationID: "x", Cursor: "c"}).Validate(); err != nil {
		t.Fatalf("cursor rejected on activity_events: %v", err)
	}
}

func TestRequestValidateRejectsIncompatibleProtocolVersion(t *testing.T) {
	if err := (Request{ProtocolVersion: 99, Verb: VerbActivityFacts, CorrelationID: "x"}).Validate(); err == nil {
		t.Fatal("unsupported protocol version accepted")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/browserbridge/ -run TestRequest -v`
Expected: FAIL — package does not compile, `undefined: Request`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package browserbridge defines the Browser Bridge Protocol v1 wire contract.
//
// The protocol is deliberately not a ShellBeam action passthrough. A caller
// names one verb from a closed enum plus at most one correlation id, and the
// host maps that verb to a fixed read plan whose every other identifier is
// derived from the activity record. See the design's sections 19 and 20.
package browserbridge

import (
	"fmt"
	"time"
	"unicode/utf8"
)

const ProtocolVersion = 1

const (
	TargetResponseBytes         = 65536
	MaxResponseBytes            = 262144
	MaxActivityEvents           = 64
	MaxStructuredOperations     = 8
	MaxFailingCasesPerOperation = 16
	MaxVerificationWorkspaces   = 4
	MaxCorrelationIDBytes       = 128
	MaxCursorBytes              = 512
)

func SupportedVersions() []int { return []int{ProtocolVersion} }

type Verb string

const (
	VerbHello                  Verb = "hello"
	VerbActivityFacts          Verb = "activity_facts"
	VerbActivityEvents         Verb = "activity_events"
	VerbVerificationFacts      Verb = "verification_facts"
	VerbStructuredFailureFacts Verb = "structured_failure_facts"
)

type Status string

const (
	StatusOK                   Status = "ok"
	StatusFactsUnavailable     Status = "facts_unavailable"
	StatusDaemonUnreachable    Status = "daemon_unreachable"
	StatusProtocolIncompatible Status = "protocol_incompatible"
	StatusInvalidRequest       Status = "invalid_request"
)

type Request struct {
	ProtocolVersion int    `json:"protocol_version"`
	Verb            Verb   `json:"verb"`
	CorrelationID   string `json:"correlation_id,omitempty"`
	Cursor          string `json:"cursor,omitempty"`
}

func (r Request) Validate() error {
	if r.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version")
	}
	switch r.Verb {
	case VerbHello:
		if r.CorrelationID != "" || r.Cursor != "" {
			return fmt.Errorf("hello takes no selectors")
		}
		return nil
	case VerbActivityFacts, VerbVerificationFacts, VerbStructuredFailureFacts:
		if r.Cursor != "" {
			return fmt.Errorf("verb does not accept a cursor")
		}
	case VerbActivityEvents:
		if len(r.Cursor) > MaxCursorBytes || !utf8.ValidString(r.Cursor) {
			return fmt.Errorf("invalid cursor")
		}
	default:
		return fmt.Errorf("unknown verb")
	}
	if r.CorrelationID == "" || len(r.CorrelationID) > MaxCorrelationIDBytes || !utf8.ValidString(r.CorrelationID) {
		return fmt.Errorf("invalid correlation_id")
	}
	return nil
}

type Coverage struct {
	HistoricalOperations string `json:"historical_operations,omitempty"`
	CompactedOperations  int    `json:"compacted_operations"`
	Truncated            bool   `json:"truncated"`
	TruncationReason     string `json:"truncation_reason,omitempty"`
}

type SessionFacts struct {
	Provisioning int `json:"provisioning"`
	Live         int `json:"live"`
	Terminal     int `json:"terminal"`
	Lost         int `json:"lost"`
}

type ActivityFacts struct {
	Found              bool         `json:"found"`
	LatestOperationAt  *time.Time   `json:"latest_operation_at,omitempty"`
	OperationsRetained int          `json:"operations_retained"`
	WorkspaceIDs       []string     `json:"workspace_ids,omitempty"`
	Sessions           SessionFacts `json:"sessions"`
	SessionsTruncated  bool         `json:"sessions_truncated,omitempty"`
}

type EventFacts struct {
	Returned  int      `json:"returned"`
	Kinds     []string `json:"kinds,omitempty"`
	Cursor    string   `json:"cursor,omitempty"`
	LatestAt  *time.Time `json:"latest_at,omitempty"`
}

type WorkspaceVerification struct {
	WorkspaceID      string `json:"workspace_id"`
	PolicyState      string `json:"policy_state"`
	GateStatus       string `json:"gate_status"`
	SourceGeneration string `json:"source_generation,omitempty"`
	Satisfied        int    `json:"satisfied"`
	Waived           int    `json:"waived"`
	Blocking         int    `json:"blocking"`
	Indeterminate    int    `json:"indeterminate"`
}

type FailingCase struct {
	Name    string `json:"name"`
	Package string `json:"package,omitempty"`
	Status  string `json:"status"`
}

type OperationFailureFacts struct {
	OperationID      string        `json:"operation_id"`
	AdapterID        string        `json:"adapter_id,omitempty"`
	AdapterVersion   int           `json:"adapter_version,omitempty"`
	Authority        string        `json:"authority,omitempty"`
	DerivationMethod string        `json:"derivation_method,omitempty"`
	Completeness     string        `json:"completeness,omitempty"`
	TestPassed       int           `json:"test_passed"`
	TestFailed       int           `json:"test_failed"`
	Errors           int           `json:"errors"`
	FailingCases     []FailingCase `json:"failing_cases,omitempty"`
	CasesTruncated   bool          `json:"cases_truncated,omitempty"`
}

type Response struct {
	ProtocolVersion   int                     `json:"protocol_version"`
	SupportedVersions []int                   `json:"supported_versions"`
	Verb              Verb                    `json:"verb"`
	Status            Status                  `json:"status"`
	Reason            string                  `json:"reason,omitempty"`
	Activity          *ActivityFacts          `json:"activity,omitempty"`
	Events            *EventFacts             `json:"events,omitempty"`
	Verification      []WorkspaceVerification `json:"verification,omitempty"`
	Structured        []OperationFailureFacts `json:"structured,omitempty"`
	Coverage          Coverage                `json:"coverage"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/browserbridge/ -run TestRequest -v`
Expected: PASS, five tests.

- [ ] **Step 5: Commit**

```bash
git add internal/core/browserbridge/protocol.go internal/core/browserbridge/protocol_test.go
git commit -m "feat: define browser bridge protocol v1 contract"
```

---

### Task 2: Explicit response bounding

**Files:**
- Create: `internal/core/browserbridge/bound.go`
- Test: `internal/core/browserbridge/bound_test.go`

**Interfaces:**
- Consumes: `Response`, `Coverage`, `TargetResponseBytes`, `MaxResponseBytes` from Task 1.
- Produces: `BoundResponse(Response) ([]byte, error)` — marshals a response, and when the encoding exceeds `TargetResponseBytes` drops the heaviest optional content in a fixed order (failing cases, then event kinds, then structured entries beyond the first) and records `coverage.truncated=true` with a `truncation_reason`. Never returns bytes longer than `MaxResponseBytes`.

- [ ] **Step 1: Write the failing test**

```go
package browserbridge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBoundResponseLeavesSmallResponsesIntact(t *testing.T) {
	in := Response{ProtocolVersion: ProtocolVersion, SupportedVersions: SupportedVersions(), Verb: VerbActivityFacts, Status: StatusOK, Activity: &ActivityFacts{Found: true, OperationsRetained: 3}}
	raw, err := BoundResponse(in)
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Coverage.Truncated {
		t.Fatal("small response marked truncated")
	}
	if out.Activity == nil || out.Activity.OperationsRetained != 3 {
		t.Fatal("content lost")
	}
}

func TestBoundResponseTruncatesExplicitlyAndStaysUnderHardCap(t *testing.T) {
	big := Response{ProtocolVersion: ProtocolVersion, SupportedVersions: SupportedVersions(), Verb: VerbStructuredFailureFacts, Status: StatusOK}
	for i := 0; i < MaxStructuredOperations; i++ {
		entry := OperationFailureFacts{OperationID: "op-" + strings.Repeat("x", 32), TestFailed: 40}
		for j := 0; j < MaxFailingCasesPerOperation; j++ {
			entry.FailingCases = append(entry.FailingCases, FailingCase{Name: strings.Repeat("case", 512), Package: strings.Repeat("pkg", 512), Status: "fail"})
		}
		big.Structured = append(big.Structured, entry)
	}
	raw, err := BoundResponse(big)
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	if len(raw) > MaxResponseBytes {
		t.Fatalf("response %d exceeds hard cap %d", len(raw), MaxResponseBytes)
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Coverage.Truncated || out.Coverage.TruncationReason == "" {
		t.Fatal("truncation was silent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/core/browserbridge/ -run TestBoundResponse -v`
Expected: FAIL — `undefined: BoundResponse`.

- [ ] **Step 3: Write minimal implementation**

```go
package browserbridge

import "encoding/json"

// BoundResponse encodes a response within the protocol budget.
//
// Truncation is always recorded. A silently shortened response would let the
// extension read a partial fact set as a complete one, which the design
// forbids: coverage travels with the facts rather than being inferred.
func BoundResponse(resp Response) ([]byte, error) {
	raw, err := json.Marshal(resp)
	if err != nil {
		return nil, err
	}
	if len(raw) <= TargetResponseBytes {
		return raw, nil
	}
	for _, step := range []struct {
		reason string
		apply  func(*Response)
	}{
		{"failing_cases_dropped", func(r *Response) {
			for i := range r.Structured {
				r.Structured[i].FailingCases = nil
				r.Structured[i].CasesTruncated = true
			}
		}},
		{"event_kinds_dropped", func(r *Response) {
			if r.Events != nil {
				r.Events.Kinds = nil
			}
		}},
		{"structured_entries_dropped", func(r *Response) {
			if len(r.Structured) > 1 {
				r.Structured = r.Structured[:1]
			}
		}},
	} {
		step.apply(&resp)
		resp.Coverage.Truncated = true
		resp.Coverage.TruncationReason = step.reason
		if raw, err = json.Marshal(resp); err != nil {
			return nil, err
		}
		if len(raw) <= TargetResponseBytes {
			return raw, nil
		}
	}
	if len(raw) > MaxResponseBytes {
		minimal := Response{ProtocolVersion: resp.ProtocolVersion, SupportedVersions: resp.SupportedVersions, Verb: resp.Verb, Status: resp.Status, Coverage: Coverage{Truncated: true, TruncationReason: "response_exceeded_hard_cap"}}
		return json.Marshal(minimal)
	}
	return raw, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/core/browserbridge/ -count=1 -v`
Expected: PASS, all six tests in the package.

- [ ] **Step 5: Commit**

```bash
git add internal/core/browserbridge/bound.go internal/core/browserbridge/bound_test.go
git commit -m "feat: bound browser bridge responses with explicit truncation"
```

---

### Task 3: Daemon port and the activity_facts read plan

**Files:**
- Create: `internal/app/browserbridge/ports.go`
- Create: `internal/app/browserbridge/facts_activity.go`
- Test: `internal/app/browserbridge/facts_activity_test.go`

**Interfaces:**
- Consumes: `browserbridge.Request/Response/ActivityFacts/SessionFacts/Coverage` from Task 1.
- Produces: `type DaemonReader interface { Read(ctx context.Context, req ipc.RequestV2) (ipc.ResponseV2, error) }`; `type Planner struct{ reader DaemonReader }`; `NewPlanner(DaemonReader) *Planner`; `(*Planner).ActivityFacts(ctx context.Context, correlationID string) protocol.Response`.

Note on the port: it takes `ipc.RequestV2` deliberately, so the read plans stay in one package and the adapter layer adds no translation of its own. The port is *not* exported beyond this package's constructor, and the plans only ever construct requests whose action is one of the five reads named in this plan.

- [ ] **Step 1: Write the failing test**

```go
package browserbridge

import (
	"context"
	"testing"
	"time"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

type fakeReader struct {
	seen []ipc.RequestV2
	byAction map[string]ipc.ResponseV2
	err  error
}

func (f *fakeReader) Read(_ context.Context, req ipc.RequestV2) (ipc.ResponseV2, error) {
	f.seen = append(f.seen, req)
	if f.err != nil {
		return ipc.ResponseV2{}, f.err
	}
	return f.byAction[req.Action], nil
}

func TestActivityFactsComposesActivityAndSessionsAndDerivesEveryID(t *testing.T) {
	observed := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	reader := &fakeReader{byAction: map[string]ipc.ResponseV2{
		"inspect.activity": {OK: true, Activity: &activitycore.Activity{
			ID: "chatgpt-wt-01", WorkspaceIDs: []workspace.WorkspaceID{"ws-1"},
			Operations:          []activitycore.OperationRef{{OperationID: "op-1", SessionID: "s-1", ObservedAt: observed}},
			CompactedOperations: 12,
		}},
		"inspect.sessions": {OK: true, Sessions: &persistent.InspectPage{Sessions: []persistent.Summary{
			{SessionID: "s-1", State: string(persistent.LifecycleLive)},
			{SessionID: "s-2", State: string(persistent.LifecycleTerminal)},
		}}},
	}}
	resp := NewPlanner(reader).ActivityFacts(context.Background(), "chatgpt-wt-01")

	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if resp.Activity == nil || !resp.Activity.Found {
		t.Fatal("activity not reported found")
	}
	if resp.Activity.Sessions.Live != 1 || resp.Activity.Sessions.Terminal != 1 {
		t.Fatalf("session counts = %+v", resp.Activity.Sessions)
	}
	if resp.Coverage.CompactedOperations != 12 || resp.Coverage.HistoricalOperations != "partial" {
		t.Fatalf("coverage = %+v", resp.Coverage)
	}
	if len(reader.seen) != 2 {
		t.Fatalf("expected two reads, got %d", len(reader.seen))
	}
	for _, req := range reader.seen {
		if req.Command != "" || len(req.Argv) != 0 || req.CWD != "" || req.SessionID != "" || req.OperationID != "" {
			t.Fatalf("read plan leaked an execution or caller-named selector: %+v", req)
		}
		if req.ActivityID != "chatgpt-wt-01" {
			t.Fatalf("read not scoped to the correlation id: %+v", req)
		}
	}
}

func TestActivityFactsReportsFactsUnavailableWhenActivityMissing(t *testing.T) {
	reader := &fakeReader{byAction: map[string]ipc.ResponseV2{
		"inspect.activity": {OK: false, Error: &ipc.Error{Code: "activity_not_found"}},
	}}
	resp := NewPlanner(reader).ActivityFacts(context.Background(), "chatgpt-wt-01")
	if resp.Status != protocol.StatusFactsUnavailable {
		t.Fatalf("status = %q, want facts_unavailable", resp.Status)
	}
	if resp.Activity != nil && resp.Activity.Found {
		t.Fatal("missing activity reported as found")
	}
}

func TestActivityFactsReportsDaemonUnreachableOnTransportError(t *testing.T) {
	reader := &fakeReader{err: context.DeadlineExceeded}
	resp := NewPlanner(reader).ActivityFacts(context.Background(), "chatgpt-wt-01")
	if resp.Status != protocol.StatusDaemonUnreachable {
		t.Fatalf("status = %q, want daemon_unreachable", resp.Status)
	}
}
```

Imports for this test file: `activitycore "github.com/maemreyo/shellbeam/internal/core/activity"`, `workspace "github.com/maemreyo/shellbeam/internal/core/workspace"`, `persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"`, `ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"`, `protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/browserbridge/ -run TestActivityFacts -v`
Expected: FAIL — `undefined: NewPlanner`.

- [ ] **Step 3: Write minimal implementation**

```go
// ports.go
package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
)

// DaemonReader is the only way a read plan reaches the daemon.
//
// The interface is intentionally one method wide. Every request a plan builds
// is constructed inside this package from a fixed action string, so no caller
// input can select an action, a command, or a session.
type DaemonReader interface {
	Read(ctx context.Context, req ipc.RequestV2) (ipc.ResponseV2, error)
}

type Planner struct{ reader DaemonReader }

func NewPlanner(reader DaemonReader) *Planner { return &Planner{reader: reader} }
```

```go
// facts_activity.go
package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	persistent "github.com/maemreyo/shellbeam/internal/core/persistentsession"
)

func base(verb protocol.Verb, status protocol.Status) protocol.Response {
	return protocol.Response{ProtocolVersion: protocol.ProtocolVersion, SupportedVersions: protocol.SupportedVersions(), Verb: verb, Status: status}
}

func unreachable(verb protocol.Verb) protocol.Response {
	resp := base(verb, protocol.StatusDaemonUnreachable)
	resp.Reason = "daemon_unreachable"
	return resp
}

func unavailable(verb protocol.Verb, reason string) protocol.Response {
	resp := base(verb, protocol.StatusFactsUnavailable)
	resp.Reason = reason
	return resp
}

// ActivityFacts runs the activity_facts read plan: inspect.activity for the
// operation refs, workspace ids and compaction counter, then
// inspect.sessions scoped to the same activity, because an Activity record
// carries no session state of its own.
func (p *Planner) ActivityFacts(ctx context.Context, correlationID string) protocol.Response {
	act, resp, ok := p.activity(ctx, protocol.VerbActivityFacts, correlationID)
	if !ok {
		return resp
	}
	facts := protocol.ActivityFacts{Found: true, OperationsRetained: len(act.Operations)}
	for _, id := range act.WorkspaceIDs {
		facts.WorkspaceIDs = append(facts.WorkspaceIDs, string(id))
	}
	if n := len(act.Operations); n > 0 {
		at := act.Operations[n-1].ObservedAt
		facts.LatestOperationAt = &at
	}
	sessions, err := p.reader.Read(ctx, ipc.RequestV2{IPVersion: 2, Kind: "request", RequestID: "bb-sessions", Action: "inspect.sessions", ActivityID: correlationID, MaxRecords: 64})
	if err != nil {
		return unreachable(protocol.VerbActivityFacts)
	}
	if sessions.OK && sessions.Sessions != nil {
		for _, s := range sessions.Sessions.Sessions {
			switch persistent.Lifecycle(s.State) {
			case persistent.LifecycleProvisioning:
				facts.Sessions.Provisioning++
			case persistent.LifecycleLive:
				facts.Sessions.Live++
			case persistent.LifecycleTerminal:
				facts.Sessions.Terminal++
			case persistent.LifecycleLost:
				facts.Sessions.Lost++
			}
		}
		facts.SessionsTruncated = sessions.Sessions.Continuation != ""
	}
	out := base(protocol.VerbActivityFacts, protocol.StatusOK)
	out.Activity = &facts
	out.Coverage = coverageFor(act.CompactedOperations)
	return out
}

func coverageFor(compacted int) protocol.Coverage {
	coverage := protocol.Coverage{CompactedOperations: compacted, HistoricalOperations: "complete"}
	if compacted > 0 {
		coverage.HistoricalOperations = "partial"
	}
	return coverage
}

func (p *Planner) activity(ctx context.Context, verb protocol.Verb, correlationID string) (activityRecord, protocol.Response, bool) {
	resp, err := p.reader.Read(ctx, ipc.RequestV2{IPVersion: 2, Kind: "request", RequestID: "bb-activity", Action: "inspect.activity", ActivityID: correlationID})
	if err != nil {
		return activityRecord{}, unreachable(verb), false
	}
	if !resp.OK || resp.Activity == nil {
		return activityRecord{}, unavailable(verb, "activity_not_found"), false
	}
	return activityRecord{Operations: resp.Activity.Operations, WorkspaceIDs: resp.Activity.WorkspaceIDs, CompactedOperations: resp.Activity.CompactedOperations}, protocol.Response{}, true
}
```

Declare the small local view type in the same file so the plans do not pass the whole core record around:

```go
type activityRecord struct {
	Operations          []activitycore.OperationRef
	WorkspaceIDs        []workspace.WorkspaceID
	CompactedOperations int
}
```

with imports `activitycore "github.com/maemreyo/shellbeam/internal/core/activity"` and `workspace "github.com/maemreyo/shellbeam/internal/core/workspace"`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/browserbridge/ -run TestActivityFacts -v -count=1`
Expected: PASS, three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/app/browserbridge/ports.go internal/app/browserbridge/facts_activity.go internal/app/browserbridge/facts_activity_test.go
git commit -m "feat: add activity_facts browser bridge read plan"
```

---

### Task 4: The activity_events read plan

**Files:**
- Modify: `internal/app/browserbridge/facts_activity.go`
- Test: `internal/app/browserbridge/facts_activity_test.go`

**Interfaces:**
- Consumes: `Planner`, `base`, `unreachable`, `unavailable` from Task 3.
- Produces: `(*Planner).ActivityEvents(ctx context.Context, correlationID, cursor string) protocol.Response`.

This verb is a single read with a single cursor, because the event journal has an activity target (`observation.TargetActivity`). Do not walk operations here.

- [ ] **Step 1: Write the failing test**

```go
func TestActivityEventsUsesOneActivityScopedReadAndPassesCursorThrough(t *testing.T) {
	reader := &fakeReader{byAction: map[string]ipc.ResponseV2{
		"inspect.events": {OK: true, Events: &observationapp.InspectResult{}},
	}}
	resp := NewPlanner(reader).ActivityEvents(context.Background(), "chatgpt-wt-01", "cursor-7")
	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if len(reader.seen) != 1 {
		t.Fatalf("expected exactly one read, got %d", len(reader.seen))
	}
	req := reader.seen[0]
	if req.Action != "inspect.events" {
		t.Fatalf("action = %q", req.Action)
	}
	if req.Target == nil || req.Target.Kind != observationcore.TargetActivity || req.Target.ActivityID != "chatgpt-wt-01" {
		t.Fatalf("target = %+v", req.Target)
	}
	if req.Target.OperationID != "" || req.Target.SessionID != "" {
		t.Fatal("event target leaked a non-activity selector")
	}
	if req.AfterEventCursor != "cursor-7" {
		t.Fatalf("after_event_cursor = %q", req.AfterEventCursor)
	}
	if req.MaxEvents != protocol.MaxActivityEvents {
		t.Fatalf("max_events = %d", req.MaxEvents)
	}
}
```

Add imports `observationapp "github.com/maemreyo/shellbeam/internal/app/observation"` and `observationcore "github.com/maemreyo/shellbeam/internal/core/observation"`.

The event cursor travels in `RequestV2.AfterEventCursor`, not `Continuation`, and the daemon validates it: it must carry the `observationapp.EventCursorPrefix` prefix and stay within `observationapp.MaxEventCursorBytes`. An opaque cursor from the extension that fails those checks must therefore surface as `facts_unavailable` rather than as a crash, so add this test too:

```go
func TestActivityEventsRejectsAMalformedCursorWithoutCrashing(t *testing.T) {
	reader := &fakeReader{byAction: map[string]ipc.ResponseV2{
		"inspect.events": {OK: false, Error: &ipc.Error{Code: "invalid_input"}},
	}}
	resp := NewPlanner(reader).ActivityEvents(context.Background(), "chatgpt-wt-01", "not-a-valid-cursor")
	if resp.Status != protocol.StatusFactsUnavailable {
		t.Fatalf("status = %q, want facts_unavailable", resp.Status)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/browserbridge/ -run TestActivityEvents -v`
Expected: FAIL — `undefined: (*Planner).ActivityEvents`.

- [ ] **Step 3: Write minimal implementation**

```go
// ActivityEvents runs the activity_events read plan: one activity-scoped
// journal read with one cursor. The cursor is opaque to the host and is
// stored by the extension, because the host holds no state.
func (p *Planner) ActivityEvents(ctx context.Context, correlationID, cursor string) protocol.Response {
	req := ipc.RequestV2{
		IPVersion: 2, Kind: "request", RequestID: "bb-events", Action: "inspect.events",
		Target:    &observationcore.Target{Kind: observationcore.TargetActivity, ActivityID: correlationID},
		MaxEvents: protocol.MaxActivityEvents,
	}
	if cursor != "" {
		req.AfterEventCursor = cursor
	}
	resp, err := p.reader.Read(ctx, req)
	if err != nil {
		return unreachable(protocol.VerbActivityEvents)
	}
	if !resp.OK || resp.Events == nil {
		return unavailable(protocol.VerbActivityEvents, "events_unavailable")
	}
	facts := protocol.EventFacts{Returned: len(resp.Events.Events), Cursor: resp.Events.NextEventCursor}
	seen := map[string]bool{}
	for _, event := range resp.Events.Events {
		kind := string(event.Kind)
		if !seen[kind] {
			seen[kind] = true
			facts.Kinds = append(facts.Kinds, kind)
		}
	}
	if n := len(resp.Events.Events); n > 0 {
		at := resp.Events.Events[n-1].RecordedAt
		facts.LatestAt = &at
	}
	out := base(protocol.VerbActivityEvents, protocol.StatusOK)
	out.Events = &facts
	out.Coverage.Truncated = resp.Events.Truncated
	if out.Coverage.Truncated {
		out.Coverage.TruncationReason = "more_events_available"
	}
	if resp.Events.CompactedBefore > 0 {
		out.Coverage.HistoricalOperations = "partial"
	}
	return out
}
```

`Truncated` comes from the journal's own flag rather than from the presence of a cursor, and `CompactedBefore > 0` is a separate, separately reported coverage loss: the journal discarded events the extension will never see, which is not the same fact as "more events are available after this cursor".

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/browserbridge/ -count=1 -v`
Expected: PASS, four tests.

- [ ] **Step 5: Commit**

```bash
git add internal/app/browserbridge/facts_activity.go internal/app/browserbridge/facts_activity_test.go
git commit -m "feat: add activity_events browser bridge read plan"
```

---

### Task 5: The verification_facts read plan, per workspace

**Files:**
- Create: `internal/app/browserbridge/facts_verification.go`
- Test: `internal/app/browserbridge/facts_verification_test.go`

**Interfaces:**
- Consumes: `Planner`, `activity`, `base`, `unreachable`, `unavailable`, `coverageFor` from Task 3.
- Produces: `(*Planner).VerificationFacts(ctx context.Context, correlationID string) protocol.Response`.

`inspect.verification` resolves a workspace before deriving anything, so a correlation id alone cannot execute it. The plan reads the activity first and uses its `WorkspaceIDs`. Results are per workspace and are never summed.

- [ ] **Step 1: Write the failing test**

```go
func TestVerificationFactsReturnsPerWorkspaceAndNeverAggregates(t *testing.T) {
	reader := &recordingVerificationReader{
		activity: &activitycore.Activity{ID: "wt", WorkspaceIDs: []workspace.WorkspaceID{"ws-1", "ws-2"}},
		byWorkspace: map[string]verificationapp.Inspection{
			"ws-1": {WorkspaceID: "ws-1", SourceGeneration: "g1", PolicyState: "active", Gate: verificationcore.GateEvaluation{Status: "blocked", Breakdown: verificationcore.GateBreakdown{EvidenceSatisfied: 7, Blocking: 1, Indeterminate: 2}}},
			"ws-2": {WorkspaceID: "ws-2", SourceGeneration: "g9", PolicyState: "policy_absent", Gate: verificationcore.GateEvaluation{Status: "indeterminate", Breakdown: verificationcore.GateBreakdown{Indeterminate: 4}}},
		},
	}
	resp := NewPlanner(reader).VerificationFacts(context.Background(), "wt")
	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if len(resp.Verification) != 2 {
		t.Fatalf("want two workspace entries, got %d", len(resp.Verification))
	}
	byID := map[string]protocol.WorkspaceVerification{}
	for _, entry := range resp.Verification {
		byID[entry.WorkspaceID] = entry
	}
	if byID["ws-1"].Blocking != 1 || byID["ws-1"].Satisfied != 7 || byID["ws-1"].Indeterminate != 2 {
		t.Fatalf("ws-1 counts = %+v", byID["ws-1"])
	}
	if byID["ws-2"].Indeterminate != 4 || byID["ws-2"].PolicyState != "policy_absent" {
		t.Fatalf("ws-2 = %+v", byID["ws-2"])
	}
	if byID["ws-1"].SourceGeneration == byID["ws-2"].SourceGeneration {
		t.Fatal("source generations were flattened")
	}
	for _, req := range reader.verificationRequests {
		if req.ActivityID != "wt" {
			t.Fatalf("verification read not activity-scoped: %+v", req)
		}
		if req.WorkspaceID == "" {
			t.Fatal("verification read issued without a host-derived workspace")
		}
	}
}

func TestVerificationFactsBoundsWorkspaceFanOut(t *testing.T) {
	many := make([]workspace.WorkspaceID, 0, protocol.MaxVerificationWorkspaces+3)
	for i := 0; i < protocol.MaxVerificationWorkspaces+3; i++ {
		many = append(many, workspace.WorkspaceID(fmt.Sprintf("ws-%d", i)))
	}
	reader := &recordingVerificationReader{activity: &activitycore.Activity{ID: "wt", WorkspaceIDs: many}, byWorkspace: map[string]verificationapp.Inspection{}}
	resp := NewPlanner(reader).VerificationFacts(context.Background(), "wt")
	if len(reader.verificationRequests) > protocol.MaxVerificationWorkspaces {
		t.Fatalf("issued %d reads, cap is %d", len(reader.verificationRequests), protocol.MaxVerificationWorkspaces)
	}
	if !resp.Coverage.Truncated || resp.Coverage.TruncationReason != "workspace_fan_out_capped" {
		t.Fatalf("fan-out cap was silent: %+v", resp.Coverage)
	}
}
```

Write `recordingVerificationReader` in the test file as a `DaemonReader` that answers `inspect.activity` with its `activity` field, answers `inspect.verification` from `byWorkspace` keyed on `req.WorkspaceID`, and appends each verification request to `verificationRequests`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/browserbridge/ -run TestVerificationFacts -v`
Expected: FAIL — `undefined: (*Planner).VerificationFacts`.

- [ ] **Step 3: Write minimal implementation**

```go
package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
)

// VerificationFacts runs the verification_facts read plan.
//
// inspect.verification resolves a workspace before deriving an affected
// surface, so the plan must learn the workspaces from the activity record
// first. Results stay per workspace: each workspace has its own policy,
// policy generation and authority, so a summed count would answer no
// evaluable question.
func (p *Planner) VerificationFacts(ctx context.Context, correlationID string) protocol.Response {
	act, failure, ok := p.activity(ctx, protocol.VerbVerificationFacts, correlationID)
	if !ok {
		return failure
	}
	out := base(protocol.VerbVerificationFacts, protocol.StatusOK)
	out.Coverage = coverageFor(act.CompactedOperations)
	workspaces := act.WorkspaceIDs
	if len(workspaces) > protocol.MaxVerificationWorkspaces {
		workspaces = workspaces[:protocol.MaxVerificationWorkspaces]
		out.Coverage.Truncated = true
		out.Coverage.TruncationReason = "workspace_fan_out_capped"
	}
	for _, id := range workspaces {
		resp, err := p.reader.Read(ctx, ipc.RequestV2{IPVersion: 2, Kind: "request", RequestID: "bb-verification", Action: "inspect.verification", WorkspaceID: string(id), ActivityID: correlationID})
		if err != nil {
			return unreachable(protocol.VerbVerificationFacts)
		}
		if !resp.OK || resp.Verification == nil {
			continue
		}
		v := resp.Verification
		out.Verification = append(out.Verification, protocol.WorkspaceVerification{
			WorkspaceID:      v.WorkspaceID,
			PolicyState:      string(v.PolicyState),
			GateStatus:       string(v.Gate.Status),
			SourceGeneration: v.SourceGeneration,
			Satisfied:        v.Gate.Breakdown.EvidenceSatisfied,
			Waived:           v.Gate.Breakdown.Waived,
			Blocking:         v.Gate.Breakdown.Blocking,
			Indeterminate:    v.Gate.Breakdown.Indeterminate,
		})
	}
	if len(out.Verification) == 0 && !out.Coverage.Truncated {
		return unavailable(protocol.VerbVerificationFacts, "no_workspace_verification")
	}
	return out
}
```

`resp.Verification` resolves through the embedded `VerificationResponseV2Fields` on `ResponseV2` and is a `*verificationapp.Inspection`. Its gate counts live at `Gate.Breakdown.EvidenceSatisfied`, `.Waived`, `.Blocking`, `.Indeterminate`. Imports: `verificationapp "github.com/maemreyo/shellbeam/internal/app/verification"` and `verificationcore "github.com/maemreyo/shellbeam/internal/core/verification"` in the test file.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/browserbridge/ -count=1 -v`
Expected: PASS, seven tests.

- [ ] **Step 5: Commit**

```bash
git add internal/app/browserbridge/facts_verification.go internal/app/browserbridge/facts_verification_test.go
git commit -m "feat: add per-workspace verification_facts read plan"
```

---

### Task 6: The structured_failure_facts bounded walk

**Files:**
- Create: `internal/app/browserbridge/facts_structured.go`
- Test: `internal/app/browserbridge/facts_structured_test.go`

**Interfaces:**
- Consumes: `Planner`, `activity`, `base`, `unreachable`, `unavailable`, `coverageFor` from Task 3.
- Produces: `(*Planner).StructuredFailureFacts(ctx context.Context, correlationID string) protocol.Response`.

`inspect.structured` is strictly operation-scoped, so this plan walks the activity's retained operation refs newest-first, bounded by `MaxStructuredOperations`. Compaction and an early stop are separate, separately reported coverage losses.

- [ ] **Step 1: Write the failing test**

```go
func TestStructuredFailureFactsWalksRetainedOperationsNewestFirstAndBounds(t *testing.T) {
	refs := make([]activitycore.OperationRef, 0, protocol.MaxStructuredOperations+4)
	for i := 0; i < protocol.MaxStructuredOperations+4; i++ {
		refs = append(refs, activitycore.OperationRef{OperationID: fmt.Sprintf("op-%d", i), SessionID: "s", ObservedAt: time.Unix(int64(i), 0).UTC()})
	}
	reader := &recordingStructuredReader{
		activity: &activitycore.Activity{ID: "wt", Operations: refs, CompactedOperations: 5},
		result: structuredapp.InspectResult{
			Status:   structuredapp.InspectTerminal,
			Producer: &structuredcore.Producer{AdapterID: "pytest-junit-xml", AdapterVersion: 1},
			Summary:  structuredapp.InspectSummary{TestFailed: 2, TestPassed: 8},
			Records: []structuredcore.Record{
				{RecordKind: structuredcore.RecordTestCase, Authority: structuredcore.AuthorityMechanical, DerivationMethod: structuredcore.DerivationDeterministicNormalize, TestCase: &structuredcore.TestCase{Name: "test_a", Package: "pkg", Status: structuredcore.TestFailed}},
			},
		},
	}
	resp := NewPlanner(reader).StructuredFailureFacts(context.Background(), "wt")

	if resp.Status != protocol.StatusOK {
		t.Fatalf("status = %q", resp.Status)
	}
	if len(reader.operationIDs) != protocol.MaxStructuredOperations {
		t.Fatalf("walked %d operations, cap is %d", len(reader.operationIDs), protocol.MaxStructuredOperations)
	}
	newest := fmt.Sprintf("op-%d", protocol.MaxStructuredOperations+3)
	if reader.operationIDs[0] != newest {
		t.Fatalf("walk did not start at the newest ref: %q", reader.operationIDs[0])
	}
	if resp.Coverage.CompactedOperations != 5 || resp.Coverage.HistoricalOperations != "partial" {
		t.Fatalf("compaction not surfaced: %+v", resp.Coverage)
	}
	if !resp.Coverage.Truncated {
		t.Fatal("walk cap not surfaced")
	}
	first := resp.Structured[0]
	if first.Authority != "mechanical" || first.DerivationMethod != "deterministic_normalization" || first.AdapterID != "pytest-junit-xml" {
		t.Fatalf("comparability fields lost: %+v", first)
	}
	if len(first.FailingCases) != 1 || first.FailingCases[0].Name != "test_a" {
		t.Fatalf("failing cases = %+v", first.FailingCases)
	}
}

func TestStructuredFailureFactsRequestsOnlyFailingRecords(t *testing.T) {
	reader := &recordingStructuredReader{activity: &activitycore.Activity{ID: "wt", Operations: []activitycore.OperationRef{{OperationID: "op-1", SessionID: "s"}}}, result: structuredapp.InspectResult{Status: structuredapp.InspectTerminal}}
	NewPlanner(reader).StructuredFailureFacts(context.Background(), "wt")
	if len(reader.requests) != 1 {
		t.Fatalf("expected one structured read, got %d", len(reader.requests))
	}
	if reader.requests[0].TestStatus != structuredcore.TestFailed {
		t.Fatalf("test_status filter = %q, want fail", reader.requests[0].TestStatus)
	}
	if reader.requests[0].MaxRecords != protocol.MaxFailingCasesPerOperation {
		t.Fatalf("max_records = %d", reader.requests[0].MaxRecords)
	}
}
```

Write `recordingStructuredReader` as a `DaemonReader` that answers `inspect.activity` from `activity`, answers `inspect.structured` with `result`, appends each `inspect.structured` request to `requests`, and appends `req.OperationID` to `operationIDs`.

Two typing facts that matter here: `InspectStatus` has no `InspectFound` — the terminal value is `structuredapp.InspectTerminal` (the set is `not_found`, `pending`, `processing`, `terminal`) — and `RequestV2.TestStatus` is typed `structuredcore.TestStatus`, not `string`, so it takes `structuredcore.TestFailed` without a conversion.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/browserbridge/ -run TestStructuredFailureFacts -v`
Expected: FAIL — `undefined: (*Planner).StructuredFailureFacts`.

- [ ] **Step 3: Write minimal implementation**

```go
package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
	structuredcore "github.com/maemreyo/shellbeam/internal/core/structuredresult"
)

// StructuredFailureFacts runs the structured_failure_facts read plan.
//
// inspect.structured is operation-scoped, so the plan walks the activity's
// retained operation refs newest-first under a hard cap. Two coverage losses
// are possible and are reported separately: operations the activity already
// compacted away, and refs this walk did not reach.
func (p *Planner) StructuredFailureFacts(ctx context.Context, correlationID string) protocol.Response {
	act, failure, ok := p.activity(ctx, protocol.VerbStructuredFailureFacts, correlationID)
	if !ok {
		return failure
	}
	out := base(protocol.VerbStructuredFailureFacts, protocol.StatusOK)
	out.Coverage = coverageFor(act.CompactedOperations)
	walked := 0
	for i := len(act.Operations) - 1; i >= 0; i-- {
		if walked >= protocol.MaxStructuredOperations {
			out.Coverage.Truncated = true
			out.Coverage.TruncationReason = "operation_walk_capped"
			break
		}
		walked++
		ref := act.Operations[i]
		resp, err := p.reader.Read(ctx, ipc.RequestV2{
			IPVersion: 2, Kind: "request", RequestID: "bb-structured", Action: "inspect.structured",
			OperationID: ref.OperationID, TestStatus: structuredcore.TestFailed,
			MaxRecords: protocol.MaxFailingCasesPerOperation,
		})
		if err != nil {
			return unreachable(protocol.VerbStructuredFailureFacts)
		}
		if !resp.OK || resp.Structured == nil {
			continue
		}
		out.Structured = append(out.Structured, failureFacts(ref.OperationID, resp.Structured))
	}
	return out
}

func failureFacts(operationID string, in *structuredInspect) protocol.OperationFailureFacts {
	facts := protocol.OperationFailureFacts{
		OperationID:  operationID,
		Completeness: string(in.Completeness),
		TestPassed:   in.Summary.TestPassed,
		TestFailed:   in.Summary.TestFailed,
		Errors:       in.Summary.Errors,
	}
	if in.Producer != nil {
		facts.AdapterID = in.Producer.AdapterID
		facts.AdapterVersion = in.Producer.AdapterVersion
	}
	for _, record := range in.Records {
		if record.TestCase == nil {
			continue
		}
		if len(facts.FailingCases) >= protocol.MaxFailingCasesPerOperation {
			facts.CasesTruncated = true
			break
		}
		facts.Authority = string(record.Authority)
		facts.DerivationMethod = string(record.DerivationMethod)
		facts.FailingCases = append(facts.FailingCases, protocol.FailingCase{Name: record.TestCase.Name, Package: record.TestCase.Package, Status: string(record.TestCase.Status)})
	}
	if in.Summary.Truncated {
		facts.CasesTruncated = true
	}
	return facts
}
```

Declare `type structuredInspect = structuredapp.InspectResult` so the helper signature stays short; `ResponseV2.Structured` is a `*structuredapp.InspectResult`. `in.Summary` is a `structuredapp.InspectSummary` with `TestPassed`, `TestFailed`, `Errors`, and `Truncated`.

Per-record `Authority` and `DerivationMethod` are copied rather than assumed uniform because the comparability rule in the design requires them to travel with the surface; if a later provider mixes authorities inside one operation, this plan must be revisited rather than silently reporting the last record's authority.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/browserbridge/ -count=1 -v`
Expected: PASS, eight tests.

- [ ] **Step 5: Commit**

```bash
git add internal/app/browserbridge/facts_structured.go internal/app/browserbridge/facts_structured_test.go
git commit -m "feat: add bounded structured_failure_facts read plan"
```

---

### Task 7: Hello, version negotiation, and asymmetric decoding

**Files:**
- Create: `internal/app/browserbridge/host.go`
- Test: `internal/app/browserbridge/host_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–6.
- Produces: `Serve(ctx context.Context, planner *Planner, in io.Reader, out io.Writer) error` — reads exactly one JSON message, dispatches one verb, writes exactly one bounded JSON message, returns. `Decode(raw []byte) (protocol.Request, protocol.Response, bool)` — lenient for `hello`, strict for every other verb.

- [ ] **Step 1: Write the failing test**

```go
func TestHelloParsesLenientlyAndReportsSupportedVersions(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"protocol_version":1,"verb":"hello","future_field_from_a_newer_extension":true}`)
	if err := Serve(context.Background(), NewPlanner(&fakeReader{}), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != protocol.StatusOK || resp.Verb != protocol.VerbHello {
		t.Fatalf("hello response = %+v", resp)
	}
	if len(resp.SupportedVersions) != 1 || resp.SupportedVersions[0] != protocol.ProtocolVersion {
		t.Fatalf("supported versions = %v", resp.SupportedVersions)
	}
}

func TestNonHelloVerbsRejectUnknownFields(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"protocol_version":1,"verb":"activity_facts","correlation_id":"wt","command":"rm -rf /"}`)
	if err := Serve(context.Background(), NewPlanner(&fakeReader{}), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != protocol.StatusInvalidRequest {
		t.Fatalf("status = %q, want invalid_request", resp.Status)
	}
}

func TestIncompatibleProtocolVersionIsDistinguishable(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"protocol_version":9,"verb":"hello"}`)
	if err := Serve(context.Background(), NewPlanner(&fakeReader{}), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp protocol.Response
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != protocol.StatusProtocolIncompatible {
		t.Fatalf("status = %q, want protocol_incompatible", resp.Status)
	}
	if len(resp.SupportedVersions) == 0 {
		t.Fatal("mismatch response omitted the supported version set")
	}
}

func TestServeWritesExactlyOneMessageAndIgnoresTrailingInput(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"protocol_version":1,"verb":"hello"}{"protocol_version":1,"verb":"hello"}`)
	if err := Serve(context.Background(), NewPlanner(&fakeReader{}), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if bytes.Count(out.Bytes(), []byte("\"verb\"")) != 1 {
		t.Fatalf("wrote more than one message: %s", out.String())
	}
}
```

Imports for this test file: `bytes`, `context`, `encoding/json`, `strings`, `testing`, and `protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/browserbridge/ -run 'TestHello|TestNonHello|TestIncompatible|TestServe' -v`
Expected: FAIL — `undefined: Serve`.

- [ ] **Step 3: Write minimal implementation**

```go
package browserbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
)

// Decode applies deliberately asymmetric strictness.
//
// hello must stay parseable across protocol versions: a host that only
// implements a later version still has to answer an older hello, or a version
// mismatch becomes indistinguishable from a broken host and the extension
// cannot tell the user which remediation applies. Every other verb decodes
// strictly, so an unknown field is a rejected request rather than a silently
// ignored one.
func Decode(raw []byte) (protocol.Request, protocol.Response, bool) {
	var probe struct {
		ProtocolVersion int           `json:"protocol_version"`
		Verb            protocol.Verb `json:"verb"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return protocol.Request{}, invalid("malformed_request"), false
	}
	if probe.ProtocolVersion != protocol.ProtocolVersion {
		resp := base(probe.Verb, protocol.StatusProtocolIncompatible)
		resp.Reason = "protocol_incompatible"
		return protocol.Request{}, resp, false
	}
	if probe.Verb == protocol.VerbHello {
		return protocol.Request{ProtocolVersion: probe.ProtocolVersion, Verb: protocol.VerbHello}, protocol.Response{}, true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var req protocol.Request
	if err := decoder.Decode(&req); err != nil {
		return protocol.Request{}, invalid("unexpected_field"), false
	}
	if err := req.Validate(); err != nil {
		return protocol.Request{}, invalid(err.Error()), false
	}
	return req, protocol.Response{}, true
}

func invalid(reason string) protocol.Response {
	resp := base("", protocol.StatusInvalidRequest)
	resp.Reason = reason
	return resp
}

// Serve reads one message, answers it, and returns. The host process exits
// after this call; it holds no cursors, no watch state and no session.
func Serve(ctx context.Context, planner *Planner, in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(in, protocol.MaxResponseBytes))
	if err != nil {
		return err
	}
	req, failure, ok := Decode(raw)
	resp := failure
	if ok {
		resp = dispatch(ctx, planner, req)
	}
	encoded, err := protocol.BoundResponse(resp)
	if err != nil {
		return err
	}
	_, err = out.Write(encoded)
	return err
}

func dispatch(ctx context.Context, planner *Planner, req protocol.Request) protocol.Response {
	switch req.Verb {
	case protocol.VerbHello:
		return base(protocol.VerbHello, protocol.StatusOK)
	case protocol.VerbActivityFacts:
		return planner.ActivityFacts(ctx, req.CorrelationID)
	case protocol.VerbActivityEvents:
		return planner.ActivityEvents(ctx, req.CorrelationID, req.Cursor)
	case protocol.VerbVerificationFacts:
		return planner.VerificationFacts(ctx, req.CorrelationID)
	case protocol.VerbStructuredFailureFacts:
		return planner.StructuredFailureFacts(ctx, req.CorrelationID)
	default:
		return invalid("unknown_verb")
	}
}
```

Note the `default` arm is unreachable through `Decode`, and is kept so a future verb added to the enum without a dispatch arm fails closed rather than returning a zero response.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/browserbridge/ -count=1 -v`
Expected: PASS, twelve tests.

- [ ] **Step 5: Commit**

```bash
git add internal/app/browserbridge/host.go internal/app/browserbridge/host_test.go
git commit -m "feat: serve one browser bridge message with asymmetric decoding"
```

---

### Task 8: The host binary

**Files:**
- Create: `internal/adapter/browserbridge/daemon_reader.go`
- Create: `cmd/shellbeam-browser-host/main.go`
- Test: `internal/adapter/browserbridge/daemon_reader_test.go`

**Interfaces:**
- Consumes: `bridgeapp.DaemonReader`, `bridgeapp.Serve`, `bridgeapp.NewPlanner`.
- Produces: `NewDaemonReader(socket string) *DaemonReader` with method `Read(ctx context.Context, req ipc.RequestV2) (ipc.ResponseV2, error)`.

The host is its own binary because Firefox executes the manifest `path` with no arguments of ours; a subcommand string cannot be injected into that exec.

- [ ] **Step 1: Write the failing test**

```go
package browserbridge

import (
	"context"
	"testing"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
)

func TestDaemonReaderSurfacesTransportFailureRatherThanPanicking(t *testing.T) {
	reader := NewDaemonReader("/nonexistent/shellbeam-test.sock")
	_, err := reader.Read(context.Background(), ipc.RequestV2{IPVersion: 2, Kind: "request", RequestID: "t", Action: "inspect.activity", ActivityID: "wt"})
	if err == nil {
		t.Fatal("expected a transport error against a missing socket")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/adapter/browserbridge/ -v`
Expected: FAIL — `undefined: NewDaemonReader`.

- [ ] **Step 3: Write minimal implementation**

```go
// Package browserbridge adapts the ShellBeam IPC client to the browser
// bridge's read-only port. It is the only place in the browser bridge that
// knows a transport exists.
package browserbridge

import (
	"context"

	ipc "github.com/maemreyo/shellbeam/internal/adapter/ipc"
)

type DaemonReader struct{ client *ipc.Client }

func NewDaemonReader(socket string) *DaemonReader {
	return &DaemonReader{client: ipc.NewClient(socket)}
}

func (r *DaemonReader) Read(ctx context.Context, req ipc.RequestV2) (ipc.ResponseV2, error) {
	return r.client.CallV2(ctx, req)
}
```

```go
// cmd/shellbeam-browser-host/main.go
//
// Firefox execs the native messaging host directly from the manifest path
// with no arguments of ours, so the host is a separate binary rather than a
// shellbeam subcommand. It reads one message, writes one message, and exits.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	adapter "github.com/maemreyo/shellbeam/internal/adapter/browserbridge"
	bridgeapp "github.com/maemreyo/shellbeam/internal/app/browserbridge"
	"github.com/maemreyo/shellbeam/internal/config"
)

const readTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "shellbeam-browser-host: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	paths, err := config.ResolvePaths(runtime.GOOS, os.Getuid(), home, map[string]string{
		"XDG_CONFIG_HOME": os.Getenv("XDG_CONFIG_HOME"),
		"XDG_STATE_HOME":  os.Getenv("XDG_STATE_HOME"),
		"XDG_RUNTIME_DIR": os.Getenv("XDG_RUNTIME_DIR"),
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	planner := bridgeapp.NewPlanner(adapter.NewDaemonReader(paths.Socket))
	return bridgeapp.Serve(ctx, planner, os.Stdin, os.Stdout)
}
```

**Framing is mandatory and is part of this task.** MDN specifies the wire format exactly: *"Each message is serialized using JSON, UTF-8 encoded and is preceded with an unsigned 32-bit value containing the message length in native byte order."* Native byte order, not big-endian — so use `binary.NativeEndian`, and never `binary.BigEndian`, or the host will silently misparse on every little-endian machine, which is every machine this will run on.

`Serve` stays framing-agnostic so it remains unit-testable; add `internal/app/browserbridge/framing.go`:

```go
package browserbridge

import (
	"encoding/binary"
	"fmt"
	"io"

	protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"
)

// ReadFramed reads one native-messaging frame: an unsigned 32-bit length in
// NATIVE byte order followed by that many UTF-8 JSON bytes.
func ReadFramed(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := binary.NativeEndian.Uint32(header[:])
	if length == 0 || length > protocol.MaxResponseBytes {
		return nil, fmt.Errorf("framed message length %d out of range", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// WriteFramed writes one native-messaging frame. The 1 MB app-to-extension
// transport ceiling is enforced upstream by BoundResponse; this guard exists so
// a future caller cannot bypass it.
func WriteFramed(w io.Writer, payload []byte) error {
	if len(payload) > protocol.MaxResponseBytes {
		return fmt.Errorf("framed response %d exceeds bound", len(payload))
	}
	var header [4]byte
	binary.NativeEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}
```

and a test in `internal/app/browserbridge/framing_test.go`:

```go
func TestFramingRoundTripsInNativeByteOrder(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFramed(&buf, []byte(`{"verb":"hello"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if buf.Len() != 4+len(`{"verb":"hello"}`) {
		t.Fatalf("frame length = %d", buf.Len())
	}
	got, err := ReadFramed(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != `{"verb":"hello"}` {
		t.Fatalf("payload = %q", got)
	}
}

func TestReadFramedRejectsAnOversizedOrZeroLengthHeader(t *testing.T) {
	for _, length := range []uint32{0, protocol.MaxResponseBytes + 1} {
		var header [4]byte
		binary.NativeEndian.PutUint32(header[:], length)
		if _, err := ReadFramed(bytes.NewReader(header[:])); err == nil {
			t.Fatalf("length %d accepted", length)
		}
	}
}
```

Then `main.go` uses the framed path rather than raw stdio:

```go
	payload, err := bridgeapp.ReadFramed(os.Stdin)
	if err != nil {
		return err
	}
	var reply bytes.Buffer
	if err := bridgeapp.Serve(ctx, planner, bytes.NewReader(payload), &reply); err != nil {
		return err
	}
	return bridgeapp.WriteFramed(os.Stdout, reply.Bytes())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/adapter/browserbridge/ ./internal/app/browserbridge/ -count=1 -v && go build ./cmd/shellbeam-browser-host`
Expected: PASS including the two framing tests, and the binary builds.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/browserbridge cmd/shellbeam-browser-host
git commit -m "feat: add shellbeam-browser-host binary"
```

---

### Task 9: Native manifest install and uninstall

**Files:**
- Create: `internal/app/browserbridge/manifest.go`
- Create: `internal/app/browserbridge/manifest_test.go`
- Create: `cmd/shellbeam/command_browser_host.go`
- Modify: `cmd/shellbeam/command.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `HostName = "com.shellbeam.browser_bridge"`; `ManifestDir(goos, home string) (string, error)`; `RenderManifest(hostPath, extensionID string) ([]byte, error)`; `InstallManifest(goos, home, hostPath, extensionID string) (string, error)`; `RemoveManifest(goos, home string) (string, error)`; `runBrowserHost(ctx context.Context, args []string, out io.Writer) error`.

- [ ] **Step 1: Write the failing test**

```go
func TestRenderManifestPinsExactlyOneExtensionID(t *testing.T) {
	raw, err := RenderManifest("/usr/local/bin/shellbeam-browser-host", "attention-router@shellbeam.local")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var manifest struct {
		Name              string   `json:"name"`
		Type              string   `json:"type"`
		Path              string   `json:"path"`
		AllowedExtensions []string `json:"allowed_extensions"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if manifest.Name != HostName || manifest.Type != "stdio" {
		t.Fatalf("manifest identity = %+v", manifest)
	}
	if len(manifest.AllowedExtensions) != 1 || manifest.AllowedExtensions[0] != "attention-router@shellbeam.local" {
		t.Fatalf("allowed_extensions = %v", manifest.AllowedExtensions)
	}
}

func TestRenderManifestRejectsMissingOrWildcardExtensionID(t *testing.T) {
	for _, id := range []string{"", "*", "  ", "a@b, c@d"} {
		if _, err := RenderManifest("/usr/local/bin/shellbeam-browser-host", id); err == nil {
			t.Fatalf("extension id %q accepted", id)
		}
	}
}

func TestRenderManifestRequiresAbsoluteHostPath(t *testing.T) {
	if _, err := RenderManifest("shellbeam-browser-host", "a@b"); err == nil {
		t.Fatal("relative host path accepted")
	}
}

func TestManifestDirIsPerUserAndPlatformSpecific(t *testing.T) {
	darwin, err := ManifestDir("darwin", "/Users/u")
	if err != nil {
		t.Fatalf("darwin: %v", err)
	}
	if darwin != "/Users/u/Library/Application Support/Mozilla/NativeMessagingHosts" {
		t.Fatalf("darwin dir = %q", darwin)
	}
	linux, err := ManifestDir("linux", "/home/u")
	if err != nil {
		t.Fatalf("linux: %v", err)
	}
	if linux != "/home/u/.mozilla/native-messaging-hosts" {
		t.Fatalf("linux dir = %q", linux)
	}
	if _, err := ManifestDir("plan9", "/home/u"); err == nil {
		t.Fatal("unsupported platform accepted")
	}
}

func TestInstallThenRemoveManifestRoundTrips(t *testing.T) {
	home := t.TempDir()
	path, err := InstallManifest("linux", home, "/usr/local/bin/shellbeam-browser-host", "a@b")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	if _, err := RemoveManifest("linux", home); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("manifest survived removal")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/browserbridge/ -run Manifest -v`
Expected: FAIL — `undefined: RenderManifest`.

- [ ] **Step 3: Write minimal implementation**

```go
package browserbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const HostName = "com.shellbeam.browser_bridge"

// ManifestDir returns the per-user Firefox native messaging host directory.
//
// Installation is per user and deliberately separate from `shellbeam
// install`: installing the daemon must never silently grant a browser
// extension a channel to machine facts.
func ManifestDir(goos, home string) (string, error) {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Mozilla", "NativeMessagingHosts"), nil
	case "linux":
		return filepath.Join(home, ".mozilla", "native-messaging-hosts"), nil
	default:
		return "", fmt.Errorf("unsupported platform for native messaging manifest")
	}
}

func RenderManifest(hostPath, extensionID string) ([]byte, error) {
	if !filepath.IsAbs(hostPath) {
		return nil, fmt.Errorf("host path must be absolute")
	}
	id := strings.TrimSpace(extensionID)
	if id == "" || id != extensionID || strings.ContainsAny(id, "*, \t") {
		return nil, fmt.Errorf("exactly one literal extension id is required")
	}
	return json.MarshalIndent(map[string]any{
		"name":               HostName,
		"description":        "ShellBeam Browser Bridge (read-only machine facts)",
		"path":               hostPath,
		"type":               "stdio",
		"allowed_extensions": []string{id},
	}, "", "  ")
}

func InstallManifest(goos, home, hostPath, extensionID string) (string, error) {
	raw, err := RenderManifest(hostPath, extensionID)
	if err != nil {
		return "", err
	}
	dir, err := ManifestDir(goos, home)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, HostName+".json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func RemoveManifest(goos, home string) (string, error) {
	dir, err := ManifestDir(goos, home)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, HostName+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}
```

```go
// cmd/shellbeam/command_browser_host.go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	bridgeapp "github.com/maemreyo/shellbeam/internal/app/browserbridge"
)

const browserHostUsage = "usage: shellbeam browser-host <install --extension-id=ID --host-path=PATH|uninstall>"

func runBrowserHost(_ context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf(browserHostUsage)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	switch args[0] {
	case "install":
		var extensionID, hostPath string
		fs := flag.NewFlagSet("browser-host install", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		fs.StringVar(&extensionID, "extension-id", "", "the single Firefox extension id to allow")
		fs.StringVar(&hostPath, "host-path", "", "absolute path to the shellbeam-browser-host binary")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		path, err := bridgeapp.InstallManifest(runtime.GOOS, home, hostPath, extensionID)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "installed %s\n", path)
		return nil
	case "uninstall":
		path, err := bridgeapp.RemoveManifest(runtime.GOOS, home)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "removed %s\n", path)
		return nil
	default:
		return fmt.Errorf(browserHostUsage)
	}
}
```

In `cmd/shellbeam/command.go`, add the dispatch arm after the `project` case:

```go
	case "browser-host":
		err = runBrowserHost(ctx, args[1:], stdout)
```

and change `topLevelUsage` to:

```go
const topLevelUsage = "usage: shellbeam <daemon|mcp|workspace|project|browser-host|install|uninstall|status|doctor|version>"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/browserbridge/ -count=1 -v && go build ./cmd/shellbeam && ./shellbeam browser-host 2>&1 | head -1`
Expected: tests PASS; the last command prints the usage line.

- [ ] **Step 5: Commit**

```bash
git add internal/app/browserbridge/manifest.go internal/app/browserbridge/manifest_test.go cmd/shellbeam/command_browser_host.go cmd/shellbeam/command.go
git commit -m "feat: install and remove the browser bridge native manifest"
```

---

### Task 10: Doctor check

**Files:**
- Modify: `cmd/shellbeam/doctor.go`
- Test: `cmd/shellbeam/browser_bridge_doctor_test.go`

**Interfaces:**
- Consumes: `bridgeapp.ManifestDir`, `bridgeapp.HostName`, `control.Check`, `control.Report`, `control.Pass`, `control.Warn`.
- Produces: a `browser_bridge` check appended to the doctor report.

The check reports the three bootstrap outcomes the extension cannot diagnose on its own. A missing manifest is a `warn`, not a `fail`: the bridge is optional and its absence must not make `doctor` claim an unsafe boundary.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"strings"
	"testing"

	bridgeapp "github.com/maemreyo/shellbeam/internal/app/browserbridge"
	control "github.com/maemreyo/shellbeam/internal/app/control"
)

func TestBrowserBridgeCheckWarnsWhenManifestAbsent(t *testing.T) {
	check := browserBridgeCheck("linux", t.TempDir())
	if check.ID != "browser_bridge" {
		t.Fatalf("id = %q", check.ID)
	}
	if check.Status != control.Warn {
		t.Fatalf("status = %q, want warn", check.Status)
	}
	if !strings.Contains(check.Hint, "browser-host install") {
		t.Fatalf("hint lacks remediation: %q", check.Hint)
	}
}

func TestBrowserBridgeCheckReportsPinnedExtensionIDAndProtocolVersion(t *testing.T) {
	home := t.TempDir()
	if _, err := bridgeapp.InstallManifest("linux", home, "/usr/local/bin/shellbeam-browser-host", "router@shellbeam.local"); err != nil {
		t.Fatalf("install: %v", err)
	}
	check := browserBridgeCheck("linux", home)
	if check.Status != control.Pass {
		t.Fatalf("status = %q, want pass", check.Status)
	}
	if !strings.Contains(check.Message, "router@shellbeam.local") {
		t.Fatalf("message omits pinned extension id: %q", check.Message)
	}
	if !strings.Contains(check.Message, "protocol 1") {
		t.Fatalf("message omits protocol version: %q", check.Message)
	}
}

func TestBrowserBridgeCheckNeverFailsTheReport(t *testing.T) {
	report := control.Report{SchemaVersion: 1, Checks: []control.Check{browserBridgeCheck("linux", t.TempDir())}}
	if report.ExitCode() != 0 {
		t.Fatal("absent optional bridge made doctor claim an unsafe boundary")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/shellbeam/ -run TestBrowserBridgeCheck -v`
Expected: FAIL — `undefined: browserBridgeCheck`.

- [ ] **Step 3: Write minimal implementation**

Add to `cmd/shellbeam/doctor.go`:

```go
// browserBridgeCheck reports the bootstrap facts the extension cannot see.
//
// Firefox cannot spawn a host whose manifest is missing, so "host absent" can
// never arrive as a protocol reply; only doctor can tell the operator which of
// the three remediations applies. The bridge is optional, so absence warns.
func browserBridgeCheck(goos, home string) control.Check {
	dir, err := bridgeapp.ManifestDir(goos, home)
	if err != nil {
		return control.Check{ID: "browser_bridge", Status: control.Warn, Message: "browser bridge unsupported on this platform", Hint: err.Error()}
	}
	path := filepath.Join(dir, bridgeapp.HostName+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return control.Check{ID: "browser_bridge", Status: control.Warn, Message: "browser bridge manifest not installed", Hint: "run: shellbeam browser-host install --extension-id=ID --host-path=PATH"}
	}
	var manifest struct {
		Path              string   `json:"path"`
		AllowedExtensions []string `json:"allowed_extensions"`
	}
	if json.Unmarshal(raw, &manifest) != nil || len(manifest.AllowedExtensions) != 1 {
		return control.Check{ID: "browser_bridge", Status: control.Warn, Message: "browser bridge manifest unreadable or not pinned to one extension", Hint: "reinstall with: shellbeam browser-host install"}
	}
	if _, err := os.Stat(manifest.Path); err != nil {
		return control.Check{ID: "browser_bridge", Status: control.Warn, Message: "browser bridge host binary missing", Hint: manifest.Path}
	}
	return control.Check{ID: "browser_bridge", Status: control.Pass, Message: fmt.Sprintf("browser bridge manifest pinned to %s, protocol %d", manifest.AllowedExtensions[0], protocol.ProtocolVersion)}
}
```

Add imports `bridgeapp "github.com/maemreyo/shellbeam/internal/app/browserbridge"` and `protocol "github.com/maemreyo/shellbeam/internal/core/browserbridge"`, then append the check inside `doctorReport` next to the existing checks:

```go
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		report.Checks = append(report.Checks, browserBridgeCheck(runtime.GOOS, home))
	}
```

adding the `runtime` import if it is not already present in that file.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/shellbeam/ -run TestBrowserBridgeCheck -count=1 -v`
Expected: PASS, three tests.

- [ ] **Step 5: Commit**

```bash
git add cmd/shellbeam/doctor.go cmd/shellbeam/browser_bridge_doctor_test.go
git commit -m "feat: report browser bridge bootstrap state in doctor"
```

---

### Task 11: Contract boundary tests

**Files:**
- Create: `tests/contract/browser_bridge_boundary_test.go`

**Interfaces:**
- Consumes: the whole `internal/core/browserbridge` and `internal/app/browserbridge` surface.
- Produces: nothing; this task adds enforcement only.

These mirror the existing completion-truth boundary test in `tests/contract/verification_truth_boundary_test.go`. Read that file first and follow its structure for locating and scanning source files.

- [ ] **Step 1: Write the failing test**

```go
package contract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBrowserBridgeSurfaceForbidsJudgmentFields keeps the bridge a source of
// literal facts. A field whose name encodes a policy judgment would move the
// continuation decision from the extension into ShellBeam.
func TestBrowserBridgeSurfaceForbidsJudgmentFields(t *testing.T) {
	forbidden := []string{"mechanical_blockers", "should_continue", "needs_attention", "is_stuck", "task_complete", "work_complete", "safe_to_finish"}
	for _, dir := range []string{"internal/core/browserbridge", "internal/app/browserbridge", "internal/adapter/browserbridge"} {
		entries, err := os.ReadDir(repoPath(t, dir))
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(repoPath(t, dir), entry.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			for _, key := range forbidden {
				if strings.Contains(string(raw), key) {
					t.Fatalf("%s/%s contains judgment field %q", dir, entry.Name(), key)
				}
			}
		}
	}
}

// TestBrowserBridgeDoesNotReuseTheGenericPassthrough proves the bridge never
// imports the MCP bridge handler, which forwards a caller-supplied action.
func TestBrowserBridgeDoesNotReuseTheGenericPassthrough(t *testing.T) {
	for _, dir := range []string{"internal/core/browserbridge", "internal/app/browserbridge", "internal/adapter/browserbridge", "cmd/shellbeam-browser-host"} {
		entries, err := os.ReadDir(repoPath(t, dir))
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(repoPath(t, dir), entry.Name()))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if strings.Contains(string(raw), "internal/app/bridge\"") {
				t.Fatalf("%s/%s imports the generic bridge handler", dir, entry.Name())
			}
		}
	}
}

// TestBrowserBridgeActionsAreLimitedToTheDeclaredReads pins the read plans to
// the five reads this design authorizes. A new action here is a design change.
func TestBrowserBridgeActionsAreLimitedToTheDeclaredReads(t *testing.T) {
	allowed := map[string]bool{"inspect.activity": true, "inspect.sessions": true, "inspect.events": true, "inspect.verification": true, "inspect.structured": true}
	dir := repoPath(t, "internal/app/browserbridge")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		for _, fragment := range strings.Split(string(raw), `Action: "`)[1:] {
			action := fragment[:strings.Index(fragment, `"`)]
			if !allowed[action] {
				t.Fatalf("%s uses undeclared daemon action %q", entry.Name(), action)
			}
		}
	}
}
```

The `contract` package already has a module-root helper, `repoRoot(t)` in `tests/contract/media_privacy_test.go`. Reuse it rather than adding a second way to find the root — add only this two-line wrapper at the top of the new file:

```go
func repoPath(t *testing.T, rel string) string {
	t.Helper()
	return filepath.Join(repoRoot(t), rel)
}
```

`tests/contract/verification_truth_boundary_test.go` is the model to follow for structure; read it before writing, and note that it scans an explicit path list rather than walking directories, which is the safer pattern when a new file could otherwise silently escape the scan.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./tests/contract/ -run TestBrowserBridge -v`
Expected: FAIL — `undefined: repoPath`, or a real violation if one exists.

- [ ] **Step 3: Write minimal implementation**

Add the `repoPath` helper if the package has none, then fix any violation the tests reveal in the packages written in Tasks 1–8. No new production behavior is added by this task; the tests must pass against the code as designed.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./tests/contract/ -count=1 -v`
Expected: PASS, including the three new tests and every pre-existing contract test.

- [ ] **Step 5: Commit**

```bash
git add tests/contract/browser_bridge_boundary_test.go
git commit -m "test: pin browser bridge literal-fact and no-passthrough boundaries"
```

---

### Task 12: Final gates

**Files:**
- Modify: none expected. Fix whatever the gates surface.

- [ ] **Step 1: Run the structural gate**

Run: `go run ./tools/devctl check`
Expected: no findings. Fix any file-length or function-length violation by splitting along the boundaries in the File Structure section.

- [ ] **Step 2: Run the dirty-scope test gate**

Run: `go run ./tools/devctl test --dirty --base origin/main`
Expected: PASS.

- [ ] **Step 3: Run the race detector on the new packages**

Run: `go test -race ./internal/core/browserbridge/ ./internal/app/browserbridge/ ./internal/adapter/browserbridge/ -count=1`
Expected: PASS.

- [ ] **Step 4: Run the full suite once, deliberately**

Run: `go test ./... -count=1`
Expected: PASS with no new failures against the recorded baseline.

- [ ] **Step 5: Commit any gate fixes**

```bash
git add -A
git commit -m "chore: satisfy structural and test gates for browser bridge v1"
```

---

## Out of scope for this plan

Named here so a later reader does not mistake absence for oversight. Each is deferred by the spec, not by convenience.

- Autonomous continuation of any kind (spec §27; P1).
- Automatic controller takeover, and any canonical conversation-lease record (spec §29; P2).
- Completion-marker parsing (spec §17; P1).
- Budget enforcement; only telemetry counters exist, and they live in the extension (spec §24).
- Durable resume across browser restart (spec §29; P3).
- Any task envelope or `envelope_id` (spec §29; P4).
- A persistent native port with request multiplexing; revisit only if measurement proves spawn cost dominates (spec §8, alternative D).
- Rate limiting inside the host or daemon. Admission is the extension's responsibility because a connectionless host holds no state and the daemon has no read-path budget (spec §20).
