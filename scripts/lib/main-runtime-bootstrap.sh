#!/bin/sh
# Bootstrap helpers for scripts/run-main-runtime.sh.
#
# The runtime launcher may be invoked from any checkout that already contains
# this bootstrap contract. It must never trust that checkout's remaining files:
# fetch origin/main, materialize the launcher + helpers from that exact commit,
# then exec the materialized launcher. The materialized launcher is safe to
# fast-forward the canonical main worktree because its own script file lives in
# /tmp rather than in the worktree being updated.

main_runtime_find_main_worktree() {
    repo=$1
    git -C "$repo" worktree list --porcelain 2>/dev/null |
        awk '
            /^worktree / { worktree = substr($0, 10); next }
            /^branch refs\/heads\/main$/ { print worktree; exit }
        '
}

main_runtime_sync_source_main() {
    repo=$1
    target=$2

    branch=$(git -C "$repo" symbolic-ref --quiet HEAD 2>/dev/null || true)
    [ "$branch" = refs/heads/main ] || {
        printf 'error: canonical launcher checkout is not on main: %s\n' "$repo" >&2
        return 1
    }
    [ -z "$(git -C "$repo" status --porcelain 2>/dev/null)" ] || {
        printf 'error: canonical main checkout is dirty; refusing to move it: %s\n' "$repo" >&2
        git -C "$repo" status --short >&2 || true
        return 1
    }

    current=$(git -C "$repo" rev-parse HEAD 2>/dev/null) || return 1
    if [ "$current" != "$target" ]; then
        if ! git -C "$repo" merge-base --is-ancestor "$current" "$target" 2>/dev/null; then
            printf 'error: local main is ahead of or diverged from origin/main; refusing to rewrite it\n' >&2
            printf '  local:  %s\n  remote: %s\n' "$current" "$target" >&2
            return 1
        fi
        git -C "$repo" merge --ff-only --quiet "$target" || return 1
    fi

    current=$(git -C "$repo" rev-parse HEAD 2>/dev/null) || return 1
    [ "$current" = "$target" ] || {
        printf 'error: canonical main did not reach origin/main after fast-forward\n' >&2
        return 1
    }
}

main_runtime_bootstrap() {
    invocation_repo=$1
    materialized=${2:-}

    git -C "$invocation_repo" fetch --quiet origin main || {
        printf 'error: git fetch origin main failed\n' >&2
        return 1
    }
    target=$(git -C "$invocation_repo" rev-parse FETCH_HEAD 2>/dev/null) || return 1
    source_repo=$(main_runtime_find_main_worktree "$invocation_repo")
    [ -n "$source_repo" ] || {
        printf 'error: no worktree has refs/heads/main checked out; cannot anchor the runtime launcher\n' >&2
        return 1
    }
    [ -z "$(git -C "$source_repo" status --porcelain 2>/dev/null)" ] || {
        printf 'error: canonical main checkout is dirty; refusing runtime bootstrap: %s\n' "$source_repo" >&2
        git -C "$source_repo" status --short >&2 || true
        return 1
    }

    if [ -z "$materialized" ]; then
        materialized=$(mktemp -d "${TMPDIR:-/tmp}/shellbeam-main-runtime-launcher.XXXXXX") || return 1
    else
        rm -rf "$materialized"
        mkdir -p "$materialized"
    fi
    mkdir -p "$materialized/scripts/lib"

    for path in \
        scripts/run-main-runtime.sh \
        scripts/lib/main-runtime-bootstrap.sh \
        scripts/lib/main-runtime-owner.sh
    do
        git -C "$invocation_repo" show "$target:$path" >"$materialized/$path" || {
            printf 'error: origin/main %s is missing %s\n' "$target" "$path" >&2
            rm -rf "$materialized"
            return 1
        }
    done
    chmod +x "$materialized/scripts/run-main-runtime.sh"

    exec env \
        SHELLBEAM_MAIN_RUNTIME_BOOTSTRAPPED=1 \
        SHELLBEAM_SOURCE_REPO="$source_repo" \
        SHELLBEAM_TARGET_MAIN="$target" \
        SHELLBEAM_LAUNCHER_LIB_DIR="$materialized/scripts/lib" \
        SHELLBEAM_LAUNCHER_TMP_DIR="$materialized" \
        sh "$materialized/scripts/run-main-runtime.sh"
}
