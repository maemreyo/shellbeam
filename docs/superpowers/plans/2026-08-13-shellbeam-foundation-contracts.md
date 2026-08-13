# ShellBeam Foundation and Executable Contracts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task. Execute with one primary agent; do not delegate unless the user explicitly authorizes delegation. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bootstrap `github.com/maemreyo/shellbeam` and establish the executable contracts and development controls required before any process can be spawned.

**Architecture:** This checkpoint creates one Go module and one binary composition root, but implements no shell execution. Pure core packages freeze operation fingerprints, session transitions, receipts, and typed configuration; checked-in JSON Schemas freeze the public and persisted envelopes. A minimal Go `devctl` enforces import direction, size policy, schema shape, dirty-test selection, and exact-source evidence so every runtime checkpoint can build on trusted development infrastructure.

**Tech Stack:** Go 1.26.5, Go standard library, `github.com/pelletier/go-toml/v2` v2.3.1, JSON Schema draft 2020-12, GitHub Actions definitions verified locally without requiring a GitHub connector.

**Execution mode:** This checkpoint runs in a new local Git repository. Create one documentation baseline commit on `main`, then execute all implementation commits on `ai/v1-implementation`. No remote, push, pull request, or GitHub connector is required. Commands that accept a base revision use the explicit local `main` ref unless `SHELLBEAM_BASE_REF` is set.

## Global Constraints

- Module path is exactly `github.com/maemreyo/shellbeam`.
- Declare `go 1.26.0` and `toolchain go1.26.5`; CI installs Go 1.26.5 explicitly.
- Keep one root `go.mod`, one shipped binary, and no `go.work`, nested module, database, generator pipeline, service, agent, or network listener.
- This checkpoint must not spawn commands, create process groups, open PTYs, create a daemon socket, install services, or expose an MCP server.
- Production files warn above 350 physical lines and fail above 500; test files warn above 600 and fail above 800.
- Functions require review above 60 physical lines and fail above 80; `cmd/shellbeam/main.go` warns above 100 and fails above 150.
- Interfaces normally contain one to five methods and fail above eight.
- Do not create packages or files named `utils`, `helpers`, `common`, `shared`, `base`, `misc`, or generic `models`.
- Core imports only the standard library and other allowlisted core packages. App may import core. Adapters may import app/core but never sibling adapters. `cmd/shellbeam` is the only composition root.
- Port interfaces live with their consumer. No mutable globals, `init` side effects, fire-and-forget goroutines, unbounded queues, or raw secret-bearing diagnostics.
- Use test-first slices. Run focused tests first, then `devctl test --dirty`; do not run a fresh full suite after every edit.
- Use the Go build/test cache. Do not use `go build -a`, routine `go clean`, or a second compiler cache.
- Every checkpoint receipt is valid only for its recorded source fingerprint.
- Preserve any existing repository initialization files. Stop if an existing file conflicts materially with this plan instead of overwriting it silently.

## Checkpoint Boundary

This plan proves only repository policy and deterministic contracts. Passing it does **not** prove command execution, durable file ordering, process ownership, PTY behavior, Unix peer credentials, MCP negotiation, service installation, or Secure MCP Tunnel behavior.

The next plan consumes this checkpoint to implement durable operation reservation plus a minimal `start`/`poll` process tracer. It may add runtime dependencies only when their boundary is exercised.

---

### Task 1: Bootstrap the module and non-executing CLI

**Files:**

- Create: `go.mod`
- Create: `.gitignore`
- Create: `AGENTS.md`
- Create: `cmd/shellbeam/main.go`
- Create: `cmd/shellbeam/main_test.go`
- Create: `internal/buildinfo/info.go`
- Create: `internal/buildinfo/info_test.go`

**Interfaces:**

- Produces: `buildinfo.Info{Version, Commit, BuiltAt}` and `buildinfo.Current() Info`.
- Produces: `run(args []string, stdout, stderr io.Writer) int` in the `main` package.
- CLI behavior in this checkpoint: `shellbeam version [--json]` succeeds; every runtime subcommand exits with code 2 and the stable message `not available in foundation checkpoint`.

- [ ] **Step 1: Confirm the execution precondition**

Run from a fresh checkout of `https://github.com/maemreyo/shellbeam`:

```bash
git status --short --branch
git branch --show-current
git ls-files
```

Expected: the current branch is `ai/v1-implementation`, `main` resolves to the documentation baseline commit, and the tree is clean. The packaging workspace is the authority for this execution; no remote is expected.

- [ ] **Step 2: Write the failing build-info tests**

```go
package buildinfo

import "testing"

func TestCurrentUsesLinkerValues(t *testing.T) {
	oldVersion, oldCommit, oldBuiltAt := version, commit, builtAt
	t.Cleanup(func() { version, commit, builtAt = oldVersion, oldCommit, oldBuiltAt })
	version, commit, builtAt = "v0.1.0-dev", "abc123", "2026-08-13T00:00:00Z"

	got := Current()
	if got.Version != "v0.1.0-dev" || got.Commit != "abc123" || got.BuiltAt != "2026-08-13T00:00:00Z" {
		t.Fatalf("Current() = %#v", got)
	}
}
```

- [ ] **Step 3: Run the focused test and verify RED**

Run:

```bash
go test ./internal/buildinfo -run TestCurrentUsesLinkerValues -count=1
```

Expected: compilation fails because `Current` and the linker-backed variables do not exist.

- [ ] **Step 4: Create `go.mod` and the minimal build-info implementation**

`go.mod` begins exactly as:

```go
module github.com/maemreyo/shellbeam

go 1.26.0

toolchain go1.26.5
```

`internal/buildinfo/info.go` defines:

```go
package buildinfo

type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func Current() Info {
	return Info{Version: version, Commit: commit, BuiltAt: builtAt}
}
```

- [ ] **Step 5: Add CLI contract tests**

Cover all of these cases in `cmd/shellbeam/main_test.go`:

```go
func TestRunVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version", "--json"}, &stdout, &stderr)
	if code != 0 { t.Fatalf("code = %d, stderr = %q", code, stderr.String()) }
	var got buildinfo.Info
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil { t.Fatal(err) }
	if got.Version == "" || got.Commit == "" || got.BuiltAt == "" { t.Fatalf("incomplete: %#v", got) }
}

func TestRunRejectsRuntimeCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"daemon"}, &stdout, &stderr)
	if code != 2 { t.Fatalf("code = %d", code) }
	if !strings.Contains(stderr.String(), "not available in foundation checkpoint") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
```

Also table-test empty arguments, unknown commands, `version`, `version --json`, and an extra version argument. Unknown usage exits 2; valid version exits 0.

- [ ] **Step 6: Implement the CLI and entry point**

Keep `cmd/shellbeam/main.go` below 100 lines. `main` calls only:

```go
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
```

Use `flag.NewFlagSet` with `ContinueOnError`; write usage failures to the injected stderr. JSON output uses `json.NewEncoder(stdout).Encode(buildinfo.Current())`. Plain output is `shellbeam <version> (<commit>, <built_at>)`.

- [ ] **Step 7: Add repository guidance and ignores**

`.gitignore` contains only generated local state:

```gitignore
/.build/
/dist/
*.test
```

`AGENTS.md` records the module map, non-goals, import direction, size limits, focused/dirty/checkpoint commands, no-full-suite local policy, no destructive Git operations, no push/PR/install without user authorization, and the rule that terminal success requires exact-source evidence. Keep it under 250 lines.

- [ ] **Step 8: Verify and commit the bootstrap**

Run:

```bash
gofmt -w cmd/shellbeam/main.go cmd/shellbeam/main_test.go internal/buildinfo/info.go internal/buildinfo/info_test.go
go mod tidy
go test ./internal/buildinfo ./cmd/shellbeam
go run ./cmd/shellbeam version --json
git diff --check
```

Expected: tests pass; the command prints valid JSON; `git diff --check` exits 0.

Commit:

```bash
git add go.mod .gitignore AGENTS.md cmd/shellbeam internal/buildinfo
git commit -m "chore: bootstrap shellbeam module"
```

---

### Task 2: Freeze schema identities and closed-envelope rules

**Files:**

- Create: `api/schema/mcp-input-v1.json`
- Create: `api/schema/mcp-output-v1.json`
- Create: `api/schema/ipc-v1.json`
- Create: `api/schema/receipt-v1.json`
- Create: `api/schema/config-v1.json`
- Create: `api/schema/README.md`
- Create: `api/schema/embed.go`
- Create: `api/schema/embed_test.go`
- Create: `tests/contract/schema_contract_test.go`

**Interfaces:**

- Produces: `schema.Name` constants `MCPInputV1`, `MCPOutputV1`, `IPCV1`, `ReceiptV1`, `ConfigV1`.
- Produces: `schema.Load(name Name) ([]byte, error)` from an embedded `*.json` filesystem in the authoritative `api/schema` directory.
- The checked-in files under `api/schema` are authoritative; no production code generates them at runtime.

- [ ] **Step 1: Write schema inventory tests**

Create a table with the five exact filenames and require each document to have:

```go
type schemaHeader struct {
	Schema string `json:"$schema"`
	ID     string `json:"$id"`
	Title  string `json:"title"`
	Type   string `json:"type"`
}
```

Expected `$schema` is `https://json-schema.org/draft/2020-12/schema`. IDs are:

```text
https://shellbeam.dev/schema/mcp-input-v1.json
https://shellbeam.dev/schema/mcp-output-v1.json
https://shellbeam.dev/schema/ipc-v1.json
https://shellbeam.dev/schema/receipt-v1.json
https://shellbeam.dev/schema/config-v1.json
```

- [ ] **Step 2: Run the inventory test and verify RED**

Run:

```bash
go test ./tests/contract -run TestSchemaInventory -count=1
```

Expected: failure listing all five missing schema files.

- [ ] **Step 3: Add the exact MCP input schema**

Create the closed `oneOf` union from the approved design with four action branches:

| Action | Required | Optional |
| --- | --- | --- |
| `start` | `action`, `operation_id`, `command`, `cwd` | `tty`, `yield_time_ms`, `timeout_ms`, `max_output_bytes` |
| `poll` | `action`, `session_id` | `cursor`, `yield_time_ms`, `max_output_bytes` |
| `write` | `action`, `session_id`, `input_offset`, exactly one of `chars` or `eof=true` | none |
| `kill` | `action`, `session_id`, `kill_id` | `signal` |

Every object has `additionalProperties:false`. IDs use `^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`. Integers are non-negative. `signal` is `INT|TERM|KILL`.

- [ ] **Step 4: Add the exact MCP output schema**

Use `oneOf` with these discriminators and no unknown fields:

```text
ok=true, action=start
ok=true, action=poll
ok=true, action=write
ok=true, action=kill
ok=false
```

The `start`/`poll` branches require `schema_version`, `ok`, `action`, `session_id`, `state`, `outcome`, `cursor`, `next_cursor`, `output`, `truncated`, and `receipt`; `start` additionally requires `operation_id`. The write branch requires accepted bytes, next input offset, stdin-closed state, and receipt. The kill branch requires kill ID, requested signal, signal-attempt state, session state, and receipt. The error branch requires `code`, `message`, `hint`, and `retryable`; `details` is a closed union keyed by error code.

- [ ] **Step 5: Add IPC, receipt, and config schemas**

Freeze independent `schema_version:1` envelopes:

- IPC request: `request_id`, `method`, `body`; response: the same `request_id` and exactly one of `result` or `error`.
- Receipt: all fields in design section 9, including spawn/exit evidence, input accounting, termination cause, escalation, and kill attempts.
- Config: the exact limits and paths introduced in Task 5; unknown keys fail.

Persisted enums are closed. Optional evidence is represented with JSON `null`, not missing keys, so golden comparisons remain stable.

- [ ] **Step 6: Add structural contract tests**

Without adding a general schema library, parse each schema into `map[string]any` and assert:

```go
func requireClosedObject(t *testing.T, node map[string]any) {
	t.Helper()
	if got, ok := node["additionalProperties"].(bool); !ok || got {
		t.Fatalf("object is not closed: %#v", node)
	}
}
```

Walk every object under `oneOf`, `$defs`, `properties`, and `items`. Assert four input branches, five output branches, unique discriminator values, version 1 constants, and the required receipt evidence fields.

- [ ] **Step 7: Add the embedded schema loader**

Use `//go:embed *.json` in `api/schema/embed.go`. `Load` accepts only the five typed `Name` constants, reads from the embedded filesystem, and returns a copied byte slice so callers cannot mutate shared bytes. The JSON files beside `embed.go` are both the reviewed source and the embedded bytes; no mirror or generation step exists.

- [ ] **Step 8: Document schema ownership and compatibility**

`api/schema/README.md` states:

- schemas are reviewed source files, not generated artifacts;
- input/output, IPC, receipt, and config version independently;
- unknown major versions and closed-union variants are rejected;
- any persisted-schema change requires golden read-old/migrate/reject evidence;
- MCP SDK types cannot appear outside `internal/adapter/mcp`.

- [ ] **Step 9: Verify and commit schema contracts**

Run:

```bash
go test ./api/schema ./tests/contract -count=1
git diff --check
```

Commit:

```bash
git add api/schema tests/contract
git commit -m "feat: freeze versioned shellbeam contracts"
```

---

### Task 3: Implement pure session and receipt invariants

**Files:**

- Create: `internal/core/session/state.go`
- Create: `internal/core/session/state_test.go`
- Create: `internal/core/receipt/receipt.go`
- Create: `internal/core/receipt/validate.go`
- Create: `internal/core/receipt/receipt_test.go`
- Create: `internal/core/operation/id.go`
- Create: `internal/core/operation/id_test.go`

**Interfaces:**

- Produces: `session.State`, `session.Outcome`, `session.CanTransition(from, to State) bool`, and `session.IsTerminal(State) bool`.
- Produces: `operation.ValidateID(string) error` for operation, session, request, and kill IDs.
- Produces: version-1 receipt value types plus `receipt.ValidateSnapshot(Receipt) error` and `receipt.ValidateTerminal(Receipt) error`.

- [ ] **Step 1: Write the exhaustive transition table test**

Define the allowed edges exactly:

```go
var allowed = map[State][]State{
	StateStarting:   {StateRunning, StateFinalizing, StateAbandoned},
	StateRunning:    {StateFinalizing, StateAbandoned},
	StateFinalizing: {StateCompleted, StateFailed, StateTimedOut, StateKilled, StateAbandoned},
}
```

Test every Cartesian pair of all eight states. Terminal states have no outgoing edge. Same-state observation is not a transition and returns false.

- [ ] **Step 2: Run the state test and verify RED**

Run:

```bash
go test ./internal/core/session -run TestTransitionMatrix -count=1
```

Expected: compilation fails because the state types do not exist.

- [ ] **Step 3: Implement state and outcome types**

Use string-backed constants with `Valid` methods. States are `starting`, `running`, `finalizing`, `completed`, `failed`, `timed_out`, `killed`, and `abandoned`. Outcomes are `success`, `failure`, `timeout`, `killed`, and `ambiguous`.

- [ ] **Step 4: Implement strict ID validation test-first**

Accept 1–128 ASCII bytes matching `[A-Za-z0-9][A-Za-z0-9_-]*`. Reject slashes, dots, whitespace, non-ASCII, empty strings, and 129-byte strings. Return a typed sentinel `operation.ErrInvalidID`; never include the rejected ID in the error text.

- [ ] **Step 5: Write receipt validation cases**

Required cases:

- success requires `completed`, spawn succeeded, child reaped, exit code 0, complete output, and accepted input equal to delivered input;
- non-zero exit maps to `failed/failure`;
- spawn failure uses `failed/failure`, `spawn_failed`, and null exit evidence;
- timeout maps to `timed_out/timeout`;
- explicit kill maps to `killed/killed`;
- abandoned maps only to `abandoned/ambiguous` and may lack output/exit evidence;
- `finalizing` is a valid snapshot but never a valid terminal receipt;
- no terminal receipt may have `input_delivered_bytes > input_accepted_bytes`.

- [ ] **Step 6: Implement receipt value types and validators**

Keep receipt types independent of JSON Schema and storage. Use explicit structs for `SpawnEvidence`, `ExitEvidence`, `SignalAttempt`, and `EscalationStep`. `ValidateTerminal` first calls `ValidateSnapshot`, then enforces terminal-only evidence.

Errors expose stable internal categories through `errors.Is`; messages contain field names and stable enum values but no command, cwd, stdin, or output.

- [ ] **Step 7: Run focused and package tests**

Run:

```bash
go test ./internal/core/session -count=1
go test ./internal/core/operation -count=1
go test ./internal/core/receipt -count=1
```

- [ ] **Step 8: Commit the pure invariants**

```bash
git add internal/core
git commit -m "feat: define session and receipt invariants"
```

---

### Task 4: Freeze the operation intent fingerprint

**Files:**

- Create: `internal/core/operation/intent.go`
- Create: `internal/core/operation/intent_test.go`
- Create: `tests/contract/testdata/intent-v1.json`

**Interfaces:**

- Produces: `operation.Intent{Command, CWD, TTY, TimeoutMS}`.
- Produces: `operation.FingerprintV1(Intent) ([32]byte, error)`.
- Produces: `operation.FingerprintHexV1(Intent) (string, error)`.

- [ ] **Step 1: Write sensitivity and stability tests**

Use a base intent and independently vary command bytes, exact validated cwd string, TTY, and timeout. Each variation must change the digest. Confirm response-only `yield_time_ms` and `max_output_bytes` cannot be passed to `Intent` at compile time.

Add a golden vector:

```json
{
  "command_utf8": "printf 'hello\\n'",
  "cwd": "/tmp/project",
  "tty": false,
  "timeout_ms": 60000,
  "fingerprint_sha256": "cab52b0d38dda131379448990155c989fbbf9f4245413e95dea02a69236cf61a"
}
```

The test computes the same digest with a tiny test-local reference encoder and compares both the production result and this 64-character lowercase fixture.

- [ ] **Step 2: Run the fingerprint test and verify RED**

```bash
go test ./internal/core/operation -run Fingerprint -count=1
```

Expected: compilation fails because `Intent` and `FingerprintV1` do not exist.

- [ ] **Step 3: Implement the canonical encoder**

Hash this exact byte sequence:

```text
ASCII "shellbeam.intent.v1" + NUL
uint64 big-endian command length + command bytes
uint64 big-endian cwd length + cwd UTF-8 bytes
one byte TTY: 0 or 1
uint64 big-endian timeout_ms
```

Reject empty command, non-absolute cwd, negative timeout, and integer overflow before hashing. Do not normalize cwd, trim command bytes, resolve a shell, or use JSON encoding.

- [ ] **Step 4: Verify the golden vector and mutation cases**

```bash
go test ./internal/core/operation -run 'Fingerprint|Intent' -count=1
```

Expected: all golden and sensitivity cases pass.

- [ ] **Step 5: Commit the fingerprint contract**

```bash
git add internal/core/operation tests/contract/testdata/intent-v1.json
git commit -m "feat: freeze operation intent fingerprint"
```

---

### Task 5: Implement typed configuration, paths, and resource defaults

**Files:**

- Create: `internal/config/config.go`
- Create: `internal/config/defaults.go`
- Create: `internal/config/load.go`
- Create: `internal/config/paths.go`
- Create: `internal/config/config_test.go`
- Create: `internal/config/paths_test.go`
- Create: `tests/contract/testdata/config/minimal.toml`
- Create: `tests/contract/testdata/config/complete.toml`
- Create: `tests/contract/testdata/config/unknown-key.toml`

**Interfaces:**

- Produces: `config.Defaults() Config`.
- Produces: `config.Load(path string, overrides Overrides) (Config, error)`.
- Produces: `config.ResolvePaths(goos string, uid int, home string, env map[string]string) (Paths, error)`.
- Produces: immutable validated config passed by value or read-only pointer after startup.

- [ ] **Step 1: Write exact default tests**

Freeze these initial personal-developer defaults:

| Setting | Value |
| --- | ---: |
| `max_concurrent_sessions` | 4 |
| `default_yield_ms` | 10,000 |
| `max_yield_ms` | 30,000 |
| `default_max_output_bytes` | 20,000 |
| `max_response_output_bytes` | 262,144 |
| `max_command_bytes` | 32,768 |
| `max_stdin_call_bytes` | 65,536 |
| `max_queued_input_session_bytes` | 262,144 |
| `max_queued_input_total_bytes` | 1,048,576 |
| `max_session_output_bytes` | 268,435,456 |
| `max_total_state_bytes` | 10,737,418,240 |
| `min_free_space_bytes` | 536,870,912 |
| `control_reserve_session_bytes` | 1,048,576 |
| `terminal_retention_hours` | 168 |
| `max_timeout_ms` | 86,400,000 |
| `termination_grace_ms` | 5,000 |
| `finalize_retry_min_ms` | 100 |
| `finalize_retry_max_ms` | 5,000 |

These values are version-1 defaults, not performance claims. Changing one requires a config-schema fixture and review of admission/fault tests.

- [ ] **Step 2: Write validation tests and verify RED**

Reject zero concurrency, default values above maxima, per-session queue above total queue, session output plus control reserve above total state, free-space reserve below control reserve, negative durations, retry minimum above maximum, relative state/runtime paths, and non-absolute configured shell paths.

Run:

```bash
go test ./internal/config -run 'Defaults|Validate' -count=1
```

Expected: compilation fails because `Config`, `Defaults`, and `Validate` do not exist.

- [ ] **Step 3: Implement strict TOML loading and override precedence**

Add the dependency explicitly at the point of first use:

```bash
go get github.com/pelletier/go-toml/v2@v2.3.1
```

Precedence is CLI override > TOML > default. Use `toml.NewDecoder(reader).DisallowUnknownFields()`. An absent config file returns defaults; malformed TOML, unknown keys, wrong types, or invalid values return a typed configuration error.

Represent CLI overrides with pointers so zero is distinguishable from unset:

```go
type Overrides struct {
	RuntimeDir            *string
	StateDir              *string
	Shell                 *string
	MaxConcurrentSessions *int
}
```

Only fields exposed by the current CLI may appear in `Overrides`; do not mirror every config field speculatively.

- [ ] **Step 4: Implement deterministic platform path resolution**

Linux:

- config: `$XDG_CONFIG_HOME/shellbeam/config.toml`, else `$HOME/.config/shellbeam/config.toml`;
- state: `$XDG_STATE_HOME/shellbeam`, else `$HOME/.local/state/shellbeam`;
- runtime: `$XDG_RUNTIME_DIR/shellbeam` when absolute, else `/tmp/shellbeam-<uid>`.

macOS:

- config: `$HOME/Library/Application Support/ShellBeam/config.toml`;
- state: `$HOME/Library/Application Support/ShellBeam`;
- runtime: `/tmp/shellbeam-<uid>`.

Reject unsupported OS values, missing/relative home paths, relative XDG paths, negative UIDs, and a runtime socket path whose UTF-8 byte length would exceed 100 bytes after appending `/daemon.sock`. This conservative bound stays below common Unix-socket limits on both target platforms.

- [ ] **Step 5: Test strict fixtures and path cases**

Test complete/minimal TOML, unknown key, malformed value, every precedence level, Linux XDG/fallback, macOS, unsupported OS, and socket length. Tests inject all environment data; they do not mutate the real process environment.

- [ ] **Step 6: Synchronize `config-v1.json` with the typed config**

Add a contract test that marshals `Defaults()` to a generic map and compares its keys and numeric defaults to `api/schema/config-v1.json`. The schema remains authoritative for external shape; the Go defaults are authoritative runtime values. Any mismatch fails with the exact key.

- [ ] **Step 7: Verify and commit configuration**

```bash
gofmt -w internal/config
go test ./internal/config ./tests/contract -count=1
go mod tidy -diff
git diff --check
git add internal/config tests/contract/testdata/config api/schema/config-v1.json go.mod go.sum
git commit -m "feat: define shellbeam configuration defaults"
```

---

### Task 6: Enforce architecture, naming, size, and waiver policy

**Files:**

- Create: `dev/architecture.toml`
- Create: `dev/quality-waivers.toml`
- Create: `tools/devctl/main.go`
- Create: `tools/devctl/internal/command/run.go`
- Create: `tools/devctl/internal/architecture/check.go`
- Create: `tools/devctl/internal/architecture/check_test.go`
- Create: `tools/devctl/internal/quality/check.go`
- Create: `tools/devctl/internal/quality/check_test.go`
- Create: `tools/devctl/internal/quality/testdata/`

**Interfaces:**

- Produces: `devctl check architecture`, `devctl check size`, and `devctl check schema`.
- Produces: `architecture.Check(ctx, root, manifest) Report`.
- Produces: `quality.Check(root, policy, now) Report`.
- Reports contain deterministic sorted violations and never rewrite source files.

- [ ] **Step 1: Write architecture fixture tests**

Create tiny fixture modules that prove:

- core importing app fails;
- app importing adapter fails;
- one adapter importing a sibling adapter fails;
- production importing `internal/testkit` fails;
- command composition root importing app/adapter/config/observability passes;
- allowed core-to-core and app-to-core imports pass;
- banned catch-all package segment fails even without imports.

Each failure assertion checks the source package, imported package, rule ID, and suggested boundary.

- [ ] **Step 2: Define the machine-readable barrier manifest**

`dev/architecture.toml` declares path prefixes and allowed destination classes, plus banned package segments. Give every rule a stable ID such as `ARCH_CORE_INWARD`, `ARCH_ADAPTER_SIBLING`, and `ARCH_CATCH_ALL_NAME`.

- [ ] **Step 3: Implement architecture checking**

Run `go list -json ./...`, decode package import lists, classify repository-local imports by longest matching prefix, and evaluate direct edges. An unclassified production package is a failure. A `go list` error is a failure, never an empty success.

Do not inspect vendor or module-cache packages. Sort by rule ID, source, and destination before output.

- [ ] **Step 4: Write size and waiver tests**

Fixtures cover production lines 350/351/500/501, test lines 600/601/800/801, function spans 60/61/80/81, main 100/101/150/151, generated exclusion, expired waiver, 31-day waiver, and a valid 30-day waiver.

Warnings exit 0 but appear in both human and JSON reports. Hard-gate violations and expired/invalid waivers exit 1.

- [ ] **Step 5: Implement physical-line and AST span checking**

Count physical lines with `bufio.Scanner` using an enlarged but bounded buffer. Use `go/parser` and `token.FileSet` for function/method spans and interface method counts. Exclude `vendor`, `testdata`, checked-in schemas/goldens, and files beginning with the standard `Code generated ... DO NOT EDIT.` header.

Waivers require exact target, rule ID, reason, rejected alternatives, owner, issue URL, and ISO date expiry. The initial `quality-waivers.toml` contains no waivers.

- [ ] **Step 6: Add `devctl` dispatch and JSON output**

`tools/devctl/main.go` remains a small entry point. `command.Run` accepts injected stdout/stderr and maps usage to exit 2, policy violations to exit 1, and success to exit 0. Every check supports `--json`; human output prints one line per warning/error plus a summary.

- [ ] **Step 7: Verify policy against the real repository**

```bash
go test ./tools/devctl/internal/architecture ./tools/devctl/internal/quality -count=1
go run ./tools/devctl check architecture
go run ./tools/devctl check size
go run ./tools/devctl check schema
```

Expected: all commands exit 0; no hard-gate violation; any review warning is intentional and explained.

- [ ] **Step 8: Commit policy enforcement**

```bash
git add dev tools/devctl
git commit -m "feat: enforce shellbeam architecture policy"
```

---

### Task 7: Implement fail-closed dirty selection and evidence receipts

**Files:**

- Create: `dev/test-impact.toml`
- Create: `tools/devctl/internal/changes/changes.go`
- Create: `tools/devctl/internal/changes/changes_test.go`
- Create: `tools/devctl/internal/impact/graph.go`
- Create: `tools/devctl/internal/impact/select.go`
- Create: `tools/devctl/internal/impact/select_test.go`
- Create: `tools/devctl/internal/evidence/receipt.go`
- Create: `tools/devctl/internal/evidence/receipt_test.go`
- Create: `tools/devctl/internal/runner/test.go`
- Create: `tools/devctl/internal/runner/test_test.go`

**Interfaces:**

- Produces: `devctl explain [--base <rev>] [--working-tree-only] [--json]`.
- Produces: `devctl test --focused <pkg> --run <regexp>` and `devctl test --dirty [--base <rev>] [--working-tree-only] [--json]`.
- Produces: version-1 evidence receipts under `.build/receipts/`.

- [ ] **Step 1: Write change-set fixture tests**

Create temporary Git repositories and cover committed-since-merge-base, staged, unstaged, deleted, renamed, and untracked non-ignored files. Verify `--working-tree-only` excludes committed changes and marks its receipt `checkpoint_eligible=false`.

The default base is resolved in this order: explicit `--base`, non-empty `SHELLBEAM_BASE_REF`, then local `main`. A missing base, merge-base failure, Git command failure, or unparseable status fails visibly and selects all packages only when a valid module graph is still available. It must never probe or require a network remote.

- [ ] **Step 2: Implement deterministic source fingerprinting**

Hash a versioned stream containing:

```text
HEAD commit
merge-base commit
sorted changed paths with change kind
git diff --binary against merge-base
git diff --binary --cached
git diff --binary for unstaged changes
sorted untracked paths and SHA-256 of each file's bytes
```

Use length prefixes and a domain separator `shellbeam.devctl.source.v1`. Never use timestamps. Refuse an untracked file above 64 MiB with a visible selector error and broaden the test scope rather than silently omitting it.

- [ ] **Step 3: Write package-graph selection tests**

Fixture modules prove:

- production Go change selects owner plus reverse transitive dependents;
- `_test.go` selects only owner package;
- new/deleted Go files use directory ownership plus before/after graph;
- shared testkit change selects declared consumers;
- `go.mod`, `go.sum`, or toolchain change selects all packages;
- unknown path broadens to the nearest barrier, then all if unclassified;
- graph failure cannot produce an empty selection.

- [ ] **Step 4: Implement package and non-Go impact selection**

Build the graph from `go list -deps -test -json ./...`. Cache only the decoded graph under `.build/cache/devctl/`, keyed by Go version, GOOS, GOARCH, build tags, `go.mod`, `go.sum`, and package source manifests. Cache corruption causes recomputation, not success with an empty graph.

`dev/test-impact.toml` maps:

- `api/schema/**` to contract tests;
- `dev/**` and `tools/devctl/**` to all devctl policy/self-tests;
- `docs/**` to documentation checks;
- `.github/workflows/**` to CI contract checks;
- `internal/core/**` to its package plus direct contract consumers.

- [ ] **Step 5: Define the exact evidence receipt**

```go
type Receipt struct {
	SchemaVersion      int               `json:"schema_version"`
	Command            string            `json:"command"`
	BaseRevision       string            `json:"base_revision"`
	HeadRevision       string            `json:"head_revision"`
	SourceFingerprint  string            `json:"source_fingerprint_sha256"`
	CheckpointEligible bool              `json:"checkpoint_eligible"`
	SelectedPackages   []string          `json:"selected_packages"`
	SelectedSuites     []string          `json:"selected_suites"`
	Reasons            map[string][]string `json:"reasons"`
	Executions         []Execution       `json:"executions"`
	CacheMode          string            `json:"cache_mode"`
	StartedAt          time.Time         `json:"started_at"`
	FinishedAt         time.Time         `json:"finished_at"`
}
```

`Execution` records argv as separate redacted-safe strings, exit code, duration, and artifact paths. `CacheMode` is `go-native`. Environment values and raw file contents never enter the receipt.

- [ ] **Step 6: Implement focused and dirty runners**

Focused runs exactly:

```text
go test <pkg> -run <regexp>
```

Dirty runs one deterministic `go test` command for the sorted selected package list and separate mapped contract/integration commands. Preserve normal Go caching; do not add `-count=1`. An empty legitimate Go selection still runs mapped non-Go suites. A completely empty change set prints `no affected units` and exits 0 with a receipt; selector failure never takes this branch.

- [ ] **Step 7: Verify lost/changed-source evidence behavior**

Tests prove two identical trees produce the same source fingerprint, a one-byte source change invalidates it, execution exit codes are preserved, and a failed test produces a receipt before `devctl` exits non-zero.

- [ ] **Step 8: Exercise the selector on the repository**

```bash
go test ./tools/devctl/internal/... -count=1
go run ./tools/devctl explain --working-tree-only
go run ./tools/devctl test --dirty --working-tree-only
```

Expected: the explanation names every current change and selection reason; dirty tests pass; a JSON receipt exists beneath `.build/receipts/`.

- [ ] **Step 9: Commit dirty selection**

```bash
git add dev/test-impact.toml tools/devctl
git commit -m "feat: add affected test selection"
```

---

### Task 8: Add checkpoint orchestration and CI proof

**Files:**

- Create: `tools/devctl/internal/runner/checkpoint.go`
- Create: `tools/devctl/internal/runner/checkpoint_test.go`
- Create: `.github/workflows/checkpoint.yml`
- Create: `tests/contract/ci_contract_test.go`
- Create: `docs/adr/0001-foundation-and-contract-ownership.md`
- Modify: `tools/devctl/internal/command/run.go`
- Modify: `dev/test-impact.toml`

**Interfaces:**

- Produces: `devctl verify --checkpoint [--base <rev>] [--working-tree-only] [--json]`.
- Checkpoint composes schema, architecture, size, `go vet`, dirty tests, and module verification without a preceding full build.
- CI recomputes scope from the target merge-base and uploads the exact-source receipt.

- [ ] **Step 1: Write orchestration-order tests**

Use an injected command executor and assert this order:

```text
check schema
check architecture
check size
go vet <selected packages>
test --dirty
go mod tidy -diff
go mod verify
```

Stop after the first failed stage, write its execution result to the receipt, and return its non-zero status. Do not run `go build`; tests compile selected packages.

- [ ] **Step 2: Implement `verify --checkpoint`**

Reuse the one computed change set, package graph, and source fingerprint across all stages. Re-check the source fingerprint immediately before final receipt publication; if the tree changed during verification, set `checkpoint_eligible=false`, exit 1, and print `source changed during verification`.

- [ ] **Step 3: Add CI contract tests before the workflow**

The test parses `.github/workflows/checkpoint.yml` as text plus a small YAML-shape check and requires:

- pull request and push-to-main triggers;
- Ubuntu and macOS jobs;
- Go version `1.26.5`;
- `go run ./tools/devctl verify --checkpoint`;
- PR base SHA passed explicitly;
- receipt artifact upload with `if: always()`;
- no `go build -a`, `go clean`, cache deletion, or full release command.

- [ ] **Step 4: Run the CI contract test and verify RED**

```bash
go test ./tests/contract -run TestCheckpointWorkflow -count=1
```

Expected: failure because the workflow does not exist.

- [ ] **Step 5: Create the checkpoint workflow**

Use a matrix with `ubuntu-latest` and `macos-latest`, `actions/checkout@v5`, `actions/setup-go@v6`, and `actions/upload-artifact@v7`, read-only default permissions, Go/module caches keyed by OS, Go version, and `go.sum`, and a 20-minute job timeout.

For pull requests pass `${{ github.event.pull_request.base.sha }}`. For pushes to main pass `${{ github.event.before }}`. Upload `.build/receipts/**` and safe test artifacts even on failure. Do not expose secrets; this workflow needs no credentials beyond repository read access.

- [ ] **Step 6: Record the boundary decision**

ADR 0001 states why ShellBeam uses one module, checked-in versioned contracts, inward dependency rules, native Go caching, affected testing, and no runtime implementation in this checkpoint. It records the rollback: remove unwired packages/contracts before any persisted V1 data exists.

- [ ] **Step 7: Run fresh checkpoint verification**

Run from the final source tree:

```bash
go run ./tools/devctl verify --checkpoint --working-tree-only
git diff --check
```

Expected: every stage exits 0; the receipt is marked ineligible only because `--working-tree-only` intentionally omits committed-since-base scope.

Then commit:

```bash
git add .github tools/devctl tests/contract dev/test-impact.toml docs/adr
git commit -m "ci: establish shellbeam checkpoint verification"
```

- [ ] **Step 8: Run the authoritative post-commit checkpoint**

Run without `--working-tree-only`:

```bash
go run ./tools/devctl verify --checkpoint --base main --json
git status --short
```

Expected: checkpoint exits 0, receipt has `checkpoint_eligible=true`, the source fingerprint matches the committed tree plus zero working-tree changes, and `git status --short` is empty.

Do not claim runtime readiness. Report only foundation-contract readiness with the receipt path, commit SHA, Go toolchain, selected packages/suites, and exit codes.

## Plan Self-Review Map

| Approved-design requirement | Implemented by this checkpoint |
| --- | --- |
| Go module/toolchain and one binary | Task 1 |
| Closed MCP input/output schemas | Task 2 |
| Versioned IPC/receipt/config schemas | Task 2 |
| Session states including `finalizing` | Task 3 |
| Receipt evidence consistency | Task 3 |
| Retry intent fingerprint excludes response tuning | Task 4 |
| Exact resource defaults and strict config | Task 5 |
| Modular monolith and import barriers | Task 6 |
| File/function/interface limits and waivers | Task 6 |
| Dirty affected-package selection | Task 7 |
| Native cache and exact-source evidence | Tasks 7–8 |
| Checkpoint CI on macOS/Linux | Task 8 |

Explicitly not covered here: persistence adapter semantics, capacity/storage reservation, process spawning, output logs, stdin delivery, signal ownership, retention execution, Unix socket peer authentication, MCP SDK integration, service managers, doctor, tunnel E2E, race/fuzz/fault runtime suites, and release builds. Each belongs to a subsequent independently reviewable plan.

## Completion Gate

Checkpoint 1 is complete only when all eight commits exist in order, the post-commit `devctl verify --checkpoint --base main --json` receipt is eligible and green, the working tree is clean, and no runtime command is implemented. The handoff must say **foundation-contract ready**, never **runtime proven** or **V1 complete**.
