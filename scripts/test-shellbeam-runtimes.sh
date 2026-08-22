#!/bin/sh
set -eu

ROOT=$(CDPATH="" cd -- "$(dirname -- "$0")/.." && pwd)
SUBJECT="$ROOT/scripts/shellbeam-runtimes.sh"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
pass() { printf 'PASS: %s\n' "$*" >&2; }
assert_eq() { [ "$1" = "$2" ] || fail "$3: got [$1], want [$2]"; }

[ -f "$SUBJECT" ] || fail "missing subject: $SUBJECT"

TMP=$(mktemp -d "${TMPDIR:-/tmp}/shellbeam-runtimes-test.XXXXXX")
children=""
cleanup_test() {
	for pid in $children; do kill -KILL "$pid" 2>/dev/null || true; done
	rm -rf "$TMP"
}
trap cleanup_test EXIT INT TERM

SHELLBEAM_RUNTIMES_LIB_ONLY=1
export SHELLBEAM_RUNTIMES_LIB_ONLY
# shellcheck source=shellbeam-runtimes.sh
. "$SUBJECT"

# --- classification is by argv shape, never by searching for "shellbeam" -------
#
# The negatives matter more than the positives: anything misclassified here is
# something `kill` would signal.
assert_eq "$(classify_role /usr/local/bin/shellbeam daemon)" "daemon" "daemon by argv shape"
assert_eq "$(classify_role /tmp/x/shellbeam mcp)" "mcp" "mcp by argv shape"
assert_eq "$(classify_role tunnel-client run)" "tunnel" "tunnel by argv shape"
assert_eq "$(classify_role sh /repo/scripts/run-main-runtime.sh)" "launcher" "launcher via interpreter"
assert_eq "$(classify_role /repo/scripts/run-main-runtime.sh)" "launcher" "launcher invoked directly"
assert_eq "$(classify_role grep shellbeam)" "" "grep for the word is not a runtime"
assert_eq "$(classify_role /usr/local/bin/shellbeam doctor)" "" "non-runtime subcommand"
assert_eq "$(classify_role tunnel-client doctor)" "" "tunnel doctor is not the tunnel"
assert_eq "$(classify_role vim scripts/run-main-runtime.sh)" "" "editing the launcher is not running it"
pass "role classification is exact and rejects lookalikes"

# --- runtime dir precedence mirrors internal/config/load.go -------------------
printf 'schema_version = 1\nruntime_dir = "%s/from-config"\n' "$TMP" >"$TMP/config.toml"
assert_eq \
	"$(runtime_dir_from_argv shellbeam daemon --runtime-dir "$TMP/from-flag" --config "$TMP/config.toml")" \
	"$TMP/from-flag" "flag beats config"
assert_eq \
	"$(runtime_dir_from_argv shellbeam daemon --config "$TMP/config.toml")" \
	"$TMP/from-config" "config beats default"
assert_eq \
	"$(runtime_dir_from_argv shellbeam daemon --runtime-dir="$TMP/joined")" \
	"$TMP/joined" "joined --runtime-dir=form"
DEFAULT_CONFIG_FILE="$TMP/absent.toml"
assert_eq \
	"$(runtime_dir_from_argv shellbeam daemon)" \
	"$DEFAULT_RUNTIME_DIR" "default when neither is given"
pass "runtime directory precedence is flag over config over default"

# --- lease state ---------------------------------------------------------------
mkdir -p "$TMP/rt"
printf '{"schema_version":1,"pid":4242,"incarnation":"x"}\n' >"$TMP/rt/daemon.owner"
assert_eq "$(lease_pid "$TMP/rt")" "4242" "lease pid parsed"
assert_eq "$(lease_pid "$TMP/absent")" "" "missing lease reads empty"
pass "lease pid is read from daemon.owner"

# --- PID reuse defense ----------------------------------------------------------
#
# This is the whole reason identity comes from the owner helper. A live process
# whose recorded start fingerprint does not match must never be signalled, even
# though the pid is real and running.
sleep 60 & victim=$!; children="$children $victim"
signal_pid "$victim" "not-the-real-start-time" TERM "fixture"
sleep 0.3
kill -0 "$victim" 2>/dev/null || fail "mismatched fingerprint was signalled"
pass "a pid whose start fingerprint differs is never signalled"

real_started=$(runtime_process_started "$victim")
[ -n "$real_started" ] || fail "could not fingerprint fixture process"
signal_pid "$victim" "$real_started" TERM "fixture"
deadline=$(($(date +%s) + 5))
while [ "$(date +%s)" -lt "$deadline" ]; do
	case "$(runtime_process_state "$victim")" in
	"" | Z*) break ;;
	esac
	sleep 0.1
done
case "$(runtime_process_state "$victim")" in
"" | Z*) ;;
*) fail "matching fingerprint did not signal" ;;
esac
pass "a pid whose start fingerprint matches is signalled"

# --- wait_gone reports a set, and does not abort on timeout ---------------------
sleep 60 & holdout=$!; children="$children $holdout"
printf '%s\n' "$holdout" >"$TMP/pids"
if wait_gone "$TMP/pids" 1; then fail "wait_gone claimed a live set had exited"; fi
kill -KILL "$holdout" 2>/dev/null || true
if ! wait_gone "$TMP/pids" 5; then fail "wait_gone did not observe the set exit"; fi
pass "wait_gone times out without aborting and observes exit"

# --- stop ordering contract -----------------------------------------------------
#
# Launchers must be signalled before anything else: run-main-runtime.sh's
# supervise() watches the tunnel, not the daemon, so killing its daemon first
# leaves the tunnel and mcp alive in front of a dead socket.
launcher_line=$(grep -n '^[[:space:]]*signal_role launcher TERM$' "$SUBJECT" | head -1 | cut -d: -f1)
rest_line=$(grep -n '^[[:space:]]*signal_role rest TERM$' "$SUBJECT" | head -1 | cut -d: -f1)
[ -n "$launcher_line" ] && [ -n "$rest_line" ] && [ "$launcher_line" -lt "$rest_line" ] ||
	fail "launchers must be signalled before the rest"
grep -q '^\. "\$HELPER"$' "$SUBJECT" || fail "subject does not source the owner helper"
grep -q 'runtime_process_is_same' "$SUBJECT" || fail "subject does not use the helper identity guard"
pass "stop order signals launchers before the rest"

# --- the launcher's tunnel-watching premise still holds -------------------------
#
# If this ever fails, the ordering above is no longer justified and the header
# comment explaining it has gone stale.
grep -q 'while kill -0 "\$tunnel_pid"' "$ROOT/scripts/run-main-runtime.sh" ||
	fail "run-main-runtime.sh no longer supervises on the tunnel pid"
grep -q '^trap handle_signal INT TERM$' "$ROOT/scripts/run-main-runtime.sh" ||
	fail "run-main-runtime.sh no longer traps INT/TERM into cleanup"
pass "launcher premises this script depends on are unchanged"

printf 'all shellbeam runtimes tests passed\n'
