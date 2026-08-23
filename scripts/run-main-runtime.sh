#!/bin/sh
# Bring the ShellBeam runtime up on exactly what is merged on origin/main.
#
# This exists because a hand-rolled `shellbeam daemon & ; sleep 1 ; doctor` loop
# fails in six ways that are all invisible until MCP refuses a call:
#
#   1. It never syncs or rebuilds, so the daemon serves whatever binary happened
#      to be on disk. That is how `binary_identity_mismatch` happens.
#   2. A local `main` branch is not "what is merged on remote". A local main can
#      sit *ahead* of origin/main after a local fast-forward onto a feature
#      branch, and then `merge --ff-only origin/main` reports "Already up to
#      date" and changes nothing -- silently building unmerged commits. So the
#      runtime worktree is checked out DETACHED at origin/main. There is no
#      local branch to drift, which makes the property true by construction
#      rather than by convention.
#   3. Plain `doctor` reports an unresponsive daemon as a warning with exit code
#      0, so it cannot gate anything. --require-ready turns that into a failure.
#      See the comment on requireReadyFlag in cmd/shellbeam/doctor.go.
#   4. `sleep 1` races startup recovery. The socket is published before the
#      daemon is serving, so readiness must be polled against a wall-clock
#      deadline, not slept through.
#   5. Nothing owned the daemon's lifetime, so quitting the tunnel left an
#      orphan daemon holding the ownership lease and pinning an old binary.
#      Here one trap owns both children.
#   6. Re-running this script could replace only the daemon while an older
#      launcher+tunnel+MCP stack kept polling the same profile. Requests then
#      alternated between old and new MCP binaries. A machine-global launcher
#      owner now makes takeover retire the whole incumbent stack first.
#
# Daemon ownership is still decided by the daemon lifetime lease, never by a
# command-line match. Launcher-stack ownership is a separate machine-global
# record under /tmp, keyed to this user and guarded by PID + process-start
# fingerprint so PID reuse can never authorize a signal. Exact command matching
# exists only as a migration path for pre-owner-record tunnel processes.
#
# Usage:
#   scripts/run-main-runtime.sh
#
# Environment:
#   MAIN_WT                 runtime worktree path (created if absent)
#   ENV_FILE                0600 file exporting CONTROL_PLANE_API_KEY (+ optional
#                           SHELLBEAM_TUNNEL_ID); see shellbeam-tunnel.env.example
#   TUNNEL_PROFILE          tunnel-client profile name (default: shellbeam)
#   TUNNEL_PROFILE_FILE     profile path (default: ~/.config/tunnel-client/<profile>.yaml)
#   READY_TIMEOUT_SECONDS   how long to wait for the daemon to serve (default: 90)
#   STOP_TIMEOUT_SECONDS    how long to wait for the old stack to release (default: 15)
#   RUNTIME_OWNER_DIR       machine-global launcher owner directory
#                           (default: /tmp/shellbeam-main-runtime-<uid>.owner)
#   SYNC_WATCH_SECONDS      poll origin/main every N seconds while running
#                           (default: 60; set 0 to disable)
#   SYNC_ON_CHANGE          notify | restart (default: restart)
#   SKIP_TUNNEL             set to 1 to sync, build and serve without starting
#                           the tunnel, and exit once the daemon is proven ready.
#                           Needs no credentials, so it is the way to check that
#                           origin/main actually builds and serves. Disruptive:
#                           it retires the incumbent daemon and then stops the
#                           one it proved, so nothing is serving afterwards. Do
#                           not run it against a live session.
set -eu

# Never let git block on an interactive credential prompt. A hung fetch inside a
# startup script is indistinguishable from a hung daemon, and this repo's remote
# is HTTPS.
export GIT_TERMINAL_PROMPT=0

MAIN_WT=${MAIN_WT:-$HOME/Documents/zaob-dev/shellbeam-worktrees/main-runtime}
ENV_FILE=${ENV_FILE:-$HOME/.shellbeam-tunnel.env}
TUNNEL_PROFILE=${TUNNEL_PROFILE:-shellbeam}
TUNNEL_PROFILE_FILE=${TUNNEL_PROFILE_FILE:-$HOME/.config/tunnel-client/$TUNNEL_PROFILE.yaml}
# Measured: ~15s to become ready on a 282MB store, because the socket is
# published before startup reconciliation finishes. The budget is generous
# because overrunning it aborts a healthy startup, while a crashed daemon is
# still caught immediately by the liveness check inside wait_ready.
READY_TIMEOUT_SECONDS=${READY_TIMEOUT_SECONDS:-90}
STOP_TIMEOUT_SECONDS=${STOP_TIMEOUT_SECONDS:-15}
SYNC_WATCH_SECONDS=${SYNC_WATCH_SECONDS:-60}
SYNC_ON_CHANGE=${SYNC_ON_CHANGE:-restart}
SKIP_TUNNEL=${SKIP_TUNNEL:-}
RUNTIME_OWNER_DIR=${RUNTIME_OWNER_DIR:-/tmp/shellbeam-main-runtime-$(id -u).owner}

INVOKE_REPO=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

# A launcher checkout is not authoritative. Once this bootstrap contract exists,
# even a stale topic worktree must fetch origin/main and exec the launcher and
# helpers materialized from that exact commit. Known pre-bootstrap worktrees are
# deliberately removed during rollout because no future code can retrofit a
# guard into an already-old file.
if [ -z "${SHELLBEAM_MAIN_RUNTIME_BOOTSTRAPPED:-}" ]; then
	[ -f "$INVOKE_REPO/scripts/lib/main-runtime-bootstrap.sh" ] || {
		printf 'error: runtime bootstrap helper missing: %s\n' "$INVOKE_REPO/scripts/lib/main-runtime-bootstrap.sh" >&2
		exit 1
	}
	# shellcheck source=lib/main-runtime-bootstrap.sh
	. "$INVOKE_REPO/scripts/lib/main-runtime-bootstrap.sh"
	main_runtime_bootstrap "$INVOKE_REPO"
	exit $?
fi

SRC_REPO=${SHELLBEAM_SOURCE_REPO:?missing SHELLBEAM_SOURCE_REPO from runtime bootstrap}
TARGET_MAIN=${SHELLBEAM_TARGET_MAIN:?missing SHELLBEAM_TARGET_MAIN from runtime bootstrap}
LAUNCHER_LIB_DIR=${SHELLBEAM_LAUNCHER_LIB_DIR:?missing SHELLBEAM_LAUNCHER_LIB_DIR from runtime bootstrap}
BIN="$MAIN_WT/shellbeam"

log() { printf '%s %s\n' "[$(date +%H:%M:%S)]" "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ -f "$LAUNCHER_LIB_DIR/main-runtime-bootstrap.sh" ] ||
	die "runtime bootstrap helper missing: $LAUNCHER_LIB_DIR/main-runtime-bootstrap.sh"
[ -f "$LAUNCHER_LIB_DIR/main-runtime-owner.sh" ] ||
	die "runtime ownership helper missing: $LAUNCHER_LIB_DIR/main-runtime-owner.sh"
# shellcheck source=lib/main-runtime-bootstrap.sh
. "$LAUNCHER_LIB_DIR/main-runtime-bootstrap.sh"
# shellcheck source=lib/main-runtime-owner.sh
. "$LAUNCHER_LIB_DIR/main-runtime-owner.sh"

# The materialized bootstrap directory is owned from the moment it exists, not
# from the moment the richer cleanup handler is installed further down. Every
# preflight refusal happens between those two points -- a missing Go toolchain
# is exactly that -- and each one used to leave a copy of the launcher and its
# helpers behind in TMPDIR with nothing that would ever collect it. This handler
# is deliberately narrow: at this point no child has been started and no
# ownership has been taken, so the directory is the only thing there is to
# release. `trap cleanup EXIT` replaces it once there is more to undo.
early_cleanup() {
	[ -n "${SHELLBEAM_LAUNCHER_TMP_DIR:-}" ] || return 0
	rm -rf "$SHELLBEAM_LAUNCHER_TMP_DIR" 2>/dev/null || true
}
trap early_cleanup EXIT INT TERM

# ---------------------------------------------------------------- preflight ---

case "$SYNC_ON_CHANGE" in
notify | restart) ;;
*) die "SYNC_ON_CHANGE must be 'notify' or 'restart', got '$SYNC_ON_CHANGE'" ;;
esac

log "syncing canonical main to origin/main ($(printf '%.12s' "$TARGET_MAIN"))"
main_runtime_sync_source_main "$SRC_REPO" "$TARGET_MAIN" ||
	die "canonical main is not safely fast-forwardable to origin/main"

# Detaching the worktree we are reading this script from can rewrite the script
# mid-execution, because sh reads a script incrementally rather than all at once.
# Run it from a different checkout instead of guessing whether this file changed.
[ "$SRC_REPO" != "$MAIN_WT" ] ||
	die "refusing to run from the runtime worktree it manages ($MAIN_WT);
  invoke it from a different checkout of this repository, or point MAIN_WT elsewhere"

command -v git >/dev/null 2>&1 || die "git not found on PATH"

# The Go toolchain is checked here rather than discovered at `make build`, and
# the reason is a failure this cost a live runtime to find. A launcher armed
# from a detached context -- a controller, a scheduler, anything that does not
# inherit an interactive profile -- gets sh's built-in PATH, which on darwin
# omits /opt/homebrew/bin. `go` is not found, check-json-mode.sh exits 127, and
# make reports `Error 127`, which this script then reported as the generic
# "build failed". Meanwhile the incumbent stack had already been retired by
# whatever asked for the restart, so nothing came back and the only symptom
# visible from outside was an MCP connector that never reconnected.
#
# Refusing here instead names the cause, and does it before anything has been
# torn down. PATH is quoted into the message because "go not found" is not
# actionable on a machine where go is plainly installed -- the PATH the
# launcher actually received is the thing the operator needs to see.
for tool in go make; do
	command -v "$tool" >/dev/null 2>&1 ||
		die "$tool not found on PATH; the runtime worktree cannot be built
  PATH=$PATH
  a launcher armed outside an interactive shell inherits sh's default PATH,
  which omits /opt/homebrew/bin; export a PATH that resolves the Go toolchain"
done

# Everything below is needed only to run the tunnel, so SKIP_TUNNEL can validate
# the sync/build/serve path on a machine that holds no credentials at all.
if [ -z "$SKIP_TUNNEL" ]; then
	command -v tunnel-client >/dev/null 2>&1 ||
		die "tunnel-client not found on PATH; install the OpenAI Secure MCP Tunnel client"
	command -v curl >/dev/null 2>&1 || die "curl not found on PATH"

	[ -f "$ENV_FILE" ] ||
		die "env file missing: $ENV_FILE
  copy $SRC_REPO/scripts/shellbeam-tunnel.env.example there, fill it in, then: chmod 600 $ENV_FILE"

	# A credential file readable by group or other is a credential leak, so this
	# is a refusal rather than a warning. stat(1) is BSD-flavoured here; this
	# script targets the darwin dev loop.
	env_mode=$(stat -f '%OLp' "$ENV_FILE" 2>/dev/null || echo '')
	[ "$env_mode" = "600" ] ||
		die "env file $ENV_FILE has mode ${env_mode:-unknown}, want 600; run: chmod 600 $ENV_FILE"

	[ -f "$TUNNEL_PROFILE_FILE" ] || die "tunnel profile not found: $TUNNEL_PROFILE_FILE"

	# Credentials are loaded and validated here rather than at the point of use.
	# The tunnel starts only after the incumbent daemon has been retired and the
	# binary rebuilt, so a key checked there would tear down a working runtime
	# before discovering it cannot finish. The key reaches tunnel-client through
	# the environment, never through a command line, so it stays out of `ps`.
	# shellcheck source=/dev/null
	. "$ENV_FILE"
	[ -n "${CONTROL_PLANE_API_KEY:-}" ] ||
		die "CONTROL_PLANE_API_KEY is empty in $ENV_FILE; fill it in before starting the runtime"
	export CONTROL_PLANE_API_KEY
fi

# ------------------------------------------------------------------ helpers ---

# remote_head prints the sha origin/main currently points at, without fetching
# objects. It is also the change detector for the watch loop, so a transient
# network failure must not read as "no change" -- callers distinguish empty.
remote_head() {
	git -C "$SRC_REPO" ls-remote origin refs/heads/main 2>/dev/null | awk 'NR==1 {print $1}'
}

worktree_dirty() {
	[ -n "$(git -C "$MAIN_WT" status --porcelain 2>/dev/null)" ]
}

# daemon_owner_pid prints the pid of the daemon holding the runtime lease, or
# nothing. It reads the lease through `doctor` rather than the lease file so that
# a config file overriding runtime_dir is honoured, instead of this script
# duplicating the path resolution in internal/config/paths.go.
daemon_owner_pid() {
	[ -x "$BIN" ] || return 0
	"$BIN" doctor --json 2>/dev/null |
		tr ',' '\n' |
		sed -n 's/.*"hint":"pid=\([0-9][0-9]*\) .*/\1/p' |
		awk 'NR==1 {print}'
}

# stop_daemon retires the incumbent daemon and waits for it to release the lease.
#
# It never unlinks the socket to make room. Unix keeps a listener alive after its
# pathname is unlinked, so removing the socket does not stop a daemon -- it only
# hides it, and lets a second daemon bind the same path.
stop_daemon() {
	pid=$(daemon_owner_pid)
	if [ -z "$pid" ]; then
		log "no daemon owns the runtime directory"
		return 0
	fi
	log "stopping incumbent daemon (pid $pid)"
	kill -TERM "$pid" 2>/dev/null || true
	deadline=$(($(date +%s) + STOP_TIMEOUT_SECONDS))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		kill -0 "$pid" 2>/dev/null || {
			log "incumbent daemon exited"
			return 0
		}
		sleep 0.2
	done
	die "daemon pid $pid did not exit within ${STOP_TIMEOUT_SECONDS}s; stop it yourself before retrying"
}

# wait_ready blocks until the daemon is *serving*, not merely until a socket
# exists. The deadline is wall-clock rather than an iteration count: each probe
# can itself block on the socket timeout, so counting iterations would stretch a
# "30s" budget several times longer exactly when the daemon is unhealthy.
wait_ready() {
	deadline=$(($(date +%s) + READY_TIMEOUT_SECONDS))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		if ! kill -0 "$daemon_pid" 2>/dev/null; then
			log "daemon exited during startup; last output:"
			tail -n 20 "$daemon_log" >&2 || true
			die "daemon failed to start"
		fi
		if "$BIN" doctor --json --require-ready >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.1
	done
	log "daemon did not become ready within ${READY_TIMEOUT_SECONDS}s; last output:"
	tail -n 20 "$daemon_log" >&2 || true
	die "daemon not ready"
}

# ------------------------------------------------------------------ cleanup ---

daemon_pid=""
tunnel_pid=""
daemon_log=""
cleaned=""

# EXIT cleanup is idempotent. INT/TERM must additionally exit after cleanup so
# a takeover signal cannot return to supervise and accidentally keep the old
# launcher alive. Ownership is released last, only after tunnel and daemon have
# been reaped, so a successor cannot start while old children are still live.
cleanup() {
	[ -n "$cleaned" ] && return 0
	cleaned=1
	if [ -n "$tunnel_pid" ]; then
		kill "$tunnel_pid" 2>/dev/null || true
		wait "$tunnel_pid" 2>/dev/null || true
		tunnel_pid=""
	fi
	if [ -n "$daemon_pid" ]; then
		kill "$daemon_pid" 2>/dev/null || true
		wait "$daemon_pid" 2>/dev/null || true
		daemon_pid=""
	fi
	release_runtime_owner
	if [ -n "${SHELLBEAM_LAUNCHER_TMP_DIR:-}" ]; then
		rm -rf "$SHELLBEAM_LAUNCHER_TMP_DIR" 2>/dev/null || true
	fi
}
handle_signal() {
	cleanup
	exit 0
}
trap cleanup EXIT
trap handle_signal INT TERM

# --------------------------------------------------------------------- sync ---

ensure_worktree() {
	if [ -d "$MAIN_WT/.git" ] || [ -f "$MAIN_WT/.git" ]; then
		return 0
	fi
	[ ! -e "$MAIN_WT" ] || die "$MAIN_WT exists but is not a git worktree"
	log "creating runtime worktree at $MAIN_WT"
	git -C "$SRC_REPO" worktree add --detach "$MAIN_WT" >&2
}

# sync_to_remote_main puts the runtime worktree on the exact commit origin/main
# points at. Detached, so there is no local branch that can quietly get ahead.
sync_to_remote_main() {
	target=$TARGET_MAIN

	# This worktree is never authored in, so anything uncommitted here is a
	# surprise worth stopping for rather than discarding.
	if worktree_dirty; then
		git -C "$MAIN_WT" status --short >&2
		die "runtime worktree $MAIN_WT is dirty; it is not meant to be edited -- resolve the files above"
	fi

	current=$(git -C "$MAIN_WT" rev-parse HEAD 2>/dev/null || echo '')
	if [ "$current" = "$target" ]; then
		log "already at origin/main ($(printf '%.12s' "$target"))"
		return 0
	fi
	log "checking out origin/main ($(printf '%.12s' "$target"))"
	git -C "$MAIN_WT" checkout --detach --quiet "$target" ||
		die "checkout of $target failed"
}

# --------------------------------------------------------- profile assertion ---

# The profile is the user's own config, so this verifies and reports rather than
# rewriting it. A profile pointing at another worktree's binary is precisely the
# stale-binary bug this script exists to prevent, one level up.
assert_profile() {
	want="$BIN mcp"
	if ! grep -q -- "$want" "$TUNNEL_PROFILE_FILE"; then
		have=$(sed -n 's/.*command: *"\(.*\)".*/\1/p' "$TUNNEL_PROFILE_FILE" | awk 'NR==1 {print}')
		die "tunnel profile points at the wrong binary
  profile: $TUNNEL_PROFILE_FILE
  found:   ${have:-<no mcp command>}
  want:    $want
  edit mcp.commands[0].command in that file to the 'want' value"
	fi

	# SHELLBEAM_TUNNEL_ID is optional, and when set it guards against running
	# against a tunnel bound to a different local MCP server -- which surfaces
	# as channel=main contention rather than as a clear error.
	if [ -n "${SHELLBEAM_TUNNEL_ID:-}" ] &&
		! grep -q -- "$SHELLBEAM_TUNNEL_ID" "$TUNNEL_PROFILE_FILE"; then
		die "SHELLBEAM_TUNNEL_ID from $ENV_FILE is not the tunnel_id in $TUNNEL_PROFILE_FILE"
	fi
}

# ---------------------------------------------------------------- readiness ---

health_url_file() {
	f=$(sed -n 's/^[[:space:]]*url_file:[[:space:]]*"\(.*\)".*/\1/p' "$TUNNEL_PROFILE_FILE" | awk 'NR==1 {print}')
	printf '%s' "${f:-/tmp/tunnel-client-$TUNNEL_PROFILE-health.url}"
}

# await_tunnel_health waits for the tunnel to publish its own health URL, then
# probes it. The url_file is written by tunnel-client at startup, so reading it
# *before* starting the tunnel yields the previous run's port -- which is how a
# readyz probe passes against a tunnel that is not running.
await_tunnel_health() {
	url_file=$(health_url_file)
	deadline=$(($(date +%s) + READY_TIMEOUT_SECONDS))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		if ! kill -0 "$tunnel_pid" 2>/dev/null; then
			die "tunnel exited during startup"
		fi
		if [ -s "$url_file" ] && curl -fsS "$(cat "$url_file")/readyz" >/dev/null 2>&1; then
			log "tunnel ready at $(cat "$url_file")"
			return 0
		fi
		sleep 0.2
	done
	die "tunnel did not become ready within ${READY_TIMEOUT_SECONDS}s"
}

# -------------------------------------------------------------------- cycle ---

restart_requested=""

run_cycle() {
	cleaned=""
	restart_requested=""

	ensure_worktree
	sync_to_remote_main
	[ -n "$SKIP_TUNNEL" ] || assert_profile

	# Build before retiring the incumbent. `go build` writes a new inode and
	# renames, so the running daemon is unaffected -- which means a build failure
	# leaves the old daemon still serving instead of leaving nothing running, and
	# the downtime is only the restart rather than the whole build.
	log "building"
	make -C "$MAIN_WT" build >&2 || die "build failed"
	built_sha=$(shasum -a 256 "$BIN" | awk '{print $1}')

	# Build first so a failure leaves the incumbent stack untouched. Then acquire
	# machine-global launcher ownership before retiring anything. The exact-profile
	# scan is the migration/safety-net path for launchers predating owner records.
	acquire_runtime_owner
	retire_legacy_tunnels
	stop_daemon

	mkdir -p "$MAIN_WT/.build/run"
	daemon_log="$MAIN_WT/.build/run/daemon.log"
	log "starting daemon"
	"$BIN" daemon >"$daemon_log" 2>&1 &
	daemon_pid=$!

	wait_ready

	# The identity assertion this whole script exists for. The daemon hashes its
	# own executable at startup, so proving that our child holds the lease and
	# that its binary is still the one we just built is what makes the MCP-side
	# binary_identity_mismatch unreachable.
	owner=$(daemon_owner_pid)
	[ "$owner" = "$daemon_pid" ] ||
		die "another daemon won the lease (owner pid ${owner:-none}, ours $daemon_pid)"
	now_sha=$(shasum -a 256 "$BIN" | awk '{print $1}')
	[ "$now_sha" = "$built_sha" ] ||
		die "binary changed after the daemon started; something rebuilt $BIN behind this script"
	log "daemon ready: pid $daemon_pid, binary $(printf '%.12s' "$built_sha"), main $(printf '%.12s' "$(git -C "$MAIN_WT" rev-parse HEAD)")"

	if [ -n "$SKIP_TUNNEL" ]; then
		log "SKIP_TUNNEL set: sync, build and serve verified; stopping the daemon again"
		return 0
	fi

	# CONTROL_PLANE_API_KEY was loaded and validated during preflight, before
	# anything was torn down.
	tunnel-client doctor --profile "$TUNNEL_PROFILE" >&2 || die "tunnel-client doctor failed"

	# Remove the stale URL first so await_tunnel_health cannot pass against the
	# port a previous run happened to bind.
	rm -f "$(health_url_file)"

	log "starting tunnel (profile $TUNNEL_PROFILE)"
	tunnel-client run --profile "$TUNNEL_PROFILE" &
	tunnel_pid=$!

	await_tunnel_health

	if [ "$SYNC_WATCH_SECONDS" -gt 0 ]; then
		log "watching origin/main every ${SYNC_WATCH_SECONDS}s (on change: $SYNC_ON_CHANGE)"
	fi
	log "runtime is up; Ctrl-C stops the tunnel and the daemon together"

	supervise
}

# supervise holds the process open until the tunnel exits, optionally polling
# origin/main. Polling happens inline rather than in a background watcher so
# there is no second process to reap and no signal plumbing to get wrong.
supervise() {
	seen=$(git -C "$MAIN_WT" rev-parse HEAD)
	next_poll=$(($(date +%s) + SYNC_WATCH_SECONDS))
	while kill -0 "$tunnel_pid" 2>/dev/null; do
		sleep 1
		[ "$SYNC_WATCH_SECONDS" -gt 0 ] || continue
		[ "$(date +%s)" -ge "$next_poll" ] || continue
		next_poll=$(($(date +%s) + SYNC_WATCH_SECONDS))

		head=$(remote_head)
		# An empty result means the probe failed, not that nothing changed.
		# Treating it as "no change" is correct here; treating it as a change
		# would restart the runtime on a flaky network.
		[ -n "$head" ] || {
			log "warning: could not reach origin to check for updates"
			continue
		}
		[ "$head" != "$seen" ] || continue
		seen=$head

		if [ "$SYNC_ON_CHANGE" = restart ]; then
			log "origin/main moved to $(printf '%.12s' "$head"); restarting runtime"
			restart_requested=1
			return 0
		fi
		log "origin/main moved to $(printf '%.12s' "$head") -- rerun this script to pick it up"
	done

	# Clear the pid as part of reaping it. After `wait` returns, the pid is
	# eligible for reuse, so leaving the variable set would have the exit-time
	# cleanup signal whatever now holds that number.
	if wait "$tunnel_pid"; then
		tunnel_status=0
	else
		tunnel_status=$?
	fi
	tunnel_pid=""
	return "$tunnel_status"
}

# --------------------------------------------------------------------- main ---

while :; do
	run_cycle
	[ -n "$restart_requested" ] || break
	cleanup
	log "--- re-entering bootstrap for new origin/main ---"
	launcher_tmp=${SHELLBEAM_LAUNCHER_TMP_DIR:-}
	unset SHELLBEAM_MAIN_RUNTIME_BOOTSTRAPPED SHELLBEAM_SOURCE_REPO SHELLBEAM_TARGET_MAIN \
		SHELLBEAM_LAUNCHER_LIB_DIR SHELLBEAM_LAUNCHER_TMP_DIR
	[ -z "$launcher_tmp" ] || rm -rf "$launcher_tmp"
	exec sh "$SRC_REPO/scripts/run-main-runtime.sh"
done
