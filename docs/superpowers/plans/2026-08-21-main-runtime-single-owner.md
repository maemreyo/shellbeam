# Main Runtime Single-Owner Implementation Plan

 > **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

 **Goal:** Prevent two `run-main-runtime.sh` launchers/tunnels from surviving concurrently and routing requests through stale MCP binaries.

 **Architecture:** Keep daemon ownership authoritative for the daemon itself, and add a machine-global launcher ownership record for the operational main-runtime stack. Acquisition uses atomic `mkdir`, records launcher PID plus process-start fingerprint to reject PID reuse, gracefully terminates a recorded incumbent launcher, and has an exact-profile legacy tunnel migration path for pre-fix launchers that have no owner record.

 **Tech Stack:** POSIX `sh`, Darwin `ps`, existing `tunnel-client`, ShellBeam `doctor`, shell regression tests.

 **Spec:** `scripts/run-main-runtime.sh` ownership/lifecycle contract and the 2026-08-21 duplicate-runtime incident reproduced by stale launcher PID 63651 / tunnel PID 63801 / MCP PID 63809 alongside the current stack.

 ## Global Constraints

 - Do not use repo-local ownership state; ownership must be per-user and machine-global.
 - Do not infer daemon ownership from command-line matching; keep `doctor`/daemon lifetime lease authoritative.
 - Never signal a recorded launcher unless PID and process-start fingerprint both match.
 - Legacy tunnel discovery may match only the exact `tunnel-client run --profile <profile>` command shape.
 - A signal to a fixed launcher must synchronously cleanup tunnel and daemon before the launcher exits.
 - Do not disturb unrelated worktrees or sessions during tests.

 ---

 ### Task 1: Add stack ownership helper and regression coverage

 **Files:**
 - Create: `scripts/lib/main-runtime-owner.sh`
 - Create: `scripts/test-main-runtime-owner.sh`

 **Interfaces:**
 - Consumes: `TUNNEL_PROFILE`, `STOP_TIMEOUT_SECONDS`, `RUNTIME_OWNER_DIR`, and caller-provided `log`/`die` functions.
 - Produces: `retire_legacy_tunnels`, `acquire_runtime_owner`, `release_runtime_owner`, `runtime_process_started`.

 - [x] **Step 1: Write failing tests** for exact-profile legacy parsing, legacy tunnel termination, live-owner takeover, stale PID-fingerprint rejection, and owner release.
 - [x] **Step 2: Run `sh scripts/test-main-runtime-owner.sh`** and verify RED because the helper does not exist.
 - [x] **Step 3: Implement the minimal helper** using atomic `mkdir`, PID/start metadata, guarded stale-record removal, and bounded waits.
 - [x] **Step 4: Run `sh scripts/test-main-runtime-owner.sh`** and verify PASS.

 ### Task 2: Integrate ownership into the launcher lifecycle

 **Files:**
 - Modify: `scripts/run-main-runtime.sh`
 - Test: `scripts/test-main-runtime-owner.sh`

 **Interfaces:**
 - Consumes: Task 1 helper functions.
 - Produces: single-owner takeover before incumbent daemon retirement and signal-safe owner release.

 - [x] **Step 1: Add a failing static integration assertion** requiring the launcher to source the owner helper, acquire before `stop_daemon`, and use separate EXIT vs INT/TERM traps.
 - [x] **Step 2: Run the focused shell test** and verify RED.
 - [x] **Step 3: Integrate helper:** resolve default `/tmp/shellbeam-main-runtime-<uid>.owner`, acquire ownership after build and before daemon retirement, run the exact-profile legacy scan as a post-acquisition safety net, release owner last in cleanup, and make INT/TERM call cleanup then exit.
 - [x] **Step 4: Run focused test, `sh -n` on both scripts, and repository checks** without launching or stopping the live runtime.
 - [x] **Step 5: Inspect process state** and verify exactly one live tunnel/MCP stack remains from the earlier cleanup.
