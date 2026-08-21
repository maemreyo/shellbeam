#!/bin/sh
set -eu

ROOT=$(CDPATH="" cd -- "$(dirname -- "$0")/.." && pwd)
HELPER="$ROOT/scripts/lib/main-runtime-owner.sh"
LAUNCHER="$ROOT/scripts/run-main-runtime.sh"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
pass() { printf 'PASS: %s\n' "$*" >&2; }
assert_eq() { [ "$1" = "$2" ] || fail "$3: got [$1], want [$2]"; }

[ -f "$HELPER" ] || fail "missing helper: $HELPER"

TMP=$(mktemp -d "${TMPDIR:-/tmp}/shellbeam-owner-test.XXXXXX")
children=""
cleanup_test() {
  for pid in $children; do kill -KILL "$pid" 2>/dev/null || true; done
  rm -rf "$TMP"
}
trap cleanup_test EXIT INT TERM

log() { :; }
die() { printf 'die: %s\n' "$*" >&2; return 97; }
TUNNEL_PROFILE=shellbeam
STOP_TIMEOUT_SECONDS=3
RUNTIME_OWNER_DIR="$TMP/owner"
export TUNNEL_PROFILE STOP_TIMEOUT_SECONDS RUNTIME_OWNER_DIR
# shellcheck source=lib/main-runtime-owner.sh
. "$HELPER"

# Exact parser: only the canonical profile command is eligible for legacy retirement.
parsed=$(cat <<'PS' | legacy_tunnel_pids_from_ps
  101 tunnel-client run --profile shellbeam
  102 /opt/homebrew/bin/tunnel-client run --profile shellbeam
  103 tunnel-client run --profile other
  104 tunnel-client doctor --profile shellbeam
  105 tunnel-client run --profile shellbeam --extra nope
PS
)
assert_eq "$parsed" "101
102" "exact legacy tunnel parser"
pass "legacy parser is exact-profile only"

# Retirement: override discovery with a real disposable process, then require it to exit.
sleep 60 & legacy_pid=$!; children="$children $legacy_pid"
legacy_tunnel_pids() { printf '%s\n' "$legacy_pid"; }
retire_legacy_tunnels
if kill -0 "$legacy_pid" 2>/dev/null; then fail "legacy tunnel survived retirement"; fi
pass "legacy tunnel is retired"

# Restore real discovery function after the test override.
unset -f legacy_tunnel_pids 2>/dev/null || true
legacy_tunnel_pids() { ps -ax -o pid= -o command= | legacy_tunnel_pids_from_ps; }

# Live owner takeover: incumbent removes its own owner dir on TERM; acquisition must wait and win.
mkdir "$RUNTIME_OWNER_DIR"
(
  trap 'rm -f "$RUNTIME_OWNER_DIR/launcher.pid" "$RUNTIME_OWNER_DIR/launcher.started" "$RUNTIME_OWNER_DIR/profile" "$RUNTIME_OWNER_DIR/ready"; rmdir "$RUNTIME_OWNER_DIR" 2>/dev/null || true; exit 0' TERM INT
  while :; do sleep 1; done
) & incumbent=$!; children="$children $incumbent"
printf '%s\n' "$incumbent" > "$RUNTIME_OWNER_DIR/launcher.pid"
runtime_process_started "$incumbent" > "$RUNTIME_OWNER_DIR/launcher.started"
printf '%s\n' "$TUNNEL_PROFILE" > "$RUNTIME_OWNER_DIR/profile"
: > "$RUNTIME_OWNER_DIR/ready"
acquire_runtime_owner
assert_eq "$(cat "$RUNTIME_OWNER_DIR/launcher.pid")" "$$" "takeover owner pid"
if kill -0 "$incumbent" 2>/dev/null; then fail "incumbent launcher survived takeover"; fi
pass "live incumbent launcher is gracefully taken over"
release_runtime_owner
[ ! -e "$RUNTIME_OWNER_DIR" ] || fail "owner dir remained after release"
pass "owner release removes only owned record"

# PID reuse defense: a mismatched start fingerprint must be treated as stale, never signalled.
mkdir "$RUNTIME_OWNER_DIR"
printf '%s\n' "$$" > "$RUNTIME_OWNER_DIR/launcher.pid"
printf '%s\n' "not-the-current-process-start" > "$RUNTIME_OWNER_DIR/launcher.started"
printf '%s\n' "$TUNNEL_PROFILE" > "$RUNTIME_OWNER_DIR/profile"
: > "$RUNTIME_OWNER_DIR/ready"
acquire_runtime_owner
assert_eq "$(cat "$RUNTIME_OWNER_DIR/launcher.pid")" "$$" "stale owner replacement"
release_runtime_owner
pass "PID reuse fingerprint mismatch is reclaimed without signalling"

# Concurrent replacement defense: if metadata changes after inspection, the
# incumbent-retire path must retry rather than bubble status 1 into launcher set -e.
mkdir "$RUNTIME_OWNER_DIR"
printf '%s\n' "12345" > "$RUNTIME_OWNER_DIR/launcher.pid"
printf '%s\n' "old-start" > "$RUNTIME_OWNER_DIR/launcher.started"
printf '%s\n' "$TUNNEL_PROFILE" > "$RUNTIME_OWNER_DIR/profile"
: > "$RUNTIME_OWNER_DIR/ready"
runtime_process_is_same() { return 1; }
remove_owner_record_if_unchanged() { return 1; }
if ! retire_recorded_owner; then
  fail "owner metadata race escaped as a fatal status"
fi
rm -rf "$RUNTIME_OWNER_DIR"
. "$HELPER"
pass "owner metadata replacement is retried instead of aborting"

# Static launcher integration contract.
grep -q '^\. .*scripts/lib/main-runtime-owner\.sh' "$LAUNCHER" || fail "launcher does not source owner helper"
acquire_line=$(grep -n '^[[:space:]]*acquire_runtime_owner$' "$LAUNCHER" | head -1 | cut -d: -f1)
stop_line=$(grep -n '^[[:space:]]*stop_daemon$' "$LAUNCHER" | head -1 | cut -d: -f1)
[ -n "$acquire_line" ] && [ -n "$stop_line" ] && [ "$acquire_line" -lt "$stop_line" ] || fail "launcher must acquire stack owner before stop_daemon"
grep -q '^trap cleanup EXIT$' "$LAUNCHER" || fail "launcher EXIT trap missing"
grep -q '^trap handle_signal INT TERM$' "$LAUNCHER" || fail "launcher INT/TERM trap missing"
pass "launcher integrates owner lifecycle before daemon retirement"

printf 'all main-runtime owner tests passed\n'
