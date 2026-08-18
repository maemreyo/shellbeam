# Interactive Handoff H0 tmux Provider Qualification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove or disprove that a qualified tmux Control Mode topology can satisfy the frozen Human–Agent Interactive Session Handoff provider semantics P0–P15 without adding any public delegated-session feature.

**Architecture:** H0 is a provider-feasibility program, not H1 implementation. A non-shipped Go qualification harness drives a ShellBeam-private tmux server, real PTYs, raw Control Mode connections, and bounded fault/stress scenarios; it emits machine-readable evidence plus a tracked Markdown verdict. Production `internal/**`, public MCP/IPC/schema, daemon composition, and `go.mod` stay unchanged. Raw tmux semantics are authoritative; a `gotmuxcc` candidate is evaluated only in an isolated temporary module after the raw lane is understood.

**Tech Stack:** repository toolchain Go 1.26.6; existing `github.com/creack/pty v1.1.24`; system tmux selected by exact absolute path; raw tmux Control Mode; optional isolated wrapper candidate `github.com/atomicstack/gotmuxcc@v0.1.4` (tag origin `440c9d00c0d094cc4dde1eb28ff3a534ceefd98b`, module sum `h1:WmFsKnomT+Zif4WxNfVH+zNu1dXLnhT0+1f1N+HJags=`), never added to root `go.mod` during H0.

**Spec:** `docs/superpowers/specs/2026-08-18-human-agent-interactive-session-handoff-design.md` at `5351215de2c02ac61ac82751c1680a35744047af`.

## Global Constraints

- H0 freezes/measures **HOW** tmux can satisfy the already-frozen master semantics; it does not reopen semantic debate unless a gate proves the contract impossible.
- No public `session_mode`, handoff action, capability advertisement, daemon route, persistent-TTY runtime, terminal launcher, shell adapter, or production interactive-session provider is implemented in this plan.
- No root `go.mod`/`go.sum` change. No production package imports the H0 harness.
- One private tmux server/socket per qualification run, `-f /dev/null` or an H0-owned empty config, under a temporary user-only directory. Never touch the user's default tmux server/config.
- Exact tmux executable path, version, SHA-256, OS/arch, Go version, Git HEAD, and test topology are recorded in every evidence report.
- H0 may inspect raw provider output, including deterministic fake canaries, but tracked evidence must never contain real credentials or unrelated environment values.
- `capture-pane` is forbidden as a privacy recovery mechanism. A test may use direct pane/process facts only when explicitly testing a non-privacy property; privacy PASS must come from live observation-path evidence.
- Attachment/switch/reconnect tests use `-E`/equivalent and prove no delegated session-environment mutation.
- `IngressFenceProof` proves no **new** old-authority input is admitted after the fence point; H0 must not claim PTY/application quiescence.
- `P3`, `P4`, `P5`, `P6`, `P14`, and `P15` are genuine gates. If any remains FAIL after the qualified native candidate mechanisms in this plan are exhausted, final H0 verdict is FAIL and H1 is blocked.
- Native macOS and native Linux evidence are both required for an overall cross-platform H0 PASS. A missing native lane is `NOT_RUN`, never inferred from cross-build. H1 remains blocked unless a later approved scope amendment explicitly narrows platforms.
- Current planning-host observation (`/opt/homebrew/bin/tmux`, tmux 3.6a) is not a product path/version assumption. Execution always receives an exact `--tmux /absolute/path` and re-records identity.
- Raw `.build/interactive-handoff-h0/**` is ignored evidence scratch space. Tracked final evidence consists of a deterministic machine gate `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json` plus a Markdown rendering of that same gate. Native test cases skip during ordinary repository tests unless `SHELLBEAM_H0_TMUX` is set to an absolute executable; an H0 qualification lane MUST set it and treats a missing tmux as NOT_RUN/FAIL evidence, not a silent skip.
- `H1_ALLOWED` is machine-derived only. The Markdown line is explanatory and has no authority by itself; H1 must validate the tracked gate JSON with the H0 verifier and bind its SHA-256.
- No push, PR, merge, rebase, reset, or stash as part of this plan unless separately requested.
- Every tracked-code task follows RED -> expected failure -> minimal GREEN -> focused/race verification -> coherent commit. Native qualification failures are recorded rather than “fixed” by weakening assertions.

## Responsibility Map

- `tools/interactive-handoff-h0/types.go`: closed H0 report/result schema and P0–P15 IDs.
- `tools/interactive-handoff-h0/control.go`: minimal raw Control Mode process/stream parser and command correlation; no reusable production adapter claim.
- `tools/interactive-handoff-h0/tmux_native.go`: private server/session/control-client/human-PTY fixture mechanics.
- `tools/interactive-handoff-h0/report.go`: deterministic JSON/Markdown evidence rendering.
- `tools/interactive-handoff-h0/scenario_*.go`: reusable H0-only P0–P15 probe functions called by both tests and the final `run` command.
- `tools/interactive-handoff-h0/*_test.go`: parser and native P0–P15 qualification assertions.
- `tools/interactive-handoff-h0/testdata/gotmuxcc-v0.1.4/main.go`: isolated wrapper probe source copied into `.build`; ignored by ordinary Go package discovery because it is under `testdata`.
- `.build/interactive-handoff-h0/<platform>/`: raw logs, event traces, JSON report, temporary tmux socket/config, wrapper temp module.
- `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md`: exact tracked qualification result, including PASS/FAIL/NOT_RUN and architecture-fork recommendation.
- `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json`: deterministic provider-qualification gate consumed by H1; binds exact spec commit, platform report digests, provider/topology selection, P0–P15, genuine gates, and derived `h1_allowed`.

---

### Task 1: Build the H0-only raw Control Mode harness and deterministic evidence schema

**Files:**
- Create: `tools/interactive-handoff-h0/main.go`
- Create: `tools/interactive-handoff-h0/types.go`
- Create: `tools/interactive-handoff-h0/control.go`
- Create: `tools/interactive-handoff-h0/control_test.go`
- Create: `tools/interactive-handoff-h0/report.go`
- Create: `tools/interactive-handoff-h0/report_test.go`

**Interfaces:**
- Consumes: exact tmux executable path and repository Git identity.
- Produces:

```go
type Status string

const (
    StatusPass   Status = "PASS"
    StatusFail   Status = "FAIL"
    StatusNotRun Status = "NOT_RUN"
)

type ProbeResult struct {
    ID      string            `json:"id"`
    Status  Status            `json:"status"`
    Summary string            `json:"summary"`
    Facts   map[string]string `json:"facts,omitempty"`
}

type Report struct {
    SchemaVersion int           `json:"schema_version"`
    GitHead       string        `json:"git_head"`
    GOOS          string        `json:"goos"`
    GOARCH        string        `json:"goarch"`
    GoVersion     string        `json:"go_version"`
    TmuxPath      string        `json:"tmux_path"`
    TmuxVersion   string        `json:"tmux_version"`
    TmuxSHA256    string        `json:"tmux_sha256"`
    Results       []ProbeResult `json:"results"`
    Verdict       Status        `json:"verdict"`
}
```

The final renderer additionally emits a closed tracked gate:

```go
type QualificationGate struct {
    SchemaVersion       int             `json:"schema_version"`
    GateKind            string          `json:"gate_kind"` // "provider_qualification"
    SpecCommit          string          `json:"spec_commit"`
    RequiredPlatforms   []string        `json:"required_platforms"`
    RequiredProbeIDs    []string        `json:"required_probe_ids"`
    GenuineGateIDs      []string        `json:"genuine_gate_ids"`
    PlatformReports     []ReportBinding `json:"platform_reports"`
    ProviderID          string          `json:"provider_id"`
    ProviderVersion     int             `json:"provider_version"`
    InputFenceMechanism string          `json:"input_fence_mechanism"`
    ObservationTopology string          `json:"observation_topology"`
    ControlAdapter      string          `json:"control_adapter"`
    H1Allowed           bool            `json:"h1_allowed"`
}
```

Each `ReportBinding` carries GOOS/GOARCH, exact tmux identity, native report verdict, and SHA-256 of the raw platform report. `validateGate` recomputes `H1Allowed`; a caller cannot set it independently of the bound P0–P15/platform/provider facts.

- Produces raw ordered Control Mode events with command blocks and notifications kept distinct; no parser recovery that silently drops malformed lines.

- [ ] **Step 1: Write RED report-schema tests.**

```go
func TestReportRequiresEveryP0ThroughP15ExactlyOnce(t *testing.T) {
    got := validateReport(Report{Results: []ProbeResult{{ID: "P0", Status: StatusPass}}})
    if got == nil {
        t.Fatal("partial report accepted")
    }
}

func TestVerdictFailsOnGateFailureAndNotRunOnMissingNativeLane(t *testing.T) {
    results := passingResults()
    results[indexOf("P14")].Status = StatusFail
    if got := verdict(results); got != StatusFail {
        t.Fatalf("verdict=%q", got)
    }
}

func TestVerifyGateRejectsCallerForgedH1Allowed(t *testing.T) {
    reports := passingNativeReports()
    reports[0].Results[indexOf("P3")].Status = StatusFail
    gate := gateFromReports(reports)
    gate.H1Allowed = true // simulate manual edit of the serialized gate
    if err := verifyGate(gate, reports); err == nil {
        t.Fatal("forged h1_allowed accepted")
    }
}

func TestVerifyGateRejectsUnboundPlatformReportDigest(t *testing.T) {
    reports := passingNativeReports()
    gate := gateFromReports(reports)
    gate.PlatformReports[0].ReportSHA256 = strings.Repeat("0", 64)
    if err := verifyGate(gate, reports); err == nil {
        t.Fatal("unbound platform report digest accepted")
    }
}
```

- [ ] **Step 2: Run the RED report tests.**

Run:

```bash
go test ./tools/interactive-handoff-h0 -run 'TestReportRequires|TestVerdictFails|TestVerifyGate' -count=1
```

Expected: FAIL because `Report`, gate derivation/verification, validation, and verdict logic do not exist. The gate verifier must recompute authority from bound native reports rather than trusting serialized `h1_allowed`.

- [ ] **Step 3: Implement the closed P0–P15 schema and deterministic renderer.**

`types.go` must define exactly `P0` through `P15`, reject duplicate/unknown/missing IDs, and sort results by numeric probe number. `report.go` renders stable Markdown with an identity table, P0–P15 table, gate summary, raw-artifact relative paths, wrapper verdict, and final H0 verdict. It must not embed timestamps into semantic comparison fields.

- [ ] **Step 4: Write RED raw Control Mode parser tests.**

Use fixtures containing `%begin`, command output, `%end`, inter-block `%output`, `%message`, `%client-detached`, and `%exit`. Include malformed nested `%begin`, mismatched command number, `%error`, invalid octal output, and EOF mid-block.

```go
func TestControlParserPreservesCommandAndNotificationOrder(t *testing.T) {
    input := "%begin 1 7 0\nanswer\n%end 1 7 0\n%output %3 abc\\012\n"
    events, err := parseControl(strings.NewReader(input))
    if err != nil { t.Fatal(err) }
    if events[0].Kind != EventCommandEnd || events[1].Kind != EventPaneOutput {
        t.Fatalf("events=%#v", events)
    }
}
```

- [ ] **Step 5: Run parser tests RED, then implement the minimal strict parser.**

Run before implementation:

```bash
go test ./tools/interactive-handoff-h0 -run 'TestControlParser' -count=1
```

Expected: FAIL. Implement only the notification/block grammar needed by H0; unknown notifications are retained as typed `EventUnknownNotification` with raw bounded text, not dropped.

- [ ] **Step 6: Implement identity capture plus the deterministic `render` CLI; defer `run` wiring until all probes exist.**

Task 1 CLI is exact and non-interactive:

```text
interactive-handoff-h0 render \
  --input .build/interactive-handoff-h0/darwin/report.json \
  --input .build/interactive-handoff-h0/linux/report.json \
  --gate-json docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json \
  --markdown docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md

interactive-handoff-h0 verify-gate \
  --gate-json docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json
```

Implement `collectIdentity(tmuxPath string)` now: it requires an absolute regular executable, runs `<tmux> -V`, hashes executable bytes, and records `runtime.GOOS/GOARCH`, `runtime.Version()`, plus `git rev-parse HEAD`. Task 6 adds the `run` subcommand only after P0–P15 probe functions exist, so intermediate commits never fabricate unimplemented probe results.

- [ ] **Step 7: Verify Task 1 and commit.**

Run:

```bash
gofmt -w tools/interactive-handoff-h0/*.go
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./tools/interactive-handoff-h0 -count=1
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./tools/interactive-handoff-h0 -count=1
go run ./tools/devctl check
git diff --check
git add tools/interactive-handoff-h0
go run ./tools/devctl commit-gate --json
git -c core.hooksPath=.githooks commit -m "test: add interactive handoff h0 harness"
```

Expected: unit/race/devctl/commit-gate PASS; no `internal/**`, `api/**`, `cmd/**`, `go.mod`, or `go.sum` change.

---

### Task 2: Qualify P0–P3 private server, exact client control, and human ingress fencing

**Files:**
- Create: `tools/interactive-handoff-h0/tmux_native.go`
- Create: `tools/interactive-handoff-h0/scenario_identity_fence.go`
- Create: `tools/interactive-handoff-h0/scenario_identity_fence_test.go`
- Modify: `tools/interactive-handoff-h0/main.go`

**Interfaces:**
- Consumes: Task 1 parser/report types.
- Produces: P0/P1/P2/P3 `ProbeResult`s plus raw transcripts under the run raw directory.

- [ ] **Step 1: Write RED P0 fixture tests for private server identity.**

The fixture must invoke only the explicit tmux executable:

```text
<tmux> -S <temp>/tmux.sock -f /dev/null new-session -d -s h0-a 'exec /bin/sh'
```

Assert server socket exists under the H0 temp directory, group/other permission bits are zero, default tmux socket/server is not touched, and `display-message -p '#{pid}|#{socket_path}|#{version}'` binds to the exact private server.

- [ ] **Step 2: Implement `nativeFixture` with cleanup that never calls default `tmux kill-server`.**

```go
type nativeFixture struct {
    Tmux       string
    Root       string
    SocketPath string
}

func (f *nativeFixture) tmux(ctx context.Context, args ...string) ([]byte, error)
func (f *nativeFixture) close(ctx context.Context) error
```

Every call prepends `-S f.SocketPath -f /dev/null` where legal. Cleanup targets the exact socket and removes only the fixture root.

- [ ] **Step 3: Write RED P1/P2 tests using a real human PTY client.**

Use existing `github.com/creack/pty` to start:

```text
<tmux> -S <socket> -f /dev/null attach-session -E -f read-only,ignore-size -t h0-a
```

Query exact client facts with a bounded format including `#{client_name}`, `#{client_tty}`, `#{client_pid}`, `#{client_readonly}`, and `#{client_flags}`. Prove one exact client can be selected and toggled read-only/writable without changing another client.

- [ ] **Step 4: Implement exact-client flag control and negative ambiguity tests.**

Use `refresh-client -t <exact-client> -f read-only` and `refresh-client -t <exact-client> -f '!read-only'` (or the exact equivalent proven by the local tmux version). A missing/ambiguous target is P2 FAIL; name/PID heuristics never count as exact identity.

- [ ] **Step 5: Write RED P3 same-client ingress-fence stress test.**

Pane command disables terminal echo and copies stdin to stdout. Bind a dedicated H0 key (for example `C-g`) on the private server so the same human-client input stream causes tmux to toggle **that exact current client** read-only. Test sequence per iteration:

```text
write pre-fence marker A
write H0 fence key on same human PTY stream
wait until exact client reports read-only=1
write post-fence marker B
observe live pane output only
```

Pass criterion: B is never admitted after the acknowledged fence across at least 1,000 iterations. A may be observed before or after the flag acknowledgement; H0 explicitly does **not** claim A has drained from PTY/application state. Any same-client tmux binding used to trigger this measurement is **H0-only** and must not be proposed as H2 authority logic if it would let the human re-enable their own write flag without daemon authorization.

- [ ] **Step 6: Run P0–P3 native tests and record raw ordering.**

Run:

```bash
SHELLBEAM_H0_TMUX="$(command -v tmux)" \
  go test ./tools/interactive-handoff-h0 \
  -run 'TestH0P0|TestH0P1|TestH0P2|TestH0P3' -count=1 -v
```

Expected on a qualifying host: P0–P3 PASS. If P3 FAILS after exact-client/same-stream mechanics are correct, record `architecture_fork=attach_side_ingress_gate_required`; do not weaken P3 and do not begin H1.

- [ ] **Step 7: Verify/commit Task 2.**

Run focused test twice (`-count=1`, then `-count=3`) and race the tool package with `SHELLBEAM_H0_TMUX="$(command -v tmux)"` set on every native command; then run `go run ./tools/devctl check`, stage only Task 2 files, run commit-gate, and commit:

```bash
git -c core.hooksPath=.githooks commit -m "test: qualify tmux client authority primitives"
```

---

### Task 3: Qualify P4–P7 privacy scope, first-byte recovery, and environment-preserving attachment

**Files:**
- Create: `tools/interactive-handoff-h0/scenario_privacy.go`
- Create: `tools/interactive-handoff-h0/scenario_privacy_test.go`
- Modify: `tools/interactive-handoff-h0/tmux_native.go`
- Modify: `tools/interactive-handoff-h0/main.go`

**Interfaces:**
- Consumes: exact private fixture/control parser from Tasks 1–2.
- Produces: measured privacy candidate/topology facts and P4–P7 results; does not choose a production topology by assertion.

- [ ] **Step 1: Write RED P4 scope-discovery test with A/B/C panes.**

Create three delegated-session analogues with unique pane IDs and emit deterministic markers:

```text
A_PUBLIC_1
B_PUBLIC_1
C_PUBLIC_1
```

For each model-visible Control Mode client, record which pane IDs it receives by default. Then exercise both raw tmux controls available on the installed version:

```text
client flag: refresh-client -f no-output
per-pane:    refresh-client -A %<pane>:off
```

Record scope; never assume `no-output` is pane-scoped. For per-pane `off`, additionally record whether tmux stops reading the pane when all relevant control clients turn it off and whether that can introduce workload backpressure.

- [ ] **Step 2: Add candidate-topology assertions without prematurely freezing A/B/C.**

At least these candidates are measured:

```text
per_session_observer
shared_observer_with_per_pane_off
shared_observer_with_daemon_demux_simulation
```

A candidate is P4-eligible only if private A can be suppressed at the model-visible path while public B/C remain observable and no silent global suppression is mislabeled as success.

- [ ] **Step 3: Write RED P5 private-from-first-byte race.**

For each eligible candidate, race a secret canary producer against observer attachment/reconfiguration. A PASS requires the observer path to begin private before its first possible A output byte. An implementation that attaches publicly, receives any A `%output`, and only then sends `no-output`/`-A off` is FAIL even if the test harness discards the byte later.

- [ ] **Step 4: Write RED P6 reconnect/no-history-replay fault matrix.**

Sequence:

```text
private A active
kill only model-visible control observer
emit A_SECRET_DURING_GAP
start replacement observer using the candidate private-from-first-byte sequence
emit A_SECRET_AFTER_RECONNECT
restore public only after explicit forward boundary
emit A_PUBLIC_AFTER_BOUNDARY
```

PASS: both secret markers absent from every model-visible/raw public log; final public marker present; no `capture-pane`/history replay used. Include old/new observer overlap case as raw data for P15.

- [ ] **Step 5: Write P7 environment mutation positive/negative control.**

Create session environment with deterministic fake values:

```text
SSH_AUTH_SOCK=/h0/session-A
DISPLAY=:h0-session
```

Launch attaching client with different fake values. Negative control attaches without `-E` and proves tmux's `update-environment` can change session environment on the tested version. Actual H0 attach uses `attach-session -E`; `switch-client -E`/recovery equivalent must leave session values byte-exact. Do not inspect unrelated environment variables.

- [ ] **Step 6: Run P4–P7 native qualification.**

```bash
SHELLBEAM_H0_TMUX="$(command -v tmux)" \
  go test ./tools/interactive-handoff-h0 \
  -run 'TestH0P4|TestH0P5|TestH0P6|TestH0P7' -count=1 -v
```

If no measured privacy candidate passes P4/P5/P6, final H0 is FAIL with `architecture_fork=privacy_topology_required`; do not invent a passing topology in prose.

- [ ] **Step 7: Verify/commit Task 3.**

Run focused repeat/race with `SHELLBEAM_H0_TMUX="$(command -v tmux)"` set on every native command, then `devctl check`, staged commit-gate, and:

```bash
git -c core.hooksPath=.githooks commit -m "test: qualify tmux private observation boundary"
```

---

### Task 4: Qualify P8–P9 shell-independent HumanControl reachability

**Files:**
- Create: `tools/interactive-handoff-h0/scenario_humancontrol.go`
- Create: `tools/interactive-handoff-h0/scenario_humancontrol_test.go`
- Modify: `tools/interactive-handoff-h0/tmux_native.go`

**Interfaces:**
- Consumes: real human PTY client fixture.
- Produces: evidence for local controls while writable and while fenced/read-only; does not implement ShellBeam's H2 control protocol.

- [ ] **Step 1: Write RED P8 writable-state OOB test using a tmux-native synchronization primitive.**

On the private tmux server, bind a dedicated H0 key to signal a unique `wait-for` channel; start another exact private-server tmux process waiting on that channel. Send the key through the human PTY while a foreground child owns pane stdin.

PASS requires:

```text
HumanControl signal observed
foreground child receives none of the control-key bytes
no shell prompt required
no permanent user config changed
```

This tests reachability, not final H2 wire format.

- [ ] **Step 2: Prove a normal shell command is the wrong fallback.**

Run foreground `/bin/cat`, write literal `shellbeam handoff ready\n`, and assert it goes to pane stdin rather than a local control plane. Keep this as a regression against reintroducing shell-dependent manual ready.

- [ ] **Step 3: Write RED P9 read-only/fenced-state reachability test.**

First prove an arbitrary writable-state H0 binding no longer fires once the client is tmux read-only. Then prove a read-only-allowed detach path can return control to the H0 attach wrapper/local PTY owner without injecting bytes into the pane. From that local surface, record `resume`, `terminate`, and `status` as reachable actions; reattach remains an H0 fixture operation, not production UX.

- [ ] **Step 4: Assert no ingress proxy is introduced by H0 when detach-to-local-control passes.**

The tracked harness may own the attach process/PTY and local menu simulation, but must not become a general byte-forwarding terminal broker. If P9 cannot pass via tmux-native + detach-to-local-control mechanics, record `architecture_fork=attach_side_control_or_ingress_gate_required` rather than silently proxying all input.

- [ ] **Step 5: Run/verify/commit P8–P9.**

```bash
SHELLBEAM_H0_TMUX="$(command -v tmux)" \
  go test ./tools/interactive-handoff-h0 \
  -run 'TestH0P8|TestH0P9' -count=3 -v
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./tools/interactive-handoff-h0 -run 'TestH0P8|TestH0P9' -count=1
```

Then `devctl check`, staged commit-gate, commit:

```bash
git -c core.hooksPath=.githooks commit -m "test: qualify tmux human control reachability"
```

---

### Task 5: Qualify P10–P12 resize isolation, crash/reconnect, and ACK/output ordering

**Files:**
- Create: `tools/interactive-handoff-h0/scenario_ordering_resize_restart.go`
- Create: `tools/interactive-handoff-h0/scenario_ordering_resize_restart_test.go`
- Modify: `tools/interactive-handoff-h0/control.go`
- Modify: `tools/interactive-handoff-h0/tmux_native.go`

**Interfaces:**
- Produces exact ordering traces used later by P15 and wrapper comparison.

- [ ] **Step 1: Write P10 real-PTY resize matrix.**

Create human clients with 120x40 and 90x30 sizes plus a Control Mode observer. Qualify `ignore-size`/control-client sizing so attaching/fencing a passive observer does not unexpectedly resize a foreground TUI. Use `pty.Setsize` for human PTY changes and query exact `#{pane_width}x#{pane_height}` after each event.

PASS criteria must state the chosen provider policy and prove deterministic pane size across attach -> human writable -> human read-only -> detach -> reattach.

- [ ] **Step 2: Write P11 crash/reconnect identity tests.**

Fault separately:

```text
Control Mode client process dies
human terminal client dies
daemon-analogue observer restarts
private tmux server dies
```

PASS for recoverable client loss requires same exact tmux server/session/window/pane IDs. Server loss is reported as provider loss; never recreate same friendly name and call it continuation.

- [ ] **Step 3: Write P12 command-ACK/output-order tests around privacy controls.**

For raw Control Mode, record ordered command numbers and notifications around:

```text
emit BEFORE
refresh-client -A %pane:off   # or candidate control
ACK (%end)
emit DURING
restore output
ACK
emit AFTER
```

The parser must prove notifications never occur inside command blocks, but H0 must separately measure whether a provider ACK gives the exact privacy/ingress ordering needed by the candidate. Do not infer cross-client total ordering from one control ACK.

- [ ] **Step 4: Add high-output/backpressure measurement.**

Emit bounded multi-megabyte deterministic output while toggling per-pane/client flow controls. Record whether `refresh-client -A pane:off` stops tmux reading from the pane when all control clients turn it off and whether a human display client changes that result. Any mechanism that can silently deadlock the workload is ineligible without a documented topology that prevents that condition.

- [ ] **Step 5: Run/repeat/race P10–P12 with `SHELLBEAM_H0_TMUX="$(command -v tmux)"` set on every native command, then commit.**

Commit message:

```text
test: qualify tmux ordering and resize semantics
```

---

### Task 6: Qualify P13–P15 leak convergence, multi-session isolation, and observer-overlap faults

**Files:**
- Create: `tools/interactive-handoff-h0/scenario_stress_privacy.go`
- Create: `tools/interactive-handoff-h0/scenario_stress_privacy_test.go`
- Create: `tools/interactive-handoff-h0/fd_darwin.go`
- Create: `tools/interactive-handoff-h0/fd_linux.go`
- Modify: `tools/interactive-handoff-h0/report.go`

**Interfaces:**
- Produces final raw-provider gate results P13/P14/P15.

- [ ] **Step 1: Implement cross-platform self-FD counters for the H0 process.**

Darwin counts entries in `/dev/fd`; Linux counts `/proc/self/fd`. Also record `runtime.NumGoroutine()`, exact tmux `list-sessions`, `list-clients`, `list-panes`, child-process count scoped to the private server PID, and files/sockets under the H0 temp root.

- [ ] **Step 2: Write P13 100-cycle lifecycle stress.**

Each cycle creates session/pane/control observer/human attach, performs fence + privacy toggle + detach/reconnect, then destroys only H0 resources. Compare baseline, periodic samples, and final convergence. PASS requires no monotonic growth in tmux sessions/panes/clients, H0 goroutines, H0 FDs, helper processes, or runtime files/sockets after cleanup.

- [ ] **Step 3: Write P14 three-session privacy isolation stress.**

Run concurrently:

```text
A = private, emits A_SECRET_<n>
B = public noisy, emits B_PUBLIC_<n>
C = public noisy, emits C_PUBLIC_<n>
```

Across at least 100 privacy on/off cycles, every A secret marker is absent from model-visible logs and every expected B/C marker remains present in order permitted by its own pane stream. "Suppress everything" is an explicit FAIL.

- [ ] **Step 4: Write P15 observer replacement/overlap fault matrix.**

Fault points include:

```text
old observer alive when new observer starts
old observer private, new observer startup
old observer dies before new observer private ACK
new observer private ACK before old observer close
rapid repeated reconnect
observer receives server-exit during private state
```

PASS requires **every** model-visible observer capable of seeing A to be private before A can emit private bytes. A single secret canary on any old/new observer path is FAIL.

- [ ] **Step 5: Wire the final `run` subcommand now that P0–P15 probe functions exist.**

Exact command:

```text
interactive-handoff-h0 run \
  --tmux /absolute/path/to/tmux \
  --raw-dir .build/interactive-handoff-h0/<platform> \
  --json .build/interactive-handoff-h0/<platform>/report.json
```

`run` calls every P0–P15 probe exactly once, validates the complete report, writes raw traces only below `--raw-dir`, then atomically writes JSON. It refuses a raw directory outside `.build/interactive-handoff-h0/`; a missing/failed probe is a typed FAIL/NOT_RUN result, not omission.

- [ ] **Step 6: Run stress twice and race once.**

```bash
SHELLBEAM_H0_TMUX="$(command -v tmux)" \
  go test ./tools/interactive-handoff-h0 \
  -run 'TestH0P13|TestH0P14|TestH0P15' -count=2 -v
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./tools/interactive-handoff-h0 \
  -run 'TestH0P13|TestH0P14|TestH0P15' -count=1
```

- [ ] **Step 7: Verify/commit Task 6.**

Commit:

```text
test: stress tmux handoff qualification boundaries
```

At this point raw-provider P0–P15 must each have a concrete PASS/FAIL result on the native host. Do not run wrapper qualification as a substitute for a raw FAIL.

---

### Task 7: Qualify `gotmuxcc` v0.1.4 as an optional adapter convenience without changing root dependencies

**Files:**
- Create: `tools/interactive-handoff-h0/testdata/gotmuxcc-v0.1.4/main.go`
- Create: `tools/interactive-handoff-h0/wrapper_candidate_test.go`
- Modify: `tools/interactive-handoff-h0/report.go`

**Interfaces:**
- Consumes: raw tmux P0–P15 results and exact private socket fixture conventions.
- Produces wrapper verdict `PASS`, `FAIL`, or `NOT_RUN_RAW_GATE_FAILED`; never changes the raw H0 verdict.

- [ ] **Step 1: Freeze/verify candidate identity in the test.**

Expected candidate identity:

```text
module  github.com/atomicstack/gotmuxcc
version v0.1.4
origin  440c9d00c0d094cc4dde1eb28ff3a534ceefd98b
sum     h1:WmFsKnomT+Zif4WxNfVH+zNu1dXLnhT0+1f1N+HJags=
license MIT
```

The test executes `go mod download -json github.com/atomicstack/gotmuxcc@v0.1.4` into the normal Go module cache and rejects identity mismatch. It does not edit root `go.mod`/`go.sum`.

- [ ] **Step 2: Add an isolated `testdata` wrapper probe.**

The probe imports `github.com/atomicstack/gotmuxcc/gotmuxcc` and exercises only APIs needed to compare with raw facts:

```go
t, err := gotmuxcc.NewTmux(socketPath)
if err != nil { log.Fatal(err) }
defer t.Close()

if _, err := t.Command("display-message", "-p", "#{version}"); err != nil {
    log.Fatal(err)
}
if err := t.DisablePaneOutput(paneID); err != nil { log.Fatal(err) }
if err := t.EnablePaneOutput(paneID); err != nil { log.Fatal(err) }
if err := t.SetControlFlags("no-output"); err != nil { log.Fatal(err) }
if err := t.SetControlFlags("!no-output"); err != nil { log.Fatal(err) }
```

The execution test copies this source to `.build/interactive-handoff-h0/gotmuxcc-v0.1.4/`, creates a temporary module there, requires exactly v0.1.4, and runs it against an H0 private tmux socket.

- [ ] **Step 3: Compare wrapper behavior to raw P4/P5/P6/P11/P12 facts.**

Verify command correlation, initial handshake, `DisablePaneOutput` (`refresh-client -A pane:off` semantics), client-level flags, close/reconnect behavior, malformed transport/error propagation, large output, and resource cleanup. A wrapper PASS cannot convert a raw provider FAIL into PASS.

- [ ] **Step 4: Record dependency impact facts without adding it.**

Archive under `.build/interactive-handoff-h0/wrapper/`:

```text
go mod graph
go list -deps -json
go list -m -json all
go mod why -m github.com/atomicstack/gotmuxcc
```

Record transitive module count and exact licenses. Wrapper acceptance requires: no incompatible/non-redistributable dependency license; no CGO requirement introduced by the wrapper lane; `Close()` converges without process/goroutine/FD leak; command errors/malformed transport propagate as failures; wrapper logging never contains private canaries; wrapper does not use `capture-pane` to satisfy privacy; and its P4/P5/P6/P11/P12 behavior matches the raw provider facts. Binary/dependency-count delta is recorded as a trade-off, not given an invented numeric threshold. The final evidence recommends either `gotmuxcc v0.1.4 candidate acceptable` or `own thin Control Mode adapter`; H0 does not modify production dependencies.

- [ ] **Step 5: Verify/commit Task 7.**

Run the isolated candidate test, H0 tool unit/race tests, `devctl check`, staged commit-gate, then commit:

```text
test: qualify gotmuxcc control mode candidate
```

If raw H0 gates are FAIL, wrapper scenario may be recorded `NOT_RUN_RAW_GATE_FAILED`; still verify candidate identity/license metadata if network/module cache permits, but do not present it as a provider solution.

---

### Task 8: Produce native macOS + Linux evidence, exact H0 verdict, and hard handoff gate

**Files:**
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json`
- Create: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md`
- Modify only if evidence generation reveals a harness bug: `tools/interactive-handoff-h0/**`
- Raw ignored artifacts: `.build/interactive-handoff-h0/**`

**Interfaces:**
- Produces the sole H0 PASS/FAIL/NOT_RUN decision consumed by later H1 planning.

- [ ] **Step 1: Run the complete native macOS lane from clean tracked source.**

```bash
TMUX="$(command -v tmux)"
test -n "$TMUX"
TMUX="$(python3 -c 'import os,sys; print(os.path.realpath(sys.argv[1]))' "$TMUX")"

SHELLBEAM_H0_TMUX="$TMUX" go test ./tools/interactive-handoff-h0 -count=1
SHELLBEAM_H0_TMUX="$TMUX" go test -race ./tools/interactive-handoff-h0 -count=1

go run ./tools/interactive-handoff-h0 run \
  --tmux "$TMUX" \
  --raw-dir .build/interactive-handoff-h0/darwin \
  --json .build/interactive-handoff-h0/darwin/report.json
```

Expected: report has exactly P0–P15. Preserve failures; do not rerun only until green without diagnosing and recording the cause.

- [ ] **Step 2: Run the same complete lane on native Linux.**

Use the same committed source and exact commands with raw directory `.build/interactive-handoff-h0/linux`; actual Linux `GOARCH` is recorded inside the report rather than encoded in the path. Cross-build does not count. If no native Linux runner is available, create a Linux report with platform identity and `P0`–`P15 = NOT_RUN`, overall `NOT_RUN`; do not claim cross-platform H0 PASS.

- [ ] **Step 3: Render the tracked evidence report from platform JSON plus wrapper facts.**

```bash
go run ./tools/interactive-handoff-h0 render \
  --input .build/interactive-handoff-h0/darwin/report.json \
  --input .build/interactive-handoff-h0/linux/report.json \
  --gate-json docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json \
  --markdown docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md

go run ./tools/interactive-handoff-h0 verify-gate \
  --gate-json docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json
```

The renderer must include:

```text
frozen spec path + commit 5351215...
platform identity and exact tmux hashes
P0-P15 per platform
P3/P4/P5/P6/P14/P15 gate block
measured privacy topology candidates
per-pane off/backpressure facts
HumanControl reachability facts
attach -E environment result
wrapper candidate identity/verdict
raw artifact locations
final H0 verdict
architecture fork recommendation if FAIL
H1_ALLOWED = true|false  # rendering of gate JSON only
```

The gate JSON is the authority. It binds `gate_kind=provider_qualification`, exact master-spec commit, exact platform-report hashes, provider/version/topology, P0–P15, and genuine-gate IDs. `h1_allowed=true` is accepted only when `verify-gate` recomputes true from required native PASS lanes and every genuine gate PASS.

- [ ] **Step 4: Run final anti-goal scans.**

```bash
git diff --name-only 5351215de2c02ac61ac82751c1680a35744047af...HEAD
rg -n 'session_mode|handoff\.request|handoff\.abort' internal api cmd || true
git diff 5351215de2c02ac61ac82751c1680a35744047af -- go.mod go.sum
git diff --check
```

Expected: H0 changes are qualification harness/evidence/plan only; no public feature route/schema/capability and no root dependency change.

- [ ] **Step 5: Run exact repository gates for H0 source.**

```bash
go mod verify
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test ./tools/interactive-handoff-h0 -count=1
SHELLBEAM_H0_TMUX="$(command -v tmux)" go test -race ./tools/interactive-handoff-h0 -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base 5351215de2c02ac61ac82751c1680a35744047af --json
git diff --check
```

Do not substitute `go test ./...` for missing native P0–P15 evidence. Full repo tests may be run if selected by repo policy, but they do not prove H0 provider semantics.

- [ ] **Step 6: Stage exact H0 tracked outputs and run commit gate.**

```bash
git add tools/interactive-handoff-h0 \
  docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json \
  docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md \
  docs/superpowers/plans/2026-08-18-interactive-handoff-h0-tmux-qualification.md
git diff --cached --check
go run ./tools/devctl commit-gate --json
```

- [ ] **Step 7: Commit H0 qualification result regardless of PASS/FAIL, then hard-stop.**

```bash
git -c core.hooksPath=.githooks commit -m "test: qualify tmux interactive handoff provider"
```

Postcommit:

```bash
git status --short --branch
git rev-parse HEAD
shasum -a 256 docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json \
  docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md
go run ./tools/interactive-handoff-h0 verify-gate \
  --gate-json docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json
```

If the verified gate derives `h1_allowed=false`, **STOP**. Do not write/execute H1 implementation under this plan. Report which exact gate/topology failed and the required architecture fork. A manually edited Markdown `H1_ALLOWED=true` never unlocks H1.

## H0 Completion Gate

H0 is complete only when one exact tracked qualification result can answer all of the following without speculation:

1. P0 private server/socket/config is isolated from user tmux state.
2. P1 exact client identity is stable and queryable.
3. P2 one exact human client can be made read-only/writable without mutating unrelated clients.
4. P3 proves post-fence human input is not admitted, without overclaiming application quiescence.
5. P4 identifies the actual privacy suppression scope/topology, including per-pane `refresh-client -A` and client-level flags where supported.
6. P5 every selected private observer is private from first possible byte.
7. P6 private reconnect/recovery never replays hidden history and never exposes a gap canary.
8. P7 attach/switch/recovery preserves session environment with `-E`/equivalent.
9. P8 shell-independent OOB control is reachable while human-writable.
10. P9 required local controls remain reachable while the tmux client is fenced/read-only, without requiring arbitrary read-only bindings.
11. P10 resize/ignore-size policy does not unexpectedly perturb the running pane.
12. P11 client loss/reconnect preserves exact provider identity; server loss is not guessed into continuation.
13. P12 command ACK/output ordering and flow-control/backpressure facts are measured, not assumed.
14. P13 repeated lifecycle stress converges in clients/panes/sessions/processes/FDs/goroutines/runtime files.
15. P14 private noisy A never leaks while public noisy B/C remain correctly observable.
16. P15 old/new observer overlap/replacement creates no exposure window.
17. `gotmuxcc` is either independently qualified as a replaceable adapter convenience or explicitly rejected in favor of a thin ShellBeam-owned protocol adapter; raw provider evidence remains authoritative.
18. Native macOS and Linux status is explicit. Missing native evidence is `NOT_RUN`, not inferred.
19. No H0 tracked change implements or advertises the public feature, changes root dependencies, or weakens existing direct/persistent/resource/hermetic semantics.
20. The tracked machine gate validates with `gate_kind=provider_qualification` and derives `h1_allowed=true|false`; only verified `true` permits H1. Markdown is a rendering, not gate authority.
