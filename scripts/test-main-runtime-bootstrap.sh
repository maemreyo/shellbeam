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

printf 'all main-runtime bootstrap tests passed\n'
