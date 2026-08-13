# ShellBeam Hardening, Release Evidence, and Source Package Plan

> **Requires:** Checkpoints 1–6 green and clean implementation branch. Use `superpowers:verification-before-completion` before any PASS/complete claim.

**Goal:** Close adversarial gaps, document actual evidence, build all target tuples, and create a reproducible credential-free source ZIP for local macOS/Linux use.

### Task 1: Full contract/acceptance traceability

**Files:** `docs/testing/acceptance-matrix.md`, `internal/contract/acceptance_test.go`, `dev/test-impact.toml`.

- [ ] Map every design section 5–16 acceptance item to test name, layer, supported OS, and stable artifact path.
- [ ] Add a test that fails if a required row has no test/evidence owner or references a missing file.
- [ ] Include retry-loss, storage-before-spawn, cursor/retention, stdin/EOF, kill, finalizing, crash-abandonment, peer UID, and service ownership rows explicitly.
- [ ] Commit `test: trace v1 acceptance contracts`.

### Task 2: Race, stress, fuzz, and fault campaign

**Files:** `scripts/test-hardening.sh`, `docs/testing/hardening.md`, `.github/workflows/nightly.yml`.

- [ ] Race: full concurrent packages on current native OS. Stress: same-operation starts, concurrent output/poll, write/backpressure, kill/timeout races, shutdown/finalization.
- [ ] Fuzz bounded runs for closed JSON, IDs/fingerprint, cursor UTF-8, input ledger, receipt decoding, state transitions. Preserve seeds only if failures found.
- [ ] Fault matrix covers partial write, every sync/rename failure, ENOSPC/reserve race, spawn/signal/input/capture failure, daemon death at each state.
- [ ] Script writes safe machine-readable evidence and returns nonzero on any failure; no automatic flaky retries.
- [ ] Commit `test: harden concurrent and fault behavior`.

### Task 3: Security and dependency gate

**Files:** `scripts/test-security.sh`, `docs/security.md`, `docs/dependencies.md`, `.github/dependabot.yml`.

- [ ] Verify no listener except UDS, modes/UID/symlink defenses, peer auth before decode, redaction fixtures, bounded request/response/queue/output/state/logs, and no persisted process capability.
- [ ] Run `go mod tidy -diff`, `go mod verify`, pinned `govulncheck`, secret scan if installed, and license/dependency inventory. Missing optional scanner is `NOT RUN`; module verification is mandatory.
- [ ] Document full local-user authority, non-goals, ambiguous outcomes, tunnel trust boundary, data locations, uninstall preservation, and responsible reporting.
- [ ] Commit `security: verify shellbeam authority boundaries`.

### Task 4: User documentation and tunnel acceptance kit

**Files:** `README.md`, `docs/quickstart.md`, `docs/troubleshooting.md`, `docs/testing/tunnel-e2e.md`, `scripts/e2e-tunnel-user.sh`, `examples/tunnel-client-command.txt`.

- [ ] Quickstart: prerequisites, unpack, verify checksum, `go test`, build, config, daemon/install, doctor, local MCP test, and uninstall.
- [ ] Provide exact Secure MCP Tunnel target command using `shellbeam mcp`, but no credential acquisition/storage automation.
- [ ] User-run script checks prerequisites, requires explicit environment-provided credential configuration owned by tunnel client, executes safe temp-directory prompts for start/poll/write/kill/retry/ambiguity, and cleans only its temp root.
- [ ] Result template separates local shell success from tunnel transport and ChatGPT receipt interpretation. Never mark credentialed E2E PASS by file existence.
- [ ] Commit `docs: add local and tunnel operating guide`.

### Task 5: Release build and manifest

**Files:** `tools/devctl/release.go`, `release_test.go`, `.github/workflows/release.yml`, `LICENSE`.

- [ ] Require clean tree, exact commit, Go 1.26.5, full uncached tests `-count=1`, current-host race/native tests, and explicit `NOT RUN` entries for unavailable OS/credentialed boundaries.
- [ ] Build `CGO_ENABLED=0` darwin/linux amd64/arm64 with `-trimpath -buildvcs=true` and version/commit/builtAt ldflags. Run the native binary smoke test; inspect other binaries with `file`/Go build metadata.
- [ ] Emit `release-evidence.json`, `SHA256SUMS`, dependency inventory/SBOM, artifact manifest with sizes/hashes/toolchain/source fingerprint, and test-evidence links.
- [ ] No signing/notarization claim in private V1 package.
- [ ] Commit `build: create reproducible v1 release evidence`.

### Task 6: Source ZIP creation and clean-room verification

**Files:** `scripts/package-source.sh`, `scripts/verify-source-package.sh`, `docs/testing/package-evidence.md`.

- [ ] Package from `git archive` of the exact implementation commit, then add generated release evidence through a deterministic staging manifest. Exclude `.git`, caches, credentials, runtime/state/socket files, and raw captured output.
- [ ] Name `shellbeam-v1-source-<short-commit>.zip`; produce adjacent `.sha256`.
- [ ] In a new `mktemp -d`, unzip, compare manifest/hashes, run `go mod verify`, focused package verification, full `go test -count=1 ./...`, native build/smoke, and `shellbeam doctor --json` against isolated roots. Do not install a real service during clean-room verification.
- [ ] ZIP must contain design and all plans plus evidence labeling native macOS/tunnel E2E not run when applicable.
- [ ] Commit `build: package verified shellbeam source` before generating the final ZIP from that commit; if this final commit changes sources used by tests, rerun the invalidated full verification.

### Task 7: Final exact-source verification

- [ ] Run `git status --short`; require empty.
- [ ] Run `go version`, `go env GOOS GOARCH CGO_ENABLED`, `go test -count=1 ./...`, full current-host race suite, hardening script, security script, four builds, package clean-room script, and checksum verification.
- [ ] Record commands, exit codes, exact commit/source fingerprint, tool versions, selected/native platform, artifact paths, PASS/NOT RUN/BLOCKED matrix.
- [ ] Inspect ZIP listing for secrets, `.git`, sockets, logs, and caches.
- [ ] Store the final ZIP and checksum as reusable Library artifacts.

## Final claim rules

- Say **V1 source package verified on Linux** only if all mandatory Linux/local checks pass on Linux.
- Say **macOS compile verified; native macOS runtime not run** when only cross-build succeeded.
- Say **Secure MCP Tunnel/ChatGPT E2E not run** unless configured credentials and real path were exercised.
- Never say release-ready for public distribution without native macOS/Linux lanes, credentialed tunnel eval, signing/notarization decision, and all release rows PASS.

