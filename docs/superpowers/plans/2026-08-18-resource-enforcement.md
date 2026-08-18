# Resource Enforcement V1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an experimental Linux cgroup-v2 hard resource-enforcement capability for non-persistent `local_shell start` requests, supporting `memory_bytes` and `processes` without changing ordinary no-limit execution behavior.

**Architecture:** Resource limits become a versioned operation contract that participates in request/execution identity, flows through MCP/IPC into `operation.ExecutionSpec`, and is enforced by the existing process owner. Linux uses an explicitly delegated process-empty cgroup root whose reserved `manager` child contains the daemon; each operation-private `job-*` is a sibling and uses `clone3(CLONE_INTO_CGROUP)` through Go's `UseCgroupFD/CgroupFD`. The operator establishes the manager placement; ShellBeam never reparents itself. Darwin advertises no hard support. Resource breach classification is derived from bounded provider state while literal OS exit/signal evidence remains unchanged.

**Tech Stack:** Go 1.26.5, Linux cgroup v2, standard library `syscall`, existing ShellBeam MCP/IPC v2, GitHub Actions Ubuntu native gate.

## Global Constraints

- Existing E23 observation and resource enforcement remain distinct capabilities.
- No requested hard limit may silently degrade to sampled/best-effort enforcement.
- V1 hard fields: `memory_bytes`, `processes`; `cpu_time_ms` is schema-reserved but hard-unsupported.
- V1 persistent sessions are unsupported.
- Requests with unsupported hard semantics fail `resource_limit_unsupported` before child spawn whenever capability knowledge is available.
- No-limit ordinary execution performs zero cgroup-provider work.
- Resource limits participate in request and execution fingerprints; omitted limits preserve legacy fingerprints exactly.
- Never synthesize exit code 137 or replace literal exit/signal evidence.
- No new MCP tool and no new third-party dependency.
- Linux support requires native evidence; Darwin/cross-build cannot claim Linux support.
- `SHELLBEAM_RESOURCE_CGROUP_ROOT` is operator/daemon configuration only, never model-facing command state.
- The delegated root is process-empty; the operator must launch the daemon in reserved direct child `manager`. The provider never reparents or cleans `manager`.

---

### Task 1: Freeze resource-limit identity and validation contracts

**Files:**
- Create: `internal/core/operation/resource_limits.go`
- Create: `internal/core/operation/resource_limits_test.go`
- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/execution.go`
- Modify: `internal/core/operation/persistence.go`
- Modify: `internal/core/operation/policy_fingerprint_test.go`

**Interfaces:**
- Produces: `operation.ResourceLimits`, `operation.ResourceLimitKind`, `(*ResourceLimits).Validate()`, `(*ResourceLimits).Empty()`.
- Produces: `Intent.ResourceLimits`, `ExecutionSpec.ResourceLimits`, `Reservation.ResourceLimits`.
- Fingerprint rule: omitted/empty limits preserve the exact prior digest; non-empty limits wrap the prior digest in a versioned deterministic resource envelope before trace binding.

- [ ] **Step 1: Write failing validation/fingerprint tests**

Cover positive normalization, zero/negative rejection, CPU field acceptance as a syntactically valid but later unsupported request, deterministic field order, changed-limit conflict identity, and exact backward fingerprint compatibility for omitted limits.

- [ ] **Step 2: Run RED**

Run:
```bash
go test ./internal/core/operation -run 'Resource|Fingerprint' -count=1
```
Expected: FAIL because resource-limit types/identity binding do not exist.

- [ ] **Step 3: Implement the minimal core contract**

Use bounded integer fields:
```go
type ResourceLimits struct {
    MemoryBytes int64 `json:"memory_bytes,omitempty"`
    Processes   int   `json:"processes,omitempty"`
    CPUTimeMS   int64 `json:"cpu_time_ms,omitempty"`
}
```
Reject an explicitly supplied struct with all-zero fields at the application/schema boundary; core `Empty()` exists only so omitted internal state can retain old fingerprints. Bind non-empty normalized limits using a versioned SHA-256 JSON envelope containing the prior fingerprint and exact normalized fields.

- [ ] **Step 4: Run GREEN and compatibility tests**

Run:
```bash
go test ./internal/core/operation -count=1
```
Expected: PASS, including legacy digest fixtures unchanged when limits are absent.

- [ ] **Step 5: Commit**

```bash
git add internal/core/operation
git diff --cached --check
git commit -m "feat: define resource enforcement limits"
```

### Task 2: Advertise hard enforcement separately from observation

**Files:**
- Modify: `internal/core/capability/resource.go`
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/core/capability/catalog_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: MCP local-shell input schema file discovered from current tree
- Test: `api/schema/schema_test.go` and existing schema contract tests

**Interfaces:**
- Produces: `FeatureResourceEnforcement`.
- Produces: `ResourceEnforcementSupport` with version/maturity/provider/scope/placement and per-field `hard|unsupported` qualities.
- Produces: `Catalog.WithResourceEnforcement(...)` without changing `ResourceObservation`.

- [ ] **Step 1: Write failing catalog/schema tests**

Assert observation remains unchanged, baseline enforcement unavailable, a qualified Linux support object advertises memory/process hard + CPU/persistent unsupported, cloning is deep, and start schemas accept only bounded `limits` fields with `additionalProperties:false`.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/capability ./api/schema -count=1
```
Expected: FAIL on missing enforcement capability/schema.

- [ ] **Step 3: Implement capability and JSON schemas**

Add `$defs.resource_limits` shared by raw and typed start. Use integer minimum 1 for supplied fields and require at least one property. Add a separate catalog support definition; do not overload `resource_observation`.

- [ ] **Step 4: Run GREEN**

```bash
go test ./internal/core/capability ./api/schema -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/capability api/schema
git diff --cached --check
git commit -m "feat: advertise hard resource enforcement"
```

### Task 3: Carry limits through MCP, IPC, admission, replay, and typed commands

**Files:**
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/admission.go`
- Modify: `internal/app/daemon/bindings.go`
- Modify: `internal/app/daemon/project_command.go`
- Modify: `internal/adapter/ipc/server_v2_unix.go`
- Modify: `internal/adapter/ipc/protocol_v2*.go` as required by current request structs
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/request.go`
- Tests: focused IPC/MCP/daemon project-command/replay tests

**Interfaces:**
- Consumes: `operation.ResourceLimits` from Task 1.
- Produces: `daemon.StartRequest.ResourceLimits` and identical forwarding into `Intent`, `Reservation`, and `ExecutionSpec`.

- [ ] **Step 1: Write failing transport/admission tests**

Cover raw start, typed project command, MCP decode, IPC forward, operation retry with same limits, conflict on changed limits, persistent rejection, CPU hard rejection, and no-limit legacy behavior.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/adapter/mcp ./internal/adapter/ipc ./internal/app/daemon -run 'Resource|Limit|Replay|ProjectCommand' -count=1
```
Expected: FAIL because limits are not forwarded/validated.

- [ ] **Step 3: Implement minimal forwarding and capability-gated validation**

Validation order is before reservation/spawn. If limits are requested:
- protocol must be v2;
- persistent must be false;
- requested fields must validate;
- `FeatureResourceEnforcement` must be available;
- each requested field must be advertised `hard`.

Return `failure.ResourceLimitUnsupported` with bounded `metric`/`reason` details for unsupported semantics.

- [ ] **Step 4: Run GREEN**

```bash
go test ./internal/adapter/mcp ./internal/adapter/ipc ./internal/app/daemon -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/daemon internal/adapter/ipc internal/adapter/mcp
git diff --cached --check
git commit -m "feat: transport resource limits to execution"
```

### Task 4: Build a fail-closed Linux cgroup-v2 provider and Darwin unavailable provider

**Files:**
- Create: `internal/adapter/process/resource_enforcement.go`
- Create: `internal/adapter/process/resource_enforcement_linux.go`
- Create: `internal/adapter/process/resource_enforcement_darwin.go`
- Create: `internal/adapter/process/resource_enforcement_linux_test.go`
- Create: `internal/adapter/process/resource_enforcement_darwin_test.go`
- Modify: `internal/adapter/process/owner_unix.go`

**Interfaces:**
- Produces: `process.NewOwnerFromEnvironment()` returning an `Owner` plus qualified enforcement support/availability.
- Zero-value `Owner{}` retains ordinary no-limit behavior and cannot hard-enforce limits.
- Linux provider creates one `job-*` child below the dedicated root, configures `memory.max`, `memory.oom.group`, `pids.max`, opens the cgroup directory FD, and returns a domain object.

- [ ] **Step 1: Write failing provider tests**

Pure tests cover configured-root validation, missing controllers/files, unsafe/symlink root rejection, event counter parsing, deterministic breach classification, stale `job-*` cleanup selection, and Darwin unsupported behavior. No unit test may claim native enforcement from a temp directory.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/adapter/process -run 'Resource|Cgroup' -count=1
```
Expected: FAIL on missing provider.

- [ ] **Step 3: Implement qualification and domain lifecycle**

Requirements:
- configuration absent => unavailable without probing/mutation;
- configured path qualification requires the current daemon PID directly in reserved sibling `manager` and rejects child cgroups beneath `manager`;
- configured path qualification creates/removes only a bounded empty probe child and stale owned `job-*` siblings;
- require cgroup-v2 control files and `memory`/`pids` child-controller availability;
- require `cgroup.kill` for crash/recovery cleanup;
- startup reconciliation only touches `job-*` children under the dedicated configured root;
- no raw cgroup path enters public errors.

- [ ] **Step 4: Run GREEN/race**

```bash
go test ./internal/adapter/process -count=1
go test -race ./internal/adapter/process -run 'Resource|Cgroup' -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/process
git diff --cached --check
git commit -m "feat: prepare linux cgroup enforcement"
```

### Task 5: Spawn shell, argv, and PTY children directly into the hard resource domain

**Files:**
- Modify: `internal/adapter/process/owner_unix.go`
- Modify: `internal/adapter/process/pty_unix.go`
- Modify: `internal/adapter/process/argv.go` only if command construction requires a shared helper
- Tests: `internal/adapter/process/*resource*test.go`, existing owner/PTY/argv tests

**Interfaces:**
- Consumes: provider domain from Task 4.
- Before `cmd.Start`/`pty.Start`, Linux sets `SysProcAttr.UseCgroupFD=true` and `CgroupFD=<opened job dir fd>` while preserving existing process-group/session/TTY attributes.
- Produces optional handle status interface returning a frozen resource-breach result after `Wait`.

- [ ] **Step 1: Write failing spawn-hook tests**

Use a fake resource provider/domain to prove:
- no limits => provider is never called;
- limits => prepare happens before spawn;
- prepare failure runs no child;
- cgroup FD attributes coexist with non-TTY `Setpgid` and PTY session/controlling-terminal attributes;
- failed spawn cleans the prepared domain.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/adapter/process -run 'Resource|PTY|Owner' -count=1
```
Expected: FAIL before hook integration.

- [ ] **Step 3: Implement shared prepare/start/finalize lifecycle**

After successful spawn, close only the transient cgroup FD while retaining the job domain path/control handle. A bounded provider monitor may observe `pids.events:max` to implement the approved terminal-on-breach policy; the kernel `pids.max` remains the hard enforcement primitive. Memory breach is classified from operation-local event counter deltas. Finalize/cleanup freezes breach state before `Wait` becomes observable to daemon code.

- [ ] **Step 4: Run GREEN/race**

```bash
go test ./internal/adapter/process -count=1
go test -race ./internal/adapter/process -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/process
git diff --cached --check
git commit -m "feat: spawn processes inside resource domains"
```

### Task 6: Derive typed resource-limit terminal truth without inventing child evidence

**Files:**
- Modify: `internal/app/daemon/process_port.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/core/receipt/failure_taxonomy.go`
- Modify: `internal/core/receipt/failure_taxonomy_test.go`
- Tests: daemon lifecycle tests with a fake resource-aware handle

**Interfaces:**
- Optional handle interface: terminal resource breach kind after `Wait`.
- Persisted receipt uses bounded existing `FailureReason` values `resource_limit_memory` or `resource_limit_processes`; receipt schema need not be widened.
- Derived result failure is `stage=execution`, `class=resource`, `code=resource_limit`, detail `resource_limit_kind=memory|processes`.

- [ ] **Step 1: Write failing lifecycle tests**

Prove resource breach makes operation failure even if literal exit resembles a signal, literal `ExitEvidence` remains untouched, normal exit remains normal, and timeout/explicit kill precedence is deterministic and documented.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/app/daemon ./internal/core/receipt -run 'Resource|Failure' -count=1
```
Expected: FAIL before classification support.

- [ ] **Step 3: Implement minimal classification**

Read optional resource status only after process `Wait`. Do not replace `ExitEvidence` or synthesize signal/exit code. Existing explicit timeout/kill terminal target wins when already armed; otherwise a proven provider breach sets the resource-limit failure reason.

- [ ] **Step 4: Run GREEN**

```bash
go test ./internal/app/daemon ./internal/core/receipt -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/daemon internal/core/receipt
git diff --cached --check
git commit -m "feat: classify hard resource limit breaches"
```

### Task 7: Wire provider qualification into daemon capability composition

**Files:**
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `cmd/shellbeam/execution_telemetry_test.go`
- Create/modify: daemon resource-enforcement composition tests
- Modify: docs for experimental operator configuration

**Interfaces:**
- `command daemon` creates one qualified process owner at startup and uses the same provider result to compose capability catalog and execute children.
- A missing/unqualified root leaves enforcement unavailable; no request can see a catalog claim that the owner cannot attempt.

- [ ] **Step 1: Write failing composition tests**

Test unavailable default, qualified fake/provider support, telemetry observation unchanged, and no-limit daemon startup behavior.

- [ ] **Step 2: Run RED**

```bash
go test ./cmd/shellbeam -run 'Resource|Telemetry' -count=1
```
Expected: FAIL before composition wiring.

- [ ] **Step 3: Implement wiring and operator documentation**

Document dedicated-root ownership, Linux-only experimental status, and explicit no-support behavior on Darwin.

- [ ] **Step 4: Run GREEN**

```bash
go test ./cmd/shellbeam -run 'Resource|Telemetry' -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/shellbeam docs internal/core/capability
git diff --cached --check
git commit -m "feat: qualify resource enforcement at daemon startup"
```

### Task 8: Add native Linux enforcement and cleanup evidence

**Files:**
- Create: `scripts/test-resource-enforcement-native.sh`
- Create: `tests/integration/resource_enforcement_linux_test.go`
- Create: `docs/testing/resource-enforcement.md`
- Modify: `.github/workflows/checkpoint.yml`
- Modify: `.github/workflows/nightly.yml` if native stress belongs there

**Interfaces:**
- CI provisioning creates a process-empty delegated root owned by the test user, enables `memory`/`pids`, creates reserved `manager`, operator-bootstraps the test shell into `manager`, exports `SHELLBEAM_RESOURCE_CGROUP_ROOT`, and always moves the shell back before cleanup.

- [ ] **Step 1: Write native tests before production claims**

Cover direct birth into cgroup, descendant memory OOM, descendant fork storm, exact typed resource failure + literal child evidence, no-limit zero provider path, stale job recovery, 100 repeated bounded jobs with zero cgroup creep, shell/argv/PTY paths, and unsupported CPU/persistent requests.

- [ ] **Step 2: Run local cross-build only as compile evidence**

```bash
GOOS=linux GOARCH=amd64 go test ./internal/adapter/process ./internal/app/daemon ./tests/integration -run 'Resource' -count=1
```
Expected: compile/test success where tests are not native-only. This is **not** a Linux support verdict.

- [ ] **Step 3: Wire Ubuntu native script**

The script fails closed if cgroup v2/controller provisioning cannot be established; it never converts that into PASS. Upload bounded diagnostic/evidence on failure.

- [ ] **Step 4: Run available local gates and CI-native gate when published**

Local Darwin should prove unsupported semantics and no regressions. Linux support may be called ready only after the Ubuntu native lane is green.

- [ ] **Step 5: Commit**

```bash
git add scripts tests docs/testing .github/workflows
git diff --cached --check
git commit -m "test: verify native resource enforcement"
```

### Task 9: Final full-tree verification

**Files:**
- Modify only evidence/notes required by current project conventions.

- [ ] **Step 1: Run formatting and focused race**

```bash
gofmt -w <all changed .go files>
git diff --check
go test -race ./internal/adapter/process ./internal/app/daemon ./internal/core/operation ./internal/core/capability ./internal/core/receipt -count=1
```

- [ ] **Step 2: Run full repository suite**

```bash
go mod verify
go test -count=1 ./...
./scripts/test-hardening.sh
./scripts/test-security.sh
```

- [ ] **Step 3: Run checkpoint verification/build**

```bash
go run ./tools/devctl verify --checkpoint --base origin/main --json
go build -trimpath -buildvcs=false -o .build/checkpoints/resource-enforcement/shellbeam ./cmd/shellbeam
```

- [ ] **Step 4: Anti-goal scan**

Prove production changes add no container orchestrator, command scheduler, automatic tuning, sampled-hard overclaim, persistent-session resource domain, second MCP tool, or no-limit cgroup tax.

- [ ] **Step 5: Commit final acceptance if any files changed**

```bash
git diff --check origin/main...HEAD
git status --short
```

Do not claim Linux hard support until the native Ubuntu gate is fresh and green.
