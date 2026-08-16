#!/bin/sh
# Start a ShellBeam daemon and an OpenAI Secure MCP Tunnel client against it,
# for manual acceptance runs.
#
# The gate this replaces created split brain three ways, and each fix below is
# load-bearing:
#
#   1. It looked for a running tunnel with `pgrep -f "tunnel-client run
#      --profile shellbeam"`, which never matched the real command line
#      (`--profile-file <path>`), so an existing stack was never detected.
#   2. It ran `rm -f "$RUNTIME/daemon.sock"` before starting. Unix keeps a
#      listener alive after its pathname is unlinked, so this did not stop the
#      old daemon -- it only hid it, and let a second daemon bind the same path.
#   3. It ended with `exec tunnel-client ...`, replacing the shell that owned
#      the cleanup trap, so the daemon it started outlived the thing meant to
#      stop it.
#
# Ownership is now decided by the daemon's lifetime lease, so this script does
# not need to guess. It starts the daemon and lets it refuse to run if the
# directories are already owned.
set -eu

usage() {
	echo "usage: $0 --binary <shellbeam> --profile <tunnel-profile.yaml> --state-dir <dir> --runtime-dir <dir>" >&2
	exit 2
}

BIN="" PROFILE="" STATE="" RUNTIME="" PIDFILE=""
while [ $# -gt 0 ]; do
	case "$1" in
	--binary) BIN="${2:-}"; shift 2 ;;
	--profile) PROFILE="${2:-}"; shift 2 ;;
	--state-dir) STATE="${2:-}"; shift 2 ;;
	--runtime-dir) RUNTIME="${2:-}"; shift 2 ;;
	--pid-file) PIDFILE="${2:-}"; shift 2 ;;
	*) usage ;;
	esac
done
[ -n "$BIN" ] && [ -n "$PROFILE" ] && [ -n "$STATE" ] && [ -n "$RUNTIME" ] || usage

mkdir -p "$STATE" "$RUNTIME"
chmod 700 "$STATE" "$RUNTIME"

# Report the current owners before doing anything. `doctor` reads the leases,
# so this names the process to stop rather than guessing from command lines.
"$BIN" doctor --state-dir "$STATE" --runtime-dir "$RUNTIME" --json || true

# Never remove a socket to make room. If a daemon still owns these directories
# the start below fails with daemon_already_running, which is the correct
# outcome: stop that daemon first.
daemon_log="$RUNTIME/daemon.log"
"$BIN" daemon --state-dir "$STATE" --runtime-dir "$RUNTIME" >"$daemon_log" 2>&1 &
daemon_pid=$!

# Cleanup runs once. The same handler is wired to EXIT as well as INT and TERM,
# so without the guard a signal-triggered run would be followed by the exit-time
# run, killing a pid that had already been reaped -- and possibly reused.
tunnel_pid=""
cleaned=""
cleanup() {
	[ -n "$cleaned" ] && return 0
	cleaned=1
	if [ -n "$tunnel_pid" ]; then
		kill "$tunnel_pid" 2>/dev/null || true
		wait "$tunnel_pid" 2>/dev/null || true
		tunnel_pid=""
	fi
	kill "$daemon_pid" 2>/dev/null || true
	wait "$daemon_pid" 2>/dev/null || true
	daemon_pid=""
}
trap cleanup EXIT INT TERM

# Wait for the daemon to be *serving*, not merely to have published a socket.
# The socket appears before startup recovery runs, and plain `doctor` reports an
# unresponsive daemon as a warning with exit code 0 -- so waiting on the socket
# and then running doctor would let the tunnel start against a daemon that is
# still reconciling. --require-ready turns that warning into a failure.
# The deadline is wall-clock, not an iteration count. Each readiness probe can
# itself block for up to the socket probe timeout, so counting iterations would
# have made a "30s" budget run several times longer exactly when the daemon is
# unhealthy and the operator is waiting.
READY_TIMEOUT_SECONDS=${READY_TIMEOUT_SECONDS:-30}
deadline=$(($(date +%s) + READY_TIMEOUT_SECONDS))
ready=""
while [ "$(date +%s)" -lt "$deadline" ]; do
	if ! kill -0 "$daemon_pid" 2>/dev/null; then
		echo "daemon exited during startup; last output:" >&2
		tail -n 20 "$daemon_log" >&2 || true
		exit 4
	fi
	if "$BIN" doctor --state-dir "$STATE" --runtime-dir "$RUNTIME" --json --require-ready >/dev/null 2>&1; then
		ready=1
		break
	fi
	sleep 0.1
done
if [ -z "$ready" ]; then
	echo "daemon did not become ready within ${READY_TIMEOUT_SECONDS}s; last output:" >&2
	tail -n 20 "$daemon_log" >&2 || true
	exit 4
fi

"$BIN" doctor --state-dir "$STATE" --runtime-dir "$RUNTIME" --json --require-ready
tunnel-client doctor --profile-file "$PROFILE"
echo "Tunnel is starting. Keep this process running while exercising the manual acceptance flow."

# Run the tunnel as a child rather than exec'ing over this shell, so the trap
# above stays the lifecycle owner of the daemon it started.
if [ -n "$PIDFILE" ]; then
	tunnel-client run --profile-file "$PROFILE" --pid.file "$PIDFILE" &
else
	tunnel-client run --profile-file "$PROFILE" &
fi
tunnel_pid=$!

# Clear the pid as part of reaping it. After `wait` returns the child has been
# reaped and its pid is eligible for reuse, so leaving the variable set would
# have the exit-time cleanup signal whatever now holds that number -- the same
# hazard the trap guard above exists to avoid, reached by the ordinary path.
if wait "$tunnel_pid"; then
	tunnel_status=0
else
	tunnel_status=$?
fi
tunnel_pid=""
exit "$tunnel_status"
