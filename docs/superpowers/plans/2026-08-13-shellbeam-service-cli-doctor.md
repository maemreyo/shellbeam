# ShellBeam Service, CLI, and Doctor Implementation Plan

> **Requires:** Checkpoints 1–5 green. Install/uninstall tests use isolated fake homes and service-manager fakes unless explicitly running the native E2E script.

**Goal:** Compose the shipped `shellbeam` binary, manage only the ShellBeam per-user daemon, and diagnose each trust boundary without managing tunnel credentials.

### Task 1: CLI command composition and stable exits

**Files:** `cmd/shellbeam/main.go`, `command.go`, `daemon.go`, `mcp.go`, `status.go`, `doctor.go`, `install.go`, `uninstall.go`, `*_test.go`, plus `internal/app/control/service.go` and tests.

- [ ] Table-test `daemon`, `mcp`, `install`, `uninstall`, `status`, `doctor`, `version`, help, unknown command, JSON mode, and stable exit codes.
- [ ] `main.go` only wires config/paths/logging/adapters and calls `cli.Run`; no behavior in composition root.
- [ ] Default stdout is concise; diagnostics go stderr; JSON is closed/versioned.
- [ ] Commit `feat: compose shellbeam command surface`.

### Task 2: Linux systemd-user service

**Files:** `internal/adapter/service/systemd.go`, `systemd_test.go`, `assets/systemd/shellbeam.service.tmpl`, `scripts/e2e-systemd-user.sh`.

- [ ] Golden-test absolute escaped executable/config paths, restart policy, no shell interpolation, restrictive umask, and environment.
- [ ] Install writes atomically to verified `~/.config/systemd/user/shellbeam.service`, runs `daemon-reload` then `enable --now`; rollback removes only the newly written unit if activation fails.
- [ ] Uninstall disables/stops and removes unit but preserves config/state/output. Idempotent absent uninstall succeeds.
- [ ] Native E2E runs only when systemd user manager exists; otherwise documented `NOT RUN` code 3.
- [ ] Commit `feat: manage systemd user daemon`.

### Task 3: macOS launchd user service

**Files:** `internal/adapter/service/launchd.go`, `launchd_test.go`, `assets/launchd/com.shellbeam.daemon.plist.tmpl`, `scripts/e2e-launchd-user.sh`.

- [ ] Golden-test valid plist, absolute arguments, KeepAlive, ProcessType, Umask, stdout/stderr operator log paths, no shell interpolation.
- [ ] Install atomically writes verified `~/Library/LaunchAgents/com.shellbeam.daemon.plist`, bootstraps/kickstarts current GUI user domain, and safely rolls back only new artifacts on failure.
- [ ] Uninstall bootouts/removes plist while preserving data. Native script is macOS-only and returns code 3 elsewhere.
- [ ] Commit `feat: manage launchd user daemon`.

### Task 4: Status and boundary-specific doctor

**Files:** `internal/app/control/doctor.go`, `report.go`, `doctor_test.go`, `cmd/shellbeam/doctor_test.go`.

- [ ] Checks are separate: config parse; state/runtime ownership/mode/symlink; daemon service state; socket existence/mode/peer-auth handshake; MCP bridge self-test; `tunnel-client` executable presence/version and optional operator-provided health command.
- [ ] Never read/store tunnel credentials, print tokens, or start/manage tunnel client. Missing tunnel client is actionable warning; unsafe socket/state or daemon protocol mismatch is failure.
- [ ] JSON report includes schema version, check IDs, status `pass|warn|fail|not_run`, safe message/hint, and no raw sensitive values.
- [ ] `status` reports ShellBeam service/daemon only and distinguishes installed, running, reachable, incompatible, stale, unsafe.
- [ ] Commit `feat: diagnose shellbeam trust boundaries`.

### Task 5: Config lifecycle and safe logging

**Files:** `internal/config/load.go`, `validate.go`, `load_test.go`, `internal/observability/log.go`, `redact.go`, `redact_test.go`.

- [ ] Layer defaults < config file < explicit CLI flags; daemon records effective config hash, not secrets. Reject unknown keys and insecure paths.
- [ ] `slog` stable fields: event, incarnation, operation/session IDs, action/state, duration, byte counts, stable code. Redact command, cwd, env, stdin/output, credentials, arbitrary OS errors and raw paths by default.
- [ ] Logging failure is bounded and never recursively logs.
- [ ] Commit `feat: load config and redact diagnostics`.

### Task 6: CLI/service native checkpoint

**Files:** `dev/test-impact.toml`, `docs/adr/0006-user-service-ownership.md`, `docs/operations/install.md`, `docs/operations/doctor.md`.

- [ ] Run CLI black-box tests from built binary, service golden tests, current-host native service E2E if available, socket/doctor fault matrix, dirty/checkpoint/race scopes.
- [ ] Cross-build other OS binary and label service runtime `NOT RUN`.
- [ ] Commit `test: prove service cli checkpoint`.

## Completion gate

Checkpoint 6 requires a buildable single binary, safe per-user daemon management on the tested native service manager, and honest boundary diagnostics. It never bundles or owns tunnel-client credentials/processes.
