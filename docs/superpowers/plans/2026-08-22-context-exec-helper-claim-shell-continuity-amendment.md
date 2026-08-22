# Context Exec Helper-Claim Shell Continuity Amendment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make helper authentication preserve the already-qualified delegated-shell identity while still freshly proving provider/owner/privacy/cwd authority and exact local helper ancestry before any context-exec child can spawn.

**Architecture:** Keep the existing qualified shell probe twice before helper-launch delivery. Before launch, capture a server-owned `ShellContinuityExpectation` containing the qualified shell/runtime and pane-shell process facts. After the helper becomes foreground, the host peer verifier emits a typed `ShellContinuityProof`; the server and app binder compare proof to that pre-launch expectation, while a separate claim-time authority snapshot rechecks live provider/epoch/owner/privacy/cwd without re-running volatile shell-family detection from `pane_current_command`.

**Tech Stack:** Go 1.26.x, Unix domain sockets, Darwin process/TTY inspection, tmux control mode, existing ShellBeam context-exec core/app/adapter layers.

**Spec:** `docs/superpowers/specs/2026-08-22-context-exec-helper-claim-shell-continuity-amendment-design.md` (commit `9baac5387b6cf7438b93a85482180525b9357f4b`, SHA-256 `3891afdf5e7f28713751884f54ae2d84506f6c427f9adab639a5812f422eaa24`)

## Global Constraints

- The approved base spec remains byte-for-byte unchanged at SHA-256 `b3b10c5481fee65118c5fc062255e6f43b7f357d15a17e1069b1f15abf52a32c`.
- `PUBLIC_SCHEMA_CHANGE=false`; no MCP/IPC/public request or response fields are added.
- `HELPER_WIRE_PROTOCOL_CHANGE=false`; the pane-visible helper command remains `shellbeam __context_exec_helper <opaque_launch_id>`.
- Never treat `shellbeam` as fish/zsh/bash and never whitelist `pane_current_command=shellbeam` as shell identity.
- Pre-launch admission still proves exact supported shell identity and safe boundary immediately before helper delivery.
- Claim-time freshly proves binding/provider generation/epoch/owner/agent ingress/privacy/cwd while shell continuity comes from local peer/parent/TTY proof.
- Stable failure mapping is closed: verifier/peer proof failure -> `context_helper_auth_failed`; provider-generation or authority-epoch mismatch -> `context_exec_stale_generation`; shell continuity, pane identity, or cwd identity mismatch/unproven -> `context_exec_boundary_unproven`; ownership drift -> `context_exec_not_agent_owned`; active/pending privacy -> `context_exec_privacy_blocked`; durable mutation ambiguity -> `context_exec_ambiguous`.
- `ShellContinuityExpectation` and `ShellContinuityProof` are internal non-bearer authority material. They are never helper-supplied and never enter public state/errors/logs.
- Peer credentials, parent process identity, TTY, helper PID, claim capability bytes, pane transcript, environment values, secret hashes/lengths, and secret-derived material never enter public output.
- CWD equality means same directory object: cleaned lexical equality first, otherwise both paths must `os.Stat` as directories and satisfy `os.SameFile`; failure is fail-closed.
- Resource enforcement and hermeticity remain `unavailable`; do not strengthen capability claims.
- Native acceptance is valid only with an explicit real tmux path, currently `/opt/homebrew/Cellar/tmux/3.6a/bin/tmux` (or the exact qualified path found on the machine). A skip is not PASS.
- This worktree already contains uncommitted Task-8 changes. Do not reset, checkout, stash, or discard them. The parent Task-8 plan owns the single final implementation commit, so amendment tasks checkpoint with tests rather than independent commits.

---

### Task 1: Define Closed Shell-Continuity Expectation and Proof Types

**Files:**
- Create: `internal/core/contextexec/continuity.go`
- Create: `internal/core/contextexec/continuity_test.go`

**Interfaces:**
- Produces: `core.ShellContinuityExpectation`
- Produces: `core.ShellContinuityProof`
- Produces: `func (ShellContinuityExpectation) Validate() error`
- Produces: `func (ShellContinuityProof) Validate() error`
- Produces: `func (ShellContinuityProof) ValidateFor(ShellContinuityExpectation) error`

- [ ] **Step 1: Write RED tests for both closed types**

Create one valid expectation and one matching proof. Assert the expectation rejects each missing/invalid stable field and the proof rejects missing helper PID, false foreground truth, zero observation time, and any mismatch against the expectation.

```go
func validShellContinuityExpectation() ShellContinuityExpectation {
    return ShellContinuityExpectation{
        SessionID:                "session_ctx",
        AuthorityEpoch:           3,
        ProviderGeneration:       "gen_ctx",
        ShellRuntimeIdentity:     "zsh:runtime_ctx",
        PaneShellPID:             4242,
        PaneShellProcessIdentity: "proc_shell_ctx",
        PaneTTY:                  "/dev/ttys042",
        HelperExecutableIdentity: "/opt/shellbeam/bin/shellbeam",
    }
}

func validShellContinuityProof(e ShellContinuityExpectation) ShellContinuityProof {
    return ShellContinuityProof{
        SessionID: e.SessionID, AuthorityEpoch: e.AuthorityEpoch,
        ProviderGeneration: e.ProviderGeneration,
        ShellRuntimeIdentity: e.ShellRuntimeIdentity,
        PaneShellPID: e.PaneShellPID,
        PaneShellProcessIdentity: e.PaneShellProcessIdentity,
        PaneTTY: e.PaneTTY,
        HelperPID: 4343,
        HelperExecutableIdentity: e.HelperExecutableIdentity,
        ForegroundProven: true,
        ObservedAt: time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC),
    }
}
```

Table mismatches for `ValidateFor` must include session, epoch, provider generation, shell runtime identity, pane-shell PID, pane-shell process identity, pane TTY, and helper executable identity.

- [ ] **Step 2: Run the RED test**

```bash
go test ./internal/core/contextexec -run ShellContinuity -count=1 -v
```

Expected: FAIL to compile because the two continuity types do not exist.

- [ ] **Step 3: Implement the two internal types**

Create `continuity.go`:

```go
type ShellContinuityExpectation struct {
    SessionID                string
    AuthorityEpoch           delegated.AuthorityEpoch
    ProviderGeneration       string
    ShellRuntimeIdentity     string
    PaneShellPID             int
    PaneShellProcessIdentity string
    PaneTTY                  string
    HelperExecutableIdentity string
}

type ShellContinuityProof struct {
    SessionID                string
    AuthorityEpoch           delegated.AuthorityEpoch
    ProviderGeneration       string
    ShellRuntimeIdentity     string
    PaneShellPID             int
    PaneShellProcessIdentity string
    PaneTTY                  string
    HelperPID                int
    HelperExecutableIdentity string
    ForegroundProven         bool
    ObservedAt               time.Time
}
```

`Validate()` must use existing package-private bounded identity helpers, require `AuthorityEpoch.Validate()`, PIDs greater than 1, absolute TTY/helper paths, non-empty stable process identity, `ForegroundProven=true`, and non-zero `ObservedAt`.

`ValidateFor` must first validate both values, then compare all pre-launch fields exactly after `filepath.Clean` for TTY/helper paths:

```go
func (p ShellContinuityProof) ValidateFor(e ShellContinuityExpectation) error {
    if err := e.Validate(); err != nil { return err }
    if err := p.Validate(); err != nil { return err }
    if p.SessionID != e.SessionID || p.AuthorityEpoch != e.AuthorityEpoch ||
        p.ProviderGeneration != e.ProviderGeneration ||
        p.ShellRuntimeIdentity != e.ShellRuntimeIdentity ||
        p.PaneShellPID != e.PaneShellPID ||
        p.PaneShellProcessIdentity != e.PaneShellProcessIdentity ||
        filepath.Clean(p.PaneTTY) != filepath.Clean(e.PaneTTY) ||
        filepath.Clean(p.HelperExecutableIdentity) != filepath.Clean(e.HelperExecutableIdentity) {
        return fmt.Errorf("context shell continuity mismatch")
    }
    return nil
}
```

- [ ] **Step 4: Run the core GREEN gate**

```bash
go test ./internal/core/contextexec -count=1
git diff --check
```

Expected: PASS.

---

### Task 2: Make Peer Authentication Produce and Forward the Typed Proof

**Files:**
- Modify: `internal/adapter/contextexec/peer.go`
- Modify: `internal/adapter/contextexec/server.go`
- Modify: `internal/adapter/contextexec/auth_test.go`
- Modify: `internal/adapter/contextexec/context_frame_test.go`

**Interfaces:**
- Changes: `ClaimExpectation` gains `Continuity core.ShellContinuityExpectation`.
- Changes: `type PeerVerifier func(context.Context, net.Conn, ClaimExpectation) (core.ShellContinuityProof, error)`.
- Changes: `type ClaimBinder func(context.Context, string, core.HelperBinding, core.ContextBinding, core.ShellContinuityExpectation, core.ShellContinuityProof, time.Time, string) (operation.ContextExecState, error)`.
- Produces: `HostPeerVerifier.Verify(...) (core.ShellContinuityProof, error)`.

- [ ] **Step 1: Write RED tests for expectation cross-binding, proof production, and forwarding**

Update `ClaimExpectation.Validate()` tests so `Continuity` must match existing identity/context/helper fields:

```go
expectation.Continuity = core.ShellContinuityExpectation{
    SessionID: expectation.Identity.SessionID,
    AuthorityEpoch: expectation.Identity.AuthorityEpoch,
    ProviderGeneration: expectation.Context.ProviderGeneration,
    ShellRuntimeIdentity: expectation.Context.ShellIdentity,
    PaneShellPID: 42,
    PaneShellProcessIdentity: "proc_shell_stable",
    PaneTTY: "/dev/ttys042",
    HelperExecutableIdentity: expectation.Helper.ExecutablePath,
}
```

Extend `TestHostPeerVerifierRequiresExactHelperAndStableParentIdentity` so successful verification returns a proof matching this expectation; all existing exact-helper/direct-parent/stable-parent/foreground failures still reject before a usable proof exists.

Add an authentication test that captures both continuity values passed to `BindClaim`:

```go
var boundExpectation core.ShellContinuityExpectation
var boundProof core.ShellContinuityProof
server := &Server{
    Expectation: expectation,
    VerifyPeer: func(context.Context, net.Conn, ClaimExpectation) (core.ShellContinuityProof, error) {
        return validContinuityProof(expectation.Continuity), nil
    },
    BindClaim: func(_ context.Context, _ string, _ core.HelperBinding, _ core.ContextBinding,
        continuity core.ShellContinuityExpectation, proof core.ShellContinuityProof,
        _ time.Time, _ string) (operation.ContextExecState, error) {
        boundExpectation, boundProof = continuity, proof
        return helperAuthenticatedState(expectation), nil
    },
}
```

Assert `boundProof.ValidateFor(boundExpectation)` succeeds before request delivery.

- [ ] **Step 2: Run the adapter RED gate**

```bash
go test ./internal/adapter/contextexec \
  -run 'HostPeerVerifier|ServerAuthenticates|ServerBindsFinalPromptContext|AuthenticatedCWD|ClaimExpectation' \
  -count=1 -v
```

Expected: FAIL to compile because verifier/binder signatures and `ClaimExpectation.Continuity` do not exist.

- [ ] **Step 3: Extend `ClaimExpectation.Validate` without changing wire frames**

Require `Continuity.Validate()` and cross-check:

```go
if e.Continuity.SessionID != e.Identity.SessionID ||
    e.Continuity.AuthorityEpoch != e.Identity.AuthorityEpoch ||
    e.Continuity.ProviderGeneration != e.Context.ProviderGeneration ||
    e.Continuity.ShellRuntimeIdentity != e.Context.ShellIdentity ||
    filepath.Clean(e.Continuity.HelperExecutableIdentity) != filepath.Clean(e.Helper.ExecutablePath) {
    return fmt.Errorf("context helper continuity expectation mismatch")
}
```

Do not serialize `Continuity` into `HelloFrame`, `ChallengeFrame`, `ContextFrame`, or `RequestFrame`; it remains daemon-internal.

- [ ] **Step 4: Change `HostPeerVerifier.Verify` to return the proof**

Preserve every current peer check. Only after same-user credentials, exact helper executable, direct parent, stable parent process identity, and foreground TTY all succeed, construct:

```go
proof := core.ShellContinuityProof{
    SessionID:                expectation.Continuity.SessionID,
    AuthorityEpoch:           expectation.Continuity.AuthorityEpoch,
    ProviderGeneration:       expectation.Continuity.ProviderGeneration,
    ShellRuntimeIdentity:     expectation.Continuity.ShellRuntimeIdentity,
    PaneShellPID:             v.ParentPID,
    PaneShellProcessIdentity: v.ParentIdentity,
    PaneTTY:                  filepath.Clean(v.PaneTTY),
    HelperPID:                pid,
    HelperExecutableIdentity: filepath.Clean(v.ExpectedHelperExecutable),
    ForegroundProven:         true,
    ObservedAt:               time.Now().UTC(),
}
if err := proof.ValidateFor(expectation.Continuity); err != nil {
    return core.ShellContinuityProof{}, fmt.Errorf("context helper continuity proof invalid")
}
return proof, nil
```

No UID, capability bytes, argv, environment, or pane bytes are copied into the proof.

- [ ] **Step 5: Thread expectation + proof through `Server.Authenticate`**

Use:

```go
continuityProof, err := s.VerifyPeer(ctx, conn, s.Expectation)
if err != nil {
    return operation.ContextExecState{}, s.authFailure("peer_unproven")
}
if err := continuityProof.ValidateFor(s.Expectation.Continuity); err != nil {
    return operation.ContextExecState{}, s.authFailure("peer_unproven")
}
```

After challenge/proof and same-directory CWD verification, call:

```go
state, err := s.BindClaim(
    ctx,
    s.Expectation.Identity.ContextExecID,
    s.Expectation.Helper,
    finalContext,
    s.Expectation.Continuity,
    continuityProof,
    now,
    ClaimVerifierDigest(capability),
)
```

Update all adapter test fakes to return a matching continuity proof whenever the test intends peer verification to succeed.

- [ ] **Step 6: Run the adapter GREEN gate**

```bash
go test ./internal/adapter/contextexec -count=1
git diff --check
```

Expected: PASS, including correlation-only, wrapper-descendant, wrong-parent, non-foreground, CWD mismatch, protocol-version, and durable-claim rejection tests.

---

### Task 3: Split Pre-Launch Shell Qualification from Claim-Time Non-Shell Authority

**Files:**
- Modify: `internal/app/contextexec/ports.go`
- Modify: `internal/app/contextexec/service.go`
- Modify: `internal/app/contextexec/service_test.go`
- Modify: `internal/app/contextexec/runtime_binding_test.go`

**Interfaces:**
- Changes: `ContextAuthority` gains `ClaimSnapshot(context.Context, core.Request) (ClaimAuthoritySnapshot, error)`.
- Produces: `ClaimAuthoritySnapshot` containing binding/provider observation/effective authority/privacy/agent-ingress/transfer truth but no shell identity.
- Changes: `RuntimeCallbacks.BindClaim` and `Service.BindClaim` accept both `core.ShellContinuityExpectation` and `core.ShellContinuityProof`.

- [ ] **Step 1: Write RED tests reproducing the foreground-helper bug at app level**

Extend `authorityFake` with separate pre-launch and claim snapshots/counters. Add a test where pre-launch shell truth is qualified zsh but the fresh claim provider observation says `CurrentCommand="shellbeam"`; claim succeeds because `ClaimSnapshot` has no shell-family probe.

```go
func TestBindClaimUsesFreshNonShellAuthorityWhenHelperIsForeground(t *testing.T) {
    req := admissionRequest()
    state := helperRequestedState(t, req)
    claim := admissionClaimAuthority(t, req)
    claim.Observation.CurrentCommand = "shellbeam"
    authority := &authorityFake{snapshot: admissionAuthority(t, req), claimSnapshot: claim}
    store := &admissionStoreFake{state: state, found: true}
    svc := NewService(Options{Store: store, Authority: authority, Helper: &helperRuntimeFake{qualified: true}})

    final := contextBindingFromExpectation(state.Expectation)
    continuity := continuityExpectationFromStateAndProvider(state, claim.Observation)
    proof := continuityProofFor(continuity)
    got, err := svc.BindClaim(context.Background(), req.ContextExecID, *state.Helper, final, continuity, proof, time.Now().UTC(), strings.Repeat("c", 64))
    if err != nil { t.Fatal(err) }
    if got.Lifecycle != core.LifecycleHelperAuthenticated || authority.claimCalls != 1 {
        t.Fatalf("bound=%#v claim_calls=%d", got, authority.claimCalls)
    }
}
```

Add table cases proving no store bind for:
- stale provider generation or epoch;
- non-agent owner/ingress;
- active or pending privacy;
- continuity expectation session/epoch/provider/shell/helper mismatch against durable reservation;
- proof mismatch against continuity expectation, including `PaneShellProcessIdentity`;
- continuity pane PID/TTY mismatch against fresh provider observation;
- different cwd directory object.

Keep the existing same-directory alias case GREEN.

- [ ] **Step 2: Run the app RED gate**

```bash
go test ./internal/app/contextexec \
  -run 'BindClaim|ExecuteSecondAuthorityDrift|ExecuteReservesExpectation' \
  -count=1 -v
```

Expected: FAIL because claim-only snapshots and continuity arguments are not yet implemented.

- [ ] **Step 3: Split the authority snapshot types in `ports.go`**

```go
type ContextAuthority interface {
    Snapshot(context.Context, core.Request) (AuthoritySnapshot, error)
    ClaimSnapshot(context.Context, core.Request) (ClaimAuthoritySnapshot, error)
}

type ClaimAuthoritySnapshot struct {
    Binding                   delegated.Binding
    ProviderRef               delegated.ProviderRef
    Observation               delegatedapp.Observation
    Authority                 delegated.EffectiveAuthority
    PrivacyProviderGeneration string
    PrivacyActive             bool
    PrivacyReleasePending     bool
    AgentIngressWritable      bool
    OwnershipTransferActive   bool
}

type AuthoritySnapshot struct {
    ClaimAuthoritySnapshot
    Shell shellcore.ShellIdentity
}
```

Change `RuntimeCallbacks.BindClaim` to:

```go
BindClaim func(context.Context, string, core.HelperBinding, core.ContextBinding,
    core.ShellContinuityExpectation, core.ShellContinuityProof,
    time.Time, string) (operation.ContextExecState, error)
```

- [ ] **Step 4: Split admission validation**

Extract all non-shell checks from `validateAdmission` into:

```go
func validateClaimAdmission(req core.Request, snapshot ClaimAuthoritySnapshot) error
```

`validateAdmission(req, AuthoritySnapshot)` must call `validateClaimAdmission` first and then retain supported-shell qualification plus current provider process-facts checks. Therefore `Execute` and both pre-launch observations still reject unknown/nested/changed shells.

- [ ] **Step 5: Bind claim against durable state, pre-launch expectation, fresh provider facts, and proof**

Use this signature:

```go
func (s *Service) BindClaim(
    ctx context.Context,
    contextExecID string,
    helper core.HelperBinding,
    finalContext core.ContextBinding,
    continuity core.ShellContinuityExpectation,
    proof core.ShellContinuityProof,
    boundaryObservedAt time.Time,
    verifierDigest string,
) (operation.ContextExecState, error)
```

After loading the durable `helper_requested` state and validating `finalContext`, call `s.authority.ClaimSnapshot(ctx, req)`, run `validateClaimAdmission`, then validate continuity before `BindHelperGeneration`.

The continuity validator must require:

```go
if err := continuity.Validate(); err != nil {
    return admissionFailure(req, failure.ContextExecBoundaryUnproven, "shell_continuity_expectation_invalid", err)
}
if err := proof.ValidateFor(continuity); err != nil {
    return admissionFailure(req, failure.ContextExecBoundaryUnproven, "shell_continuity_unproven", err)
}
if continuity.SessionID != req.SessionID || continuity.AuthorityEpoch != req.AuthorityEpoch ||
    continuity.ProviderGeneration != state.Expectation.ProviderGeneration {
    return admissionFailure(req, failure.ContextExecStaleGeneration, "shell_continuity_generation_changed", nil)
}
if continuity.ShellRuntimeIdentity != state.Expectation.ShellIdentity ||
    filepath.Clean(continuity.HelperExecutableIdentity) != filepath.Clean(helper.ExecutablePath) {
    return admissionFailure(req, failure.ContextExecBoundaryUnproven, "shell_continuity_reservation_mismatch", nil)
}
if continuity.PaneShellPID != claim.Observation.PanePID ||
    filepath.Clean(continuity.PaneTTY) != filepath.Clean(claim.Observation.PaneTTY) {
    return admissionFailure(req, failure.ContextExecBoundaryUnproven, "pane_identity_changed", nil)
}
if !sameContextDirectoryIdentity(claim.Observation.CWD, state.Expectation.CWDObserved) {
    return admissionFailure(req, failure.ContextExecBoundaryUnproven, "cwd_changed", nil)
}
```

`PaneShellProcessIdentity` is now independently checked because `proof.ValidateFor(continuity)` requires exact equality to the server-owned pre-launch expectation. The binder never derives shell identity from `claim.Observation.CurrentCommand`.

- [ ] **Step 6: Run the app GREEN gate**

```bash
go test ./internal/app/contextexec -count=1
git diff --check
```

Expected: PASS, including CWD alias acceptance and real drift rejection.

---

### Task 4: Make Daemon Claim Snapshots Skip Volatile Shell-Family Probing

**Files:**
- Modify: `internal/app/daemon/context_exec.go`
- Modify: `internal/app/daemon/context_exec_test.go`

**Interfaces:**
- Consumes: `ContextAuthority.ClaimSnapshot`
- Keeps: `ComposeContextExec(...)` composition signature unchanged
- Produces: one base provider/privacy authority observation shared by `Snapshot` and `ClaimSnapshot`

- [ ] **Step 1: Write a RED daemon-composition test with a counting shell probe**

Make the shell probe fail if called after the two pre-launch observations. Drive `Execute` to `helper_requested`, then invoke the claim callback with provider `CurrentCommand="shellbeam"`; claim must bind without another probe call.

```go
type contextExecShellProbe struct {
    calls     int
    failAfter int
}

func (p *contextExecShellProbe) Probe(_ context.Context, req shellapp.ProbeRequest) (shellapp.ShellIdentityObservation, error) {
    p.calls++
    if p.failAfter > 0 && p.calls > p.failAfter {
        return shellapp.ShellIdentityObservation{}, errors.New("claim-time foreground command must not be shell-probed")
    }
    return exactZshObservation(req), nil
}
```

Assert the claim uses fresh provider/privacy/CWD truth while `probe.calls` remains the pre-launch count.

- [ ] **Step 2: Run the daemon RED gate**

```bash
go test ./internal/app/daemon -run ContextExec -count=1 -v
```

Expected: FAIL until `contextExecAuthority` implements claim-only observation.

- [ ] **Step 3: Factor base provider/privacy authority observation**

Refactor `contextExecAuthority`:

```go
func (a contextExecAuthority) ClaimSnapshot(ctx context.Context, req contextcore.Request) (contextapp.ClaimAuthoritySnapshot, error) {
    // Load delegated binding and provider ref.
    // Inspect provider and privacy.
    // Reconcile effective authority and AgentIngressWritable exactly as today.
    // Do not call a.shell.Probe.
}

func (a contextExecAuthority) Snapshot(ctx context.Context, req contextcore.Request) (contextapp.AuthoritySnapshot, error) {
    base, err := a.ClaimSnapshot(ctx, req)
    if err != nil { return contextapp.AuthoritySnapshot{}, err }
    facts := shellapp.ProviderProcessFacts{
        SessionID: req.SessionID,
        ProviderID: base.Observation.Provider.ID,
        ProviderVersion: base.Observation.Provider.Version,
        ProviderGeneration: base.Observation.ProviderGeneration,
        PanePID: base.Observation.PanePID,
        CurrentCommand: base.Observation.CurrentCommand,
        PaneTTY: base.Observation.PaneTTY,
        CWD: base.Observation.CWD,
    }
    shellObs, err := a.shell.Probe(ctx, shellapp.ProbeRequest{Facts: facts})
    if err != nil { return contextapp.AuthoritySnapshot{}, err }
    return contextapp.AuthoritySnapshot{ClaimAuthoritySnapshot: base, Shell: shellObs.Identity}, nil
}
```

There is no `shellbeam` string special-case.

- [ ] **Step 4: Run daemon/app GREEN gates**

```bash
go test ./internal/app/daemon ./internal/app/contextexec -count=1
git diff --check
```

Expected: PASS.

---

### Task 5: Build the Pre-Launch Continuity Expectation in the Native Runtime

**Files:**
- Modify: `cmd/shellbeam/context_exec_runtime.go`
- Modify: `cmd/shellbeam/context_exec_runtime_test.go`

**Interfaces:**
- Consumes: new `RuntimeCallbacks.BindClaim`, `ClaimExpectation.Continuity`, and proof-returning `PeerVerifier`
- Keeps: fixed helper argv, private runtime locator, claim challenge/response, exact parent PID/process identity/TTY proof

- [ ] **Step 1: Write RED runtime tests for pre-launch expectation construction**

Update callback fakes to accept continuity expectation + proof. Add an assertion that `serverFor` constructs continuity from values captured before helper launch:

```go
want := contextcore.ShellContinuityExpectation{
    SessionID:                arm.Shell.SessionID,
    AuthorityEpoch:           arm.Shell.Authority.Epoch,
    ProviderGeneration:       arm.Shell.Facts.ProviderGeneration,
    ShellRuntimeIdentity:     arm.Expectation.ShellIdentity,
    PaneShellPID:             arm.Shell.Facts.PanePID,
    PaneShellProcessIdentity: req.parentIdentity,
    PaneTTY:                  filepath.Clean(arm.Shell.Facts.PaneTTY),
    HelperExecutableIdentity: filepath.Clean(arm.Helper.ExecutablePath),
}
```

The fake verifier must return a proof that `ValidateFor(want)` accepts. A changed parent identity or TTY must reject before the claim callback.

- [ ] **Step 2: Run cmd RED tests**

```bash
go test ./cmd/shellbeam -run 'ContextExecRuntime|ContextExec|Handoff|Readiness' -count=1
```

Expected: FAIL until runtime/server callback signatures are wired.

- [ ] **Step 3: Construct `ClaimExpectation.Continuity` from pre-launch facts**

Keep `contextExecServeRequest.parentIdentity` captured by `r.observe(ctx, facts.PanePID)` before `armShell` delivers helper bytes. In `serverFor`, build:

```go
continuity := contextcore.ShellContinuityExpectation{
    SessionID:                arm.Shell.SessionID,
    AuthorityEpoch:           arm.Shell.Authority.Epoch,
    ProviderGeneration:       arm.Shell.Facts.ProviderGeneration,
    ShellRuntimeIdentity:     arm.Expectation.ShellIdentity,
    PaneShellPID:             arm.Shell.Facts.PanePID,
    PaneShellProcessIdentity: req.parentIdentity,
    PaneTTY:                  filepath.Clean(arm.Shell.Facts.PaneTTY),
    HelperExecutableIdentity: filepath.Clean(arm.Helper.ExecutablePath),
}
```

Put it into `contextadapter.ClaimExpectation{..., Continuity: continuity}` and keep the default verifier configured from the same parent PID/identity/TTY/helper executable. Do not put continuity data into helper argv/env/protocol frames.

- [ ] **Step 4: Run focused runtime GREEN gates**

```bash
go test ./cmd/shellbeam -run 'ContextExecRuntime|ContextExec|Handoff|Readiness' -count=1
git diff --check
```

Expected: PASS.

---

### Task 6: Remove Diagnostic-Only Instrumentation and Prove Native Post-Secret Execution

**Files:**
- Modify: `cmd/shellbeam/context_exec_acceptance_test.go`
- Modify: `cmd/shellbeam/context_exec_runtime.go` only to remove Task-8 auth diagnostic logging
- Modify: `internal/adapter/contextexec/server.go` only to remove Task-8 binder diagnostic logging
- Modify: `internal/app/interactivehandoff/readiness.go` only to remove Task-8 automatic-readiness diagnostic logging; do not change H4 semantics as part of this amendment

**Interfaces:**
- Produces: native proof that foreground helper no longer causes a shell-family false negative
- Keeps: public-safe lifecycle/socket timeout diagnostics only

- [ ] **Step 1: Remove all temporary root-cause markers**

Delete lines matching:

```text
SHELLBEAM_H5_AUTH_DIAG
SHELLBEAM_H5_BIND_DIAG
SHELLBEAM_H4_AUTOREADY_DIAG
```

Verify:

```bash
if grep -R -n 'SHELLBEAM_H5_AUTH_DIAG\|SHELLBEAM_H5_BIND_DIAG\|SHELLBEAM_H4_AUTOREADY_DIAG' cmd/shellbeam internal/app internal/adapter; then
  exit 1
fi
```

The acceptance timeout must not read or print parent PTY/private output.

- [ ] **Step 2: Run the amendment-focused non-native gate**

```bash
go test ./internal/core/contextexec \
  ./internal/adapter/contextexec \
  ./internal/app/contextexec \
  ./internal/app/daemon \
  ./cmd/shellbeam \
  -count=1
git diff --check
```

Expected: PASS.

- [ ] **Step 3: Run exact native post-secret workflow with real tmux**

```bash
SHELLBEAM_H0_TMUX=/opt/homebrew/Cellar/tmux/3.6a/bin/tmux \
  go test ./cmd/shellbeam \
  -run '^TestContextExecNativePostSecretWorkflowIsBoundedPrivateAndExactlyOnce$' \
  -count=1 -v
```

Expected: PASS without skip. The result reaches canonicalization, returns exactly `DOCTOR_OK\n`, replay causes no second doctor side effect, and changed argv under the same `context_exec_id` returns conflict.

If this run stops earlier in H4 automatic readiness, do not weaken context-exec, do not add sleeps, and do not count it as an amendment failure. Return to the existing Task-8 H4 evidence boundary and close that independent flake before retrying the native H5 proof.

- [ ] **Step 4: Prove continuity metadata is absent from public surfaces**

Extend test-only anti-leak assertions with recognizable fake continuity identifiers (for example `proc_shell_ctx_canary`) and require absence from response JSON, public state, daemon logs, evidence/repro/telemetry/state scans already owned by Task 8. Do not expose real process identities in failure text.

- [ ] **Step 5: Checkpoint without committing**

```bash
git status --short
git diff --check
```

Inspect the diff and preserve all parent Task-8 work for its final commit.

---

### Task 7: Re-enter Parent Task 8 and Finish the High-Assurance Matrix

**Files:**
- Parent plan: `docs/superpowers/plans/2026-08-18-interactive-handoff-h5-context-exec-high-assurance.md`
- Continue/create there: `tests/integration/context_exec_test.go`
- Continue: `cmd/shellbeam/context_exec_acceptance_test.go`
- Create there: `docs/superpowers/evidence/2026-08-18-context-exec-high-assurance.md`

**Interfaces:**
- Consumes: Tasks 1-6 GREEN
- Produces: parent Task-8 stable gate and single final implementation commit

- [ ] **Step 1: Resume at the first unfinished Task-8 matrix item**

Do not duplicate amendment coverage. Record amendment spec commit `9baac5387b6cf7438b93a85482180525b9357f4b`, amendment SHA `3891afdf5e7f28713751884f54ae2d84506f6c427f9adab639a5812f422eaa24`, base SHA `b3b10c5481fee65118c5fc062255e6f43b7f357d15a17e1069b1f15abf52a32c`, and the fresh native shell-continuity result in the Task-8 evidence document. Then continue exactly-once, generation/ownership, crash, anti-leak, actual-executable, and resource/hermetic matrices from the parent plan.

- [ ] **Step 2: Run the parent Task-8 exact final gates**

```bash
go mod verify
go test ./internal/core/contextexec ./internal/app/contextexec ./internal/adapter/contextexec ./internal/app/shellintegration ./internal/adapter/shellintegration ./internal/app/daemon ./api/schema ./internal/adapter/ipc ./internal/adapter/mcp ./cmd/shellbeam -count=1
go test -race ./internal/core/contextexec ./internal/app/contextexec ./internal/adapter/contextexec ./internal/app/daemon -count=1
go run ./tools/devctl check
go run ./tools/devctl test --dirty --base "$(git merge-base HEAD main)" --json
git diff --check
```

A skipped native test is not evidence. Set `CONTEXT_EXEC_STABLE_GATE=true` only after the real native tmux test and every exact parent gate are fresh PASS.

- [ ] **Step 3: Stage the complete Task-8 set and run commit gate**

The amendment spec is already committed and must not appear in this implementation diff.

```bash
git diff --check
git add -p
git diff --cached --check
git diff --cached --name-only
go run ./tools/devctl commit-gate --json
```

Inspect the staged diff to ensure no unrelated worktree changes are included.

- [ ] **Step 4: Make the parent Task-8 final commit and post-commit verification**

```bash
git -c core.hooksPath=.githooks commit -m "test: verify high assurance context execution"
go run ./tools/devctl check
git status --short
```

Expected: commit succeeds, fresh post-commit `devctl check` PASS, and the worktree is clean. Only then report Task 8 complete.
