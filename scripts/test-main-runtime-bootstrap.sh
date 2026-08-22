#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
HELPER="$ROOT/scripts/lib/main-runtime-bootstrap.sh"
LAUNCHER="$ROOT/scripts/run-main-runtime.sh"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
pass() { printf 'PASS: %s\n' "$*" >&2; }
assert_eq() { [ "$1" = "$2" ] || fail "$3: got [$1], want [$2]"; }

[ -f "$HELPER" ] || fail "missing bootstrap helper: $HELPER"
# shellcheck source=lib/main-runtime-bootstrap.sh
. "$HELPER"

TMP=$(mktemp -d "${TMPDIR:-/tmp}/shellbeam-main-bootstrap-test.XXXXXX")
cleanup() { rm -rf "$TMP"; }
trap cleanup EXIT INT TERM

REMOTE="$TMP/remote.git"
MAIN="$TMP/main"
PUBLISHER="$TMP/publisher"
STALE="$TMP/stale"
git init --bare -q "$REMOTE"
git clone -q "$REMOTE" "$MAIN"
(
  cd "$MAIN"
  git config user.email test@example.com
  git config user.name test
  mkdir -p scripts/lib
  cat > scripts/run-main-runtime.sh <<'OLD'
#!/bin/sh
echo OLD_RUNNER_SHOULD_NOT_EXECUTE
exit 91
OLD
  printf '# old bootstrap placeholder\n' > scripts/lib/main-runtime-bootstrap.sh
  printf '# owner placeholder\n' > scripts/lib/main-runtime-owner.sh
  git add scripts
  git commit -qm initial
  git branch -M main
  git push -q -u origin main
  git worktree add -q -b stale "$STALE" HEAD
)

git clone -q "$REMOTE" "$PUBLISHER"
(
  cd "$PUBLISHER"
  git config user.email test@example.com
  git config user.name test
  git checkout -q main
  cat > scripts/lib/main-runtime-bootstrap.sh <<'LATEST_HELPER'
main_runtime_sync_source_main() {
  target=$1
  current=$(git -C "$SHELLBEAM_SOURCE_REPO" rev-parse HEAD)
  [ "$current" = "$target" ] || git -C "$SHELLBEAM_SOURCE_REPO" merge --ff-only -q "$target"
}
LATEST_HELPER
  cat > scripts/run-main-runtime.sh <<'LATEST'
#!/bin/sh
set -eu
. "$SHELLBEAM_LAUNCHER_LIB_DIR/main-runtime-bootstrap.sh"
main_runtime_sync_source_main "$SHELLBEAM_TARGET_MAIN"
printf 'LATEST:%s:%s\n' "$SHELLBEAM_TARGET_MAIN" "$(git -C "$SHELLBEAM_SOURCE_REPO" rev-parse HEAD)"
LATEST
  git add scripts
  git commit -qm latest
  git push -q origin main
)

target=$(git -C "$PUBLISHER" rev-parse HEAD)
output=$(main_runtime_bootstrap "$STALE" "$TMP/materialized")
assert_eq "$output" "LATEST:$target:$target" "stale checkout must execute latest launcher and sync canonical main"
assert_eq "$(git -C "$MAIN" rev-parse HEAD)" "$target" "canonical main fast-forward"
pass "stale checkout bootstraps latest origin/main launcher and canonical main"

grep -Fq 'SYNC_WATCH_SECONDS=${SYNC_WATCH_SECONDS:-60}' "$LAUNCHER" || fail "default origin/main watch is not 60s"
grep -Fq 'SYNC_ON_CHANGE=${SYNC_ON_CHANGE:-restart}' "$LAUNCHER" || fail "default origin/main change action is not restart"
pass "launcher defaults to watching and restarting on origin/main changes"

cleanup_block=$(awk '/^cleanup\(\) \{/{flag=1} flag{print} /^\}/{if(flag){exit}}' "$LAUNCHER")
printf '%s\n' "$cleanup_block" | grep -Fq 'SHELLBEAM_LAUNCHER_TMP_DIR' || fail "launcher cleanup does not remove materialized bootstrap directory"
pass "launcher cleanup owns materialized bootstrap directory lifecycle"

# --- the toolchain is refused before anything is torn down ----------------------
#
# Reproduction of a live failure: a launcher armed from a detached context gets
# sh's built-in PATH, which on darwin omits /opt/homebrew/bin. `go` was not
# found, check-json-mode.sh exited 127, make reported Error 127, and the
# launcher reported the generic "build failed" -- after whatever asked for the
# restart had already retired the incumbent stack, so nothing came back and the
# only outward symptom was an MCP connector that never reconnected.
#
# The launcher is executed for real here rather than grepped, because the
# property under test is that it *refuses*, and refuses early. Bootstrap is
# pre-satisfied through its documented environment contract so no network is
# needed, and MAIN_WT plus RUNTIME_OWNER_DIR are pointed into TMP so a
# regression that got further than it should cannot reach a live runtime.
CANON="$TMP/canonical"
git init -q "$CANON"
(
  cd "$CANON"
  git config user.email test@example.com
  git config user.name test
  mkdir -p scripts/lib
  cp "$ROOT/scripts/lib/main-runtime-bootstrap.sh" scripts/lib/
  cp "$ROOT/scripts/lib/main-runtime-owner.sh" scripts/lib/
  git add -A
  git commit -qm fixture
  git branch -M main
)
canon_head=$(git -C "$CANON" rev-parse HEAD)

# The PATH is built rather than hardcoded. Naming real directories encodes one
# machine's layout instead of the property under test: /usr/local/bin has no Go
# on darwin but does on a GitHub ubuntu runner, so a hardcoded list silently
# stops testing anything. This shim carries exactly what the launcher needs to
# reach the toolchain check -- dirname for INVOKE_REPO, date for log(), git for
# the canonical-main sync -- and nothing else, so go and make are provably
# absent everywhere.
SHIM="$TMP/shimbin"
mkdir -p "$SHIM"
for tool in sh dirname basename date git printf sed awk cat rm mkdir; do
  tool_path=$(command -v "$tool" 2>/dev/null) || continue
  ln -sf "$tool_path" "$SHIM/$tool"
done
for absent in go make; do
  if PATH="$SHIM" command -v "$absent" >/dev/null 2>&1; then
    fail "shim PATH still resolves $absent; the case would test nothing"
  fi
done
PATH="$SHIM" command -v git >/dev/null 2>&1 ||
  fail "shim PATH lost git, which preflight checks before the toolchain"

toolchain_status=0
toolchain_out=$(
  env -i \
    PATH="$SHIM" \
    HOME="$HOME" \
    SHELLBEAM_MAIN_RUNTIME_BOOTSTRAPPED=1 \
    SHELLBEAM_SOURCE_REPO="$CANON" \
    SHELLBEAM_TARGET_MAIN="$canon_head" \
    SHELLBEAM_LAUNCHER_LIB_DIR="$CANON/scripts/lib" \
    MAIN_WT="$TMP/toolchain-wt" \
    RUNTIME_OWNER_DIR="$TMP/toolchain-owner" \
    SKIP_TUNNEL=1 \
    /bin/sh "$LAUNCHER" 2>&1
) || toolchain_status=$?

[ "$toolchain_status" -ne 0 ] || fail "launcher accepted a PATH with no Go toolchain"
printf '%s\n' "$toolchain_out" | grep -Fq 'go not found on PATH' ||
  fail "refusal does not name the missing tool: [$toolchain_out]"
printf '%s\n' "$toolchain_out" | grep -Fq "PATH=$SHIM" ||
  fail "refusal does not quote the PATH the launcher received: [$toolchain_out]"
if printf '%s\n' "$toolchain_out" | grep -Fq 'building'; then
  fail "launcher reached the build stage before refusing: [$toolchain_out]"
fi
[ ! -e "$TMP/toolchain-owner" ] || fail "launcher took runtime ownership before refusing"
pass "a launcher without a Go toolchain refuses in preflight, before teardown"

# --- a refusal does not leak the materialized launcher -------------------------
#
# The bootstrap copies the launcher and its helpers into TMPDIR and execs them.
# The full cleanup handler is installed well after preflight, so before this was
# fixed every refusal above left one of those copies behind with nothing that
# would ever collect it. Re-run the same refusal with a materialized directory
# whose path is known, and require it to be gone afterwards.
LEAKDIR="$TMP/materialized-refusal"
mkdir -p "$LEAKDIR/scripts/lib"
cp "$LAUNCHER" "$LEAKDIR/scripts/run-main-runtime.sh"
cp "$ROOT/scripts/lib/main-runtime-bootstrap.sh" "$LEAKDIR/scripts/lib/"
cp "$ROOT/scripts/lib/main-runtime-owner.sh" "$LEAKDIR/scripts/lib/"
env -i \
  PATH="$SHIM" \
  HOME="$HOME" \
  SHELLBEAM_MAIN_RUNTIME_BOOTSTRAPPED=1 \
  SHELLBEAM_SOURCE_REPO="$CANON" \
  SHELLBEAM_TARGET_MAIN="$canon_head" \
  SHELLBEAM_LAUNCHER_LIB_DIR="$LEAKDIR/scripts/lib" \
  SHELLBEAM_LAUNCHER_TMP_DIR="$LEAKDIR" \
  MAIN_WT="$TMP/leak-wt" \
  RUNTIME_OWNER_DIR="$TMP/leak-owner" \
  SKIP_TUNNEL=1 \
  /bin/sh "$LEAKDIR/scripts/run-main-runtime.sh" >/dev/null 2>&1 || true
[ ! -e "$LEAKDIR" ] || fail "a preflight refusal left the materialized launcher behind: $LEAKDIR"
pass "a preflight refusal collects its own materialized launcher directory"

printf 'all main-runtime bootstrap tests passed\n'
