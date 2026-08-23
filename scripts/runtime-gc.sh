#!/bin/sh
# Reclaim the disk the runtime loop consumes, without touching anything live.
#
# This exists because the watch-restart loop rebuilds on every origin/main
# change, and a busy merge day is a busy build day. Measured on one such day:
# the Go build cache went from 1.9G to 5.1G in half a day, free space fell from
# 6.1G to 1.3G, and `make build` then failed inside the launcher -- which had
# already retired the incumbent stack, so the runtime did not come back. The
# outward symptom was an MCP connector that would not reconnect. Disk pressure
# is therefore a runtime availability concern here, not housekeeping.
#
# Two classes of garbage, treated differently on purpose.
#
# Stale /tmp/shellbeam-* directories are pure leftovers -- test runtimes and
# throwaway worktrees that outlived whatever made them -- so they go every time.
# The live runtime directory, the launcher owner record and any path git still
# reports as a registered worktree are excluded by identity rather than by name
# pattern, because a name pattern cannot tell a leftover from the thing that is
# currently serving.
#
# The Go build cache is not garbage; it is the reason an incremental build takes
# seconds. Collecting it unconditionally would trade a disk problem for a
# latency problem on every run. It is therefore collected only once it has grown
# past a threshold, which is the condition that actually preceded the outage.
#
# Usage:
#   scripts/runtime-gc.sh [--dry-run] [--force-cache]
#
# Environment:
#   GC_CACHE_MAX_GIB   collect the Go build cache once it exceeds this many GiB
#                      (default: 3)
set -eu

GC_CACHE_MAX_GIB=${GC_CACHE_MAX_GIB:-3}

dry_run=""
force_cache=""
for arg in "$@"; do
	case "$arg" in
	--dry-run) dry_run=1 ;;
	--force-cache) force_cache=1 ;;
	-h | --help)
		awk 'NR > 1 { if ($0 !~ /^#/) exit; sub(/^# ?/, ""); print }' "$0"
		exit 0
		;;
	*)
		printf 'error: unknown argument: %s (want: --dry-run | --force-cache)\n' "$arg" >&2
		exit 1
		;;
	esac
done

log() { printf '%s %s\n' "[$(date +%H:%M:%S)]" "$*" >&2; }

REPO=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
RUNTIME_DIR="/tmp/shellbeam-$(id -u)"
OWNER_DIR="/tmp/shellbeam-main-runtime-$(id -u).owner"

# The data volume is where all of this lives on darwin; / is read-only there and
# would report a number that never moves.
DATA_VOLUME=/System/Volumes/Data
[ -d "$DATA_VOLUME" ] || DATA_VOLUME=/
avail() { df -k "$DATA_VOLUME" 2>/dev/null | awk 'NR==2 {printf "%.1f", $4/1048576}'; }

before=$(avail)
log "free before: ${before}GiB"

# --------------------------------------------------------- stale /tmp dirs ---

# Registered worktrees are resolved through git rather than assumed, because a
# throwaway worktree under /tmp is indistinguishable from a leftover by name.
registered=$(git -C "$REPO" worktree list --porcelain 2>/dev/null |
	awk '/^worktree /{print substr($0, 10)}' || true)

removed=0
for dir in /tmp/shellbeam-*; do
	[ -d "$dir" ] || continue
	case "$dir" in
	"$RUNTIME_DIR" | "$OWNER_DIR") continue ;;
	esac
	# /tmp is a symlink on darwin; git reports the physical path.
	if printf '%s\n' "$registered" | grep -qx "/private$dir"; then
		log "keep (registered worktree): $dir"
		continue
	fi
	# A directory some live process names is not a leftover, whatever its age.
	if ps -Ao command= | grep -qF "$dir"; then
		log "keep (in use by a live process): $dir"
		continue
	fi
	if [ -n "$dry_run" ]; then
		log "would remove: $dir"
	else
		rm -rf "$dir"
	fi
	removed=$((removed + 1))
done
log "stale /tmp/shellbeam-* directories: $removed"

# Materialized launcher copies land in TMPDIR, not /tmp, and one is left behind
# by every launcher that refused before installing its full cleanup handler.
tmp_root=${TMPDIR:-/tmp}
tmp_root=${tmp_root%/}
for dir in "$tmp_root"/shellbeam-main-runtime-launcher.*; do
	[ -d "$dir" ] || continue
	if ps -Ao command= | grep -qF "$dir"; then
		log "keep (running launcher): $dir"
		continue
	fi
	if [ -n "$dry_run" ]; then
		log "would remove: $dir"
	else
		rm -rf "$dir"
	fi
done

# ------------------------------------------------------------- build cache ---

cache_dir=$(go env GOCACHE 2>/dev/null || true)
if [ -n "$cache_dir" ] && [ -d "$cache_dir" ]; then
	cache_gib=$(du -sk "$cache_dir" 2>/dev/null | awk '{printf "%.1f", $1/1048576}')
	if [ -n "$force_cache" ] ||
		awk -v c="$cache_gib" -v m="$GC_CACHE_MAX_GIB" 'BEGIN{exit !(c > m)}'; then
		if [ -n "$dry_run" ]; then
			log "would collect Go build cache: ${cache_gib}GiB (limit ${GC_CACHE_MAX_GIB}GiB)"
		else
			log "collecting Go build cache: ${cache_gib}GiB (limit ${GC_CACHE_MAX_GIB}GiB)"
			go clean -cache
		fi
	else
		log "Go build cache ${cache_gib}GiB is within ${GC_CACHE_MAX_GIB}GiB; keeping it"
	fi
fi

after=$(avail)
log "free after: ${after}GiB"
