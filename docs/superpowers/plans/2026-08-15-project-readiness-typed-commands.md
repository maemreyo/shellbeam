# Project Readiness and Typed Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement A5 / E25 Project Readiness and E28 Typed Parameterized Project Commands while preserving ShellBeam's one-tool, exactly-once, non-workflow execution model.

**Architecture:** Project Manifest v2 extends the existing strict manifest with deterministic requirements and whole-token typed parameters while v1 remains unchanged. Readiness is an explicit `inspect.readiness` service with bounded cached host probes and never runs on ordinary start. Typed command start computes a caller-stable request fingerprint, durably claims the operation ID before manifest/provider reads, freezes the exact manifest/provider/params/resolved argv into a schema-v3 operation/receipt, then uses the existing direct-argv runtime.

**Tech Stack:** Go 1.26.5, `github.com/pelletier/go-toml/v2`, existing ShellBeam IPC/MCP/store/process/workspace/project layers, Go standard library.

## Global Constraints

- Readiness observes only. It never installs, repairs, bootstraps, starts services, probes network/database/cloud services, or mutates a project.
- Typed commands execute one direct-argv invocation only: no shell interpolation, dependency auto-execution, workflow language, loops, matrices, hidden env mutation, or automatic command selection.
- Manifest v2 readers accept valid v1 fixed commands unchanged. Same-version unknown fields fail closed. Newer unsupported versions are `unsupported` and are never partially interpreted.
- `local_shell` remains the only public MCP tool.
- Ordinary non-project `start` pays zero readiness/typed-binding work.
- Typed retries lookup persisted state before current workspace/manifest/provider reads.
- Environment readiness stores only presence status, never values or hashes of values.
- Parameterized shell commands are invalid. Substitution is whole-token only.
- Parameter kinds are exactly `string`, `enum`, `integer`, `repo_path`, `repo_package`.
- Positional string/path/package values reject leading `-` by default.
- `repo_path` is binding-time address integrity, never runtime confinement.
- `repo_package` never falls back to raw string when its provider is unavailable.
- Cached typed binding targets p95 <= 10 ms incremental local work with zero network and zero subprocesses.
- Do not push, PR, merge, create/switch worktrees or branches, or modify `.codegraph`.

---

### Task 1: Project Manifest v2 contracts

**Files:**
- Modify: `internal/core/project/manifest.go`
- Modify: `internal/core/project/validation.go`
- Modify: `internal/core/project/manifest_test.go`
- Modify: `internal/adapter/project/loader_test.go`
- Modify: `internal/core/capability/catalog.go`

**Interfaces:**
- Produce `ManifestSchemaV1=1`, `ManifestSchemaV2=2`, `CurrentManifestSchemaVersion=2`.
- Add `Requirements`, `ToolchainRequirement`, `ExecutableRequirement`, `EnvironmentRequirements`.
- Add `ParameterKind`, `ParameterDefinition`, `PathExistence`; extend `Command` with `Params`.

- [ ] **Step 1: Write failing v1/v2 schema tests**

Use this canonical v2 fixture:

```toml
schema_version = 2

[toolchains.go]
version_source = "go.mod"

[requirements.toolchains.go]
required = true

[requirements.executables.git]
required = true

[requirements.executables.docker]
required = false

[requirements.environment]
required_presence = ["DATABASE_URL"]
optional_presence = ["AWS_PROFILE"]

[commands.test_package]
argv = ["go", "test", "{package}", "-run", "{test_name}"]
cwd = "."

[commands.test_package.params.package]
kind = "repo_package"
provider = "go"
required = true

[commands.test_package.params.test_name]
kind = "string"
required = false
default = "."
```

Tests must prove: v1 fixed manifests parse unchanged; v1 with v2-only fields is schema-invalid; v2 parses; v2 unknown fields are invalid; v3 is unsupported; parameterized shell form is invalid; partial token interpolation such as `"./{package}/..."` is invalid; duplicate/undefined placeholders are invalid.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/project ./internal/adapter/project -run 'Manifest|Loader' -count=1
```

Expected: schema-v2 tests fail because v2 fields/types do not exist.

- [ ] **Step 3: Implement closed versioned raw schemas**

Use separate `rawManifestV1` / `rawManifestV2` decode targets selected after the lightweight `schema_version` header read. Define:

```go
type Requirements struct {
    Toolchains  map[string]ToolchainRequirement  `json:"toolchains,omitempty"`
    Executables map[string]ExecutableRequirement `json:"executables,omitempty"`
    Environment EnvironmentRequirements          `json:"environment,omitempty"`
}
type ToolchainRequirement struct { Required bool `json:"required"` }
type ExecutableRequirement struct { Required bool `json:"required"` }
type EnvironmentRequirements struct {
    RequiredPresence []string `json:"required_presence,omitempty"`
    OptionalPresence []string `json:"optional_presence,omitempty"`
}

type ParameterKind string
const (
    ParameterString      ParameterKind = "string"
    ParameterEnum        ParameterKind = "enum"
    ParameterInteger     ParameterKind = "integer"
    ParameterRepoPath    ParameterKind = "repo_path"
    ParameterRepoPackage ParameterKind = "repo_package"
)
type PathExistence string
const (
    PathExistsAny       PathExistence = "any"
    PathExistsFile      PathExistence = "file"
    PathExistsDirectory PathExistence = "directory"
)
type ParameterDefinition struct {
    Kind             ParameterKind `json:"kind"`
    Required         bool          `json:"required"`
    Default          string        `json:"default,omitempty"`
    Enum             []string      `json:"enum,omitempty"`
    Min              *int64        `json:"min,omitempty"`
    Max              *int64        `json:"max,omitempty"`
    Exists           PathExistence `json:"exists,omitempty"`
    Provider         string        `json:"provider,omitempty"`
    AllowLeadingDash bool          `json:"allow_leading_dash,omitempty"`
}
```

Normalize omitted `required` to true when no default exists. `repo_path.exists` defaults to `any`. `enum` requires 1..64 unique bounded members. Fields irrelevant to a kind are schema-invalid.

- [ ] **Step 4: Advertise manifest versions and run GREEN**

```bash
gofmt -w internal/core/project internal/adapter/project internal/core/capability
go test ./internal/core/project ./internal/adapter/project ./internal/core/capability -count=1
git add internal/core/project internal/adapter/project internal/core/capability
git diff --cached --check
git commit -m "feat: add project manifest v2 contracts"
```

---

### Task 2: Deterministic readiness core model

**Files:**
- Create: `internal/core/project/readiness.go`
- Create: `internal/core/project/readiness_test.go`
- Modify: `internal/core/failure/failure.go`
- Modify: `internal/core/failure/failure_test.go`

**Interfaces:**

```go
type ReadinessState string
const (
    ReadinessReady       ReadinessState = "ready"
    ReadinessNotReady    ReadinessState = "not_ready"
    ReadinessPartial     ReadinessState = "partial"
    ReadinessUnavailable ReadinessState = "unavailable"
)
type RequirementKind string
const (
    RequirementToolchain           RequirementKind = "toolchain"
    RequirementExecutable          RequirementKind = "executable"
    RequirementEnvironmentPresence RequirementKind = "environment_presence"
)
type CheckStatus string
const (
    CheckAvailable       CheckStatus = "available"
    CheckMissing         CheckStatus = "missing"
    CheckCompatible      CheckStatus = "compatible"
    CheckIncompatible    CheckStatus = "incompatible"
    CheckPresent         CheckStatus = "present"
    CheckPresentNonEmpty CheckStatus = "present_nonempty"
    CheckAbsent          CheckStatus = "absent"
    CheckUnknown         CheckStatus = "unknown"
    CheckUnavailable     CheckStatus = "unavailable"
)
type ReadinessCheck struct {
    ID string `json:"id"`
    Kind RequirementKind `json:"kind"`
    Required bool `json:"required"`
    Status CheckStatus `json:"status"`
    Code string `json:"code,omitempty"`
    ProviderID string `json:"provider_id,omitempty"`
    ProviderVersion int `json:"provider_version,omitempty"`
}
```

`Readiness` binds repository/workspace, manifest digest/schema, environment/toolchain observation fingerprints, capture time, cache quality/age and checks.

- [ ] **Step 1: Write failing fold/validation tests**

Required all-satisfied => `ready`; one known required failure => `not_ready`; no known failure plus required unknown/unavailable => `partial`; no usable check set => `unavailable`; optional failure never changes required fold.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/project -run Readiness -count=1
```

- [ ] **Step 3: Implement validation/fold and privacy-safe fingerprints**

Fingerprint normalized requirement IDs/status/provider identities only. Never include environment values.

Reserve stable codes: `project_readiness_unavailable`, `project_requirement_invalid`, `toolchain_missing`, `toolchain_version_unknown`, `toolchain_incompatible`.

- [ ] **Step 4: Run GREEN and commit**

```bash
gofmt -w internal/core/project internal/core/failure
go test ./internal/core/project ./internal/core/failure -count=1
git add internal/core/project internal/core/failure
git diff --cached --check
git commit -m "feat: define project readiness model"
```

---

### Task 3: Bounded readiness service and host observers

**Files:**
- Create: `internal/app/project/readiness.go`
- Create: `internal/app/project/readiness_test.go`
- Modify: `internal/app/project/ports.go`
- Create: `internal/adapter/project/readiness_host.go`
- Create: `internal/adapter/project/readiness_host_test.go`

**Interfaces:**

```go
type ExecutableObserver interface {
    ObserveExecutable(context.Context, string) core.ReadinessCheck
}
type EnvironmentObserver interface {
    ObserveEnvironmentPresence(context.Context, string, bool) core.ReadinessCheck
}
type ToolchainObserver interface {
    ObserveToolchain(context.Context, string, string, core.Toolchain) core.ReadinessCheck
}
func (s *Service) Readiness(ctx context.Context, workspaceID string) (core.Readiness, error)
```

- [ ] **Step 1: Write RED service/cache/privacy tests**

Prove valid v2 requirements are evaluated; v1/no requirements returns `unavailable`; a second unexpired call uses cache; expiry reprobes; optional failures do not make `not_ready`; serialized result cannot contain fake raw secret values.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/app/project ./internal/adapter/project -run Readiness -count=1
```

- [ ] **Step 3: Implement bounded cache**

Defaults: TTL 30s, max 256 entries. Base applicability binds repository/workspace + manifest digest/schema. Stored entries also bind normalized environment/toolchain observation fingerprints. Return cache quality `fresh` or `cached` and explicit age.

- [ ] **Step 4: Implement observers**

`exec.LookPath` adapter reports only executable `available/missing/unavailable`. `os.LookupEnv` adapter reports only presence status, never value/hash. Initial toolchain provider supports `go`: derive expected version from direct `version` or `go.mod` `go`/`toolchain` directive, perform only an explicit-readiness bounded host probe `go env GOVERSION`, parse exact `goX.Y[.Z]`, then compare. Unknown provider => `unavailable`, never incompatible.

- [ ] **Step 5: Run race/privacy gates and commit**

```bash
gofmt -w internal/app/project internal/adapter/project
go test ./internal/app/project ./internal/adapter/project -count=1
go test -race ./internal/app/project ./internal/adapter/project -count=1
git add internal/app/project internal/adapter/project
git diff --cached --check
git commit -m "feat: inspect bounded project readiness"
```

---

### Task 4: One-tool readiness action and capability discovery

**Files:**
- Modify: `internal/core/capability/catalog.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Create: `internal/adapter/ipc/project_readiness_test.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/call.go`
- Create: `internal/adapter/mcp/project_readiness_test.go`
- Modify strict schemas under `api/schema/`

**Interfaces:**
- Add action `inspect.readiness` with only `workspace_id`.
- Add `ResponseV2.Readiness *project.Readiness`.
- Advertise readiness version 1, requirement kinds, cache TTL/max entries.

- [ ] **Step 1: Write RED closed-field IPC/MCP/schema tests**

Prove routing works, extra fields fail closed, legacy projection omits A5-only fields, and MCP server still registers exactly one tool.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema -run 'Readiness|Project' -count=1
```

- [ ] **Step 3: Compose readiness service and update schemas**

Construct project readiness observers during daemon composition but do not call readiness from startup. Add action routing only.

- [ ] **Step 4: Run GREEN and commit**

```bash
gofmt -w cmd/shellbeam internal/core/capability internal/adapter/ipc internal/adapter/mcp
go test ./cmd/shellbeam ./internal/core/capability ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema -count=1
git add cmd/shellbeam internal/core/capability internal/adapter/ipc internal/adapter/mcp api/schema
git diff --cached --check
git commit -m "feat: expose project readiness inspection"
```

---

### Task 5: Typed parameter binder and repo validators

**Files:**
- Create: `internal/core/project/binding.go`
- Create: `internal/core/project/binding_test.go`
- Create: `internal/app/project/binder.go`
- Create: `internal/app/project/binder_test.go`
- Modify: `internal/app/project/ports.go`
- Create: `internal/adapter/project/path_validator.go`
- Create: `internal/adapter/project/path_validator_test.go`
- Create: `internal/adapter/project/go_package.go`
- Create: `internal/adapter/project/go_package_test.go`

**Interfaces:**

```go
type BindRequest struct {
    WorkspaceID string
    CommandID string
    Params map[string]string
    TimeoutMS int64
    TTY bool
}
type ParameterBinding struct {
    ID string `json:"id"`
    Kind ParameterKind `json:"kind"`
    Value string `json:"value"`
    Source string `json:"source"`
    ProviderID string `json:"provider_id,omitempty"`
    ProviderVersion int `json:"provider_version,omitempty"`
}
type CommandBinding struct {
    SchemaVersion int `json:"schema_version"`
    ManifestDigest string `json:"manifest_digest"`
    ManifestSchemaVersion int `json:"manifest_schema_version"`
    CommandID string `json:"command_id"`
    ParameterFingerprint string `json:"parameter_fingerprint"`
    Parameters []ParameterBinding `json:"parameters"`
    ResolvedArgv []string `json:"resolved_argv"`
    LogicalCWD string `json:"logical_cwd"`
    ResolvedCWD string `json:"resolved_cwd"`
    SourceGeneration string `json:"source_generation,omitempty"`
    PathObservationQuality string `json:"path_observation_quality,omitempty"`
}
```

- [ ] **Step 1: Write RED binder tests**

Cover all five kinds, defaults, integer canonicalization, option-like enum, positional leading-dash rejection, unknown/missing params, deterministic map order, parameterized shell rejection, provider unavailable, manifest change during bind, and `depends_on` remaining presentation-only.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/project ./internal/app/project ./internal/adapter/project -run 'Bind|Parameter|Package|Path' -count=1
```

- [ ] **Step 3: Implement scalar whole-token substitution**

Only tokens exactly `{id}` substitute. `string` is bounded UTF-8; `enum` exact-matches; `integer` parses signed base-10, enforces min/max and serializes with `strconv.FormatInt`. Sort bindings by ID before fingerprinting.

- [ ] **Step 4: Implement `repo_path` bind-time integrity**

Normalize repository-relative path, apply declared existence rule, evaluate symlinks, reject resolved target outside workspace root and record `exact_at_bind`. A test replaces the path after binding and proves the binding is not a confinement guarantee.

- [ ] **Step 5: Implement `go` repo_package provider**

Accept `.` and `./relative[/...]`; reject absolute/`..`/control/NUL/leading-dash; require non-ellipsis base directory inside workspace. Provider identity is `go-repo-package` version 1. Never run `go list`, package managers or network access.

- [ ] **Step 6: Add fuzz tests and commit**

Fuzz scalar decoding, integer canonicalization, path normalization, Unicode/size bounds and option-shaped input.

```bash
gofmt -w internal/core/project internal/app/project internal/adapter/project
go test ./internal/core/project ./internal/app/project ./internal/adapter/project -count=1
go test -race ./internal/app/project ./internal/adapter/project -count=1
git add internal/core/project internal/app/project internal/adapter/project
git diff --cached --check
git commit -m "feat: bind typed project commands"
```

---

### Task 6: Durable typed intent claims and schema-v3 frozen provenance

**Files:**
- Create: `internal/core/operation/project_command.go`
- Create: `internal/core/operation/project_command_test.go`
- Modify: `internal/core/operation/persistence.go`
- Modify: `internal/core/receipt/receipt.go`
- Modify: receipt tests
- Create: `internal/adapter/store/typed_intent.go`
- Create: `internal/adapter/store/typed_intent_test.go`
- Modify: `internal/adapter/store/reservation.go`

**Interfaces:**

```go
type TypedRequestIntent struct {
    WorkspaceID string `json:"workspace_id"`
    ProjectCommandID string `json:"project_command_id"`
    Params map[string]string `json:"params,omitempty"`
    TTY bool `json:"tty"`
    TimeoutMS int64 `json:"timeout_ms"`
}
func (i TypedRequestIntent) Fingerprint() (string, error)

type TypedIntentClaim struct {
    SchemaVersion int `json:"schema_version"`
    OperationID ID `json:"operation_id"`
    RequestFingerprint string `json:"request_fingerprint"`
    Intent TypedRequestIntent `json:"intent"`
    CreatedAt time.Time `json:"created_at"`
}
```

Add `ProjectCommand *project.CommandBinding` to schema-v3 `operation.Reservation` and `receipt.Receipt`. Ordinary starts remain schema 1/2.

Store API:

```go
ReserveTypedIntent(context.Context, operation.TypedIntentClaim) (operation.TypedIntentClaim, bool, daemon.StoreResult)
FindTypedIntent(context.Context, operation.ID) (operation.TypedIntentClaim, bool, error)
CommitTypedBinding(context.Context, operation.ID, operation.Reservation) (operation.Reservation, bool, daemon.StoreResult)
```

- [ ] **Step 1: Write RED exactly-once/fault tests**

Concurrent same claim => one winner; conflicting claim => `operation_conflict`; reopen preserves claim; no session metadata before binding commit; same binding commit is idempotent; conflicting binding rejects.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/operation ./internal/core/receipt ./internal/adapter/store -run 'Typed|ProjectCommand|Receipt' -count=1
```

- [ ] **Step 3: Implement caller-stable fingerprint**

Canonicalize params as sorted pairs. Fingerprint includes only workspace ID, project command ID, caller params, TTY and timeout/default semantics. It excludes manifest/provider/source/resolved argv/cwd.

- [ ] **Step 4: Implement durable claim/commit**

Persist private bounded claims keyed by validated operation ID. `CommitTypedBinding` requires matching fingerprint, persists full schema-v3 reservation/session metadata before spawn, and never overwrites a different frozen binding.

- [ ] **Step 5: Run race/fault gates and commit**

```bash
gofmt -w internal/core/operation internal/core/receipt internal/adapter/store
go test ./internal/core/operation ./internal/core/receipt ./internal/adapter/store -count=1
go test -race ./internal/adapter/store -count=1
git add internal/core/operation internal/core/receipt internal/adapter/store
git diff --cached --check
git commit -m "feat: persist typed command admission claims"
```

---

### Task 7: Retry-first typed command daemon admission

**Files:**
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/store_port.go`
- Modify: `internal/app/daemon/service.go`
- Modify: `internal/app/daemon/admission.go`
- Modify: `internal/app/daemon/bindings.go`
- Create: `internal/app/daemon/project_command.go`
- Create: `internal/app/daemon/project_command_test.go`
- Modify: `cmd/shellbeam/command_daemon.go`

**Interfaces:**
- Add `StartRequest.ProjectCommandID string` and `StartRequest.Params map[string]string`.
- Add `ProjectCommandBinder` port returning `project.CommandBinding`.

- [ ] **Step 1: Write RED ordering/no-spawn tests**

Recording fakes must prove: persisted operation lookup happens before binder/workspace read; typed claim persists before binder; binding failure spawns zero children; admitted retry succeeds even if binder/resolver now fail; conflicting fingerprint fails before reads; manifest/provider/source changes after admission do not alter argv; `depends_on` cannot cause a second start.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/app/daemon -run 'ProjectCommand|Typed|Retry' -count=1
```

- [ ] **Step 3: Implement typed branch before raw intent resolution**

Typed request requires v2 protocol + workspace ID and forbids simultaneous raw `command`, `argv`, caller `cwd`. Compute typed fingerprint; check frozen operation; check/reserve typed claim; only then bind. Build direct-argv `ExecutionSpec` from frozen binding and commit schema-v3 reservation before `owner.Start`.

- [ ] **Step 4: Prove ordinary start zero-tax**

Existing v1/v2 raw start path must not call project binder or typed-intent APIs.

- [ ] **Step 5: Run race gates and commit**

```bash
gofmt -w internal/app/daemon cmd/shellbeam
go test ./internal/app/daemon ./cmd/shellbeam -count=1
go test -race ./internal/app/daemon -count=1
git add internal/app/daemon cmd/shellbeam
git diff --cached --check
git commit -m "feat: admit typed project command starts"
```

---

### Task 8: Typed start IPC/MCP/schema/capability surface

**Files:**
- Modify: `internal/core/failure/failure.go`
- Modify: `internal/core/failure/failure_test.go`
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Create: `internal/adapter/ipc/project_command_test.go`
- Modify: `internal/adapter/mcp/input.go`
- Modify: `internal/adapter/mcp/call.go`
- Create: `internal/adapter/mcp/project_command_test.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Create: `api/schema/operation-v3.json`
- Create: `api/schema/receipt-v3.json`
- Create: `api/schema/project_command_v3_test.go`

**Interfaces:**
- Modern `start` accepts `project_command_id` and `params`.
- Capability advertises typed-command version 1, manifest version 2, parameter kinds, package provider `go`.
- Stable failures: `project_command_not_found`, `project_command_not_parameterized`, `parameter_unknown`, `parameter_missing`, `parameter_invalid`, `parameter_kind_unsupported`, `parameter_validation_unavailable`, `project_command_binding_conflict`.

- [ ] **Step 1: Write RED field-generation/schema tests**

Modern IPC/MCP accepts typed fields; legacy generation rejects/omits them; raw command/argv cannot coexist; params without command ID reject; schema-v3 receipt validates; legacy catalog hides A5-only fields.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema ./internal/core/capability -run 'ProjectCommand|Typed|Capability|Receipt' -count=1
```

- [ ] **Step 3: Implement closed routing/schema/capability**

Map only typed fields into daemon request. Keep `additionalProperties:false`. Do not add a second MCP tool.

- [ ] **Step 4: Run GREEN and commit**

```bash
gofmt -w internal/core/failure internal/core/capability internal/adapter/ipc internal/adapter/mcp
go test ./internal/core/failure ./internal/core/capability ./internal/adapter/ipc ./internal/adapter/mcp ./api/schema -count=1
git add internal/core/failure internal/core/capability internal/adapter/ipc internal/adapter/mcp api/schema
git diff --cached --check
git commit -m "feat: expose typed project command starts"
```

---

### Task 9: Bind telemetry/repro to frozen project-command provenance

**Files:**
- Modify: `internal/core/telemetry/record.go`
- Modify: `internal/core/telemetry/record_test.go`
- Modify: `internal/app/telemetry/service.go`
- Modify: `internal/app/telemetry/service_test.go`
- Modify: `internal/core/repro/capsule.go`
- Modify: `internal/core/repro/capsule_test.go`
- Modify: `internal/app/repro/service.go`
- Modify: `internal/app/repro/service_test.go`
- Modify: `api/schema/mcp-output-v2.json`
- Modify: `api/schema/project_command_v3_test.go`

**Interfaces:**
- Telemetry carries optional `ProjectCommandBindingDigest`; include it in compatibility identity.
- Repro carries an optional bounded frozen project-command descriptor or exact binding digest/reference derived from the receipt, never from current manifest state.

- [ ] **Step 1: Write RED propagation tests**

Typed terminal receipt => telemetry/repro preserve original manifest digest, command ID, parameter fingerprint/provider identity and resolved argv after current manifest changes. Ordinary operations remain valid with fields absent.

- [ ] **Step 2: Run RED**

```bash
go test ./internal/core/telemetry ./internal/app/telemetry ./internal/core/repro ./internal/app/repro -run 'ProjectCommand|Binding' -count=1
```

- [ ] **Step 3: Implement canonical binding digest/reference**

Compute from canonical `project.CommandBinding`. Telemetry compatibility must split changed binding semantics. Repro may carry bounded descriptor because reproduction needs frozen invocation facts.

- [ ] **Step 4: Run race/schema gates and commit**

```bash
gofmt -w internal/core/telemetry internal/app/telemetry internal/core/repro internal/app/repro
go test ./internal/core/telemetry ./internal/app/telemetry ./internal/core/repro ./internal/app/repro ./api/schema -count=1
go test -race ./internal/app/telemetry ./internal/app/repro -count=1
git add internal/core/telemetry internal/app/telemetry internal/core/repro internal/app/repro api/schema
git diff --cached --check
git commit -m "feat: bind derived facts to project commands"
```

---

### Task 10: A5 integration, performance, privacy and anti-goal acceptance

**Files:**
- Create: `tests/integration/project_readiness_typed_commands_test.go`
- Create/modify: `cmd/shellbeam/project_readiness_typed_commands_test.go`
- Production files only if acceptance exposes a real defect

- [ ] **Step 1: Add end-to-end readiness acceptance**

Real IPC daemon + v2 manifest: required/optional executable/env/toolchain distinctions; cache quality/age; secret fixture absent from output/state. A raw arbitrary start still executes when readiness is `not_ready`. No manifest command is auto-run.

- [ ] **Step 2: Add typed start/lost-response retry acceptance**

Run a parameterized command and assert exact argv + schema-v3 frozen binding. Modify/delete manifest and make provider unavailable; same operation ID must replay without rebind/second spawn. Conflicting params fail before provider read.

- [ ] **Step 3: Add path/package/depends_on acceptance**

Leading-dash positional rejects; option-like enum succeeds; symlink escape rejects; post-bind path replacement does not mutate binding and is not described as confinement; dependencies remain presentation-only.

- [ ] **Step 4: Add p95/no-tax acceptance**

At least 200 cached binding measurements; assert p95 <= 10 ms on native host and zero subprocess/network calls in binding validators. Instrument ordinary raw start and assert zero A5 observer/binder calls.

- [ ] **Step 5: Run focused integration x3**

```bash
go test ./tests/integration ./cmd/shellbeam -run 'ProjectReadiness|TypedProject|ProjectCommand' -count=3 -v
```

- [ ] **Step 6: Run package/race gates**

```bash
go mod verify
go test \
  ./internal/core/project \
  ./internal/app/project \
  ./internal/adapter/project \
  ./internal/core/operation \
  ./internal/core/receipt \
  ./internal/app/daemon \
  ./internal/adapter/store \
  ./internal/adapter/ipc \
  ./internal/adapter/mcp \
  ./internal/core/capability \
  ./internal/core/telemetry \
  ./internal/app/telemetry \
  ./internal/core/repro \
  ./internal/app/repro \
  ./cmd/shellbeam \
  ./api/schema \
  ./tests/integration \
  -count=1
go test -race \
  ./internal/app/project \
  ./internal/adapter/project \
  ./internal/app/daemon \
  ./internal/adapter/store \
  ./internal/adapter/ipc \
  ./internal/adapter/mcp \
  -count=1
```

- [ ] **Step 7: Run repository gates**

```bash
PATH="/Users/trung.ngo/go/bin:$PATH" go run ./tools/devctl test --dirty --base origin/main
PATH="/Users/trung.ngo/go/bin:$PATH" go run ./tools/devctl check
git diff --check origin/main...HEAD
git status --short -- .codegraph
```

- [ ] **Step 8: Anti-goal/privacy scan**

Prove production A5 contains no install/fix/bootstrap/service-start behavior, network/cloud/database probes, shell interpolation, dependency auto-execution, workflow/loop/matrix semantics, env value/hash persistence, package-manager subprocesses on start, automatic readiness gating, second MCP tool, or runtime-confinement claim.

- [ ] **Step 9: Commit acceptance**

```bash
git add tests/integration cmd/shellbeam internal api/schema
git diff --cached --check
git commit -m "test: verify project readiness and typed commands"
```

- [ ] **Step 10: Re-run exact final-tree verification**

Repeat Steps 5-8 after the final commit and record exact HEAD, devctl receipts, race results, clean worktree and `.codegraph` status.

---

## Completion Gate

A5 / E25+E28 is complete only when the exact final tree proves:

1. v1 fixed manifests remain unchanged; v2-only fields under v1 are invalid; unsupported newer versions are not partially interpreted.
2. Readiness deterministically reports required/optional toolchain, executable and env-presence facts without values/hashes or repair behavior.
3. `ready/not_ready/partial/unavailable` and cache quality/age follow the approved fold.
4. Readiness never gates arbitrary valid starts and ordinary start never refreshes readiness.
5. Typed request fingerprint and persisted lookup happen before current workspace/manifest/provider reads.
6. Durable pre-binding intent claim prevents conflicting same-ID callers and no child spawns before frozen binding.
7. Lost-response retry after admission replays frozen argv/binding after manifest/source/provider changes without rebind or second spawn.
8. All five kinds are closed and whole-token-only with deterministic default/unknown/missing/leading-dash behavior.
9. Parameterized shell commands are invalid and `depends_on` cannot execute.
10. `repo_path` proves bind-time root/symlink integrity without claiming runtime confinement.
11. `repo_package` unavailable returns `parameter_validation_unavailable`, never raw-string fallback or hot-path package-manager subprocess.
12. Schema-v3 typed operation/receipt binds manifest digest, command ID, parameter fingerprint/provider identity and resolved argv/cwd while old schemas remain readable.
13. Telemetry/repro stay tied to frozen typed binding and never reconstruct from current manifest.
14. Capability discovery advertises readiness/typed versions/kinds/providers/limits and `local_shell` remains the only MCP tool.
15. Cached binding meets p95 <= 10 ms and ordinary non-project start pays zero A5 tax.
16. Focused/package/race/devctl/diff/privacy/anti-goal gates pass fresh on exact final HEAD with clean worktree and untouched `.codegraph`.
