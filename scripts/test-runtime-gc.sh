#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
SUBJECT="$ROOT/scripts/runtime-gc.sh"

fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
pass() { printf 'PASS: %s\n' "$*" >&2; }

[ -f "$SUBJECT" ] || fail "missing subject: $SUBJECT"

# This tool deletes, so the properties worth pinning are the exclusions. The run
# is --dry-run against the real machine rather than a fixture, because the
# exclusions are defined by live state -- the runtime directory in use, the
# launcher owner record, worktrees git still has registered, paths a running
# process names -- and a fixture would have to fake exactly the thing under test.
plan=$(sh "$SUBJECT" --dry-run 2>&1) || fail "dry run failed: $plan"

printf '%s\n' "$plan" | grep -q 'free before:' || fail "dry run reported no starting free space"
printf '%s\n' "$plan" | grep -q 'free after:' || fail "dry run reported no ending free space"

# A dry run must propose work without doing any of it.
runtime_dir="/tmp/shellbeam-$(id -u)"
if [ -d "$runtime_dir" ]; then
  printf '%s\n' "$plan" | grep -Fq "would remove: $runtime_dir" &&
    fail "dry run proposed removing the live runtime directory"
  [ -d "$runtime_dir" ] || fail "dry run removed the live runtime directory"
fi
pass "the live runtime directory is never proposed for removal"

owner_dir="/tmp/shellbeam-main-runtime-$(id -u).owner"
if [ -d "$owner_dir" ]; then
  printf '%s\n' "$plan" | grep -Fq "would remove: $owner_dir" &&
    fail "dry run proposed removing the launcher owner record"
  [ -d "$owner_dir" ] || fail "dry run removed the launcher owner record"
fi
pass "the launcher owner record is never proposed for removal"

# Every worktree git still has registered under the tmp root must survive. These
# hold real branches; collecting one would destroy work, not reclaim garbage.
git -C "$ROOT" worktree list --porcelain 2>/dev/null |
  awk '/^worktree /{print substr($0, 10)}' |
  while IFS= read -r wt; do
    case "$wt" in
    /private/tmp/* | /tmp/*) ;;
    *) continue ;;
    esac
    plain=${wt#/private}
    if printf '%s\n' "$plan" | grep -Fq "would remove: $plain"; then
      fail "dry run proposed removing a registered worktree: $wt"
    fi
    [ -d "$wt" ] || fail "dry run removed a registered worktree: $wt"
  done
pass "registered worktrees under the tmp root are never proposed for removal"

# The cache is expensive to rebuild, so it is collected on a threshold rather
# than on every invocation. Either decision is valid; silence is not.
printf '%s\n' "$plan" |
  grep -Eq 'Go build cache .* is within .*; keeping it|would collect Go build cache' ||
  fail "dry run made no statement about the Go build cache: [$plan]"
pass "the Go build cache decision is always stated"

printf 'all runtime gc tests passed\n'
