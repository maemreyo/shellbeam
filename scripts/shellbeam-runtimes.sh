#!/bin/sh
# List every ShellBeam runtime process on this machine, and optionally stop them.
#
# This exists because nothing else can see more than one runtime at a time.
# `shellbeam status` stats one socket and `shellbeam doctor` reports one lease;
# both resolve a single set of paths. scripts/lib/main-runtime-owner.sh tracks
# exactly one launcher stack, on one tunnel profile. None of them can see a
# daemon started with its own --runtime-dir, which is what every native test
# daemon does -- and those are the ones that leak. A test that fails or is
# interrupted between spawning a daemon and reaping it leaves one running with
# ppid 1, holding a lease on a directory nothing will look at again.
#
# So this script enumerates processes, which is deliberately not how ownership
# is decided anywhere else in this repo, and the distinction is the point: a
# lease or an owner record answers "who owns this directory", which cannot
# answer a question spanning every directory at once. Ownership is then read
# back per daemon from the lease it claims, so a daemon that is running but no
# longer holds its own directory reports as `stale` rather than `owner`.
#
# Process identity comes from scripts/lib/main-runtime-owner.sh rather than from
# matching command lines. runtime_process_is_same fingerprints a pid by its
# start time, so a pid reaped and reissued to a process with byte-identical argv
# -- a relaunched daemon, which is the common case here -- cannot be mistaken
# for the one that was listed. Comparing command strings would not catch that.
#
# Stopping is ordered rather than a broadcast kill, because killing the daemon
# first is wrong in a way that is not obvious. run-main-runtime.sh's supervise()
# loops on `kill -0 "$tunnel_pid"` -- it watches the tunnel, not the daemon.
# Kill its daemon and the launcher keeps running, leaving tunnel-client and
# `shellbeam mcp` alive in front of a socket nobody is behind. Signalling the
# launcher instead runs its `trap handle_signal INT TERM`, which reaps the
# tunnel and the daemon and only then releases the machine-global owner record.
# Launchers therefore go first here, and everything else is signalled from a
# snapshot taken before any of it began exiting.
#
# Usage:
#   scripts/shellbeam-runtimes.sh [list]
#   scripts/shellbeam-runtimes.sh kill [--dry-run]
#
# Environment:
#   RUNTIME_OWNER_DIR    machine-global launcher owner record, read only
#                        (default: /tmp/shellbeam-main-runtime-<uid>.owner)
#   TUNNEL_PROFILE       tunnel profile named in that record (default: shellbeam)
#   LAUNCHER_TIMEOUT_SECONDS  how long a launcher gets to run its own cleanup
#                             before the rest is signalled (default: 10)
#   TERM_TIMEOUT_SECONDS      how long anything still running after SIGTERM gets
#                             before SIGKILL (default: 5)
#
# It never unlinks daemon.sock, never removes a state or runtime directory, and
# never writes the owner record. A unix listener outlives the unlinking of its
# pathname, so removing the socket would hide a live daemon rather than stop it,
# and would let a second daemon bind the same path. Dead leases are reported;
# collecting them is your call.
set -eu

LAUNCHER_TIMEOUT_SECONDS=${LAUNCHER_TIMEOUT_SECONDS:-10}
TERM_TIMEOUT_SECONDS=${TERM_TIMEOUT_SECONDS:-5}

DEFAULT_RUNTIME_DIR="/tmp/shellbeam-$(id -u)"
DEFAULT_CONFIG_FILE="$HOME/Library/Application Support/ShellBeam/config.toml"

log() { printf '%s %s\n' "[$(date +%H:%M:%S)]" "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

# The owner helper's documented contract: these three variables and log/die must
# exist before it is sourced. Only its read-only identity primitives are used
# here -- acquire_runtime_owner and release_runtime_owner are never called,
# because taking machine-global launcher ownership is run-main-runtime.sh's job
# and a listing tool that took it would evict the launcher just by looking.
RUNTIME_OWNER_DIR=${RUNTIME_OWNER_DIR:-/tmp/shellbeam-main-runtime-$(id -u).owner}
TUNNEL_PROFILE=${TUNNEL_PROFILE:-shellbeam}
STOP_TIMEOUT_SECONDS=${STOP_TIMEOUT_SECONDS:-15}

SRC_REPO=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
HELPER="$SRC_REPO/scripts/lib/main-runtime-owner.sh"
[ -f "$HELPER" ] || die "runtime ownership helper missing: $HELPER"
# shellcheck source=lib/main-runtime-owner.sh
. "$HELPER"

# Set by argument parsing, declared here so the functions below are safe to call
# under `set -u` when this file is sourced for its definitions alone.
action=list
dry_run=""
snapshot=""

# ------------------------------------------------------------------ discovery ---

# runtime_dir_from_argv mirrors the precedence in internal/config/load.go: an
# explicit flag wins over the config file, which wins over the built-in default.
# Reproducing it here rather than asking each daemon keeps this working against
# daemons whose binary has since been rebuilt out from under them, which is the
# common case for a stale test daemon.
#
# A runtime directory containing spaces cannot survive the round trip through
# ps(1) and is not handled; none of the paths this resolves to has ever had one.
runtime_dir_from_argv() {
	config=""
	explicit=""
	prev=""
	for word in "$@"; do
		case "$word" in
		--runtime-dir=*) explicit=${word#--runtime-dir=} ;;
		--config=*) config=${word#--config=} ;;
		*)
			case "$prev" in
			--runtime-dir) explicit=$word ;;
			--config) config=$word ;;
			esac
			;;
		esac
		prev=$word
	done
	if [ -n "$explicit" ]; then
		printf '%s\n' "$explicit"
		return 0
	fi
	[ -n "$config" ] || config=$DEFAULT_CONFIG_FILE
	if [ -f "$config" ]; then
		from_config=$(sed -n 's/^[[:space:]]*runtime_dir[[:space:]]*=[[:space:]]*"\(.*\)"[[:space:]]*$/\1/p' "$config" | head -n 1)
		if [ -n "$from_config" ]; then
			printf '%s\n' "$from_config"
			return 0
		fi
	fi
	printf '%s\n' "$DEFAULT_RUNTIME_DIR"
}

# orphan_suffix marks a process nothing will ever reap.
#
# A daemon, tunnel or mcp on ppid 1 lost the launcher or test that owned it. A
# launcher is different: its parent is only the shell that started it, so
# `nohup scripts/run-main-runtime.sh &` leaves it on ppid 1 by design while it
# still reaps its own children through its traps. Marking that reported a
# healthy detached runtime as a leak, which is how this was found.
orphan_suffix() {
	[ "$1" != launcher ] || return 0
	[ "$2" = 1 ] || return 0
	printf '+orphan\n'
}

# lease_pid prints the pid recorded in a directory's daemon.owner, or nothing.
lease_pid() {
	[ -f "$1/daemon.owner" ] || return 0
	sed -n 's/.*"pid":[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$1/daemon.owner" | head -n 1
}

# classify_role prints the role for an argv, or nothing. Classification is by
# argv shape, not by searching the command string for "shellbeam": a `grep
# shellbeam` or an editor holding one of these files open would otherwise be
# reported as a runtime and, worse, signalled by `kill`.
classify_role() {
	base0=${1##*/}
	argv1=${2:-}
	base1=${argv1##*/}
	case "$base0" in
	shellbeam)
		case "$argv1" in
		daemon) printf 'daemon\n' ;;
		mcp) printf 'mcp\n' ;;
		esac
		;;
	tunnel-client)
		if [ "$argv1" = run ]; then printf 'tunnel\n'; fi
		;;
	run-main-runtime.sh) printf 'launcher\n' ;;
	sh | bash | dash | zsh | ksh)
		if [ "$base1" = run-main-runtime.sh ]; then printf 'launcher\n'; fi
		;;
	esac
}

# collect writes one tab-separated record per runtime process to the snapshot:
#   role  pid  ppid  etime  state  runtime_dir  binary  started  command
#
# `started` is the process start fingerprint from the owner helper, captured
# here so that signalling later can prove the pid is still the same process.
collect() {
	ps -Ao pid=,ppid=,etime=,command= | while read -r pid ppid etime command; do
		# Word splitting is what turns the command back into argv, so globbing
		# has to be off first -- a `*` anywhere in a command line would
		# otherwise expand against the working directory.
		set -f
		# shellcheck disable=SC2086
		set -- $command
		set +f
		[ "$#" -gt 0 ] || continue
		argv0=$1
		role=$(classify_role "$1" "${2:-}")
		[ -n "$role" ] || continue
		if [ "$role" = launcher ]; then
			# `sh scripts/run-main-runtime.sh` names the interpreter in argv0,
			# which identifies nothing. The script path does.
			case "${argv0##*/}" in
			sh | bash | dash | zsh | ksh) argv0=${2:-$argv0} ;;
			esac
		fi

		runtime_dir="-"
		state="-"
		if [ "$role" = daemon ]; then
			runtime_dir=$(runtime_dir_from_argv "$@")
			owner=$(lease_pid "$runtime_dir")
			if [ "$owner" = "$pid" ]; then
				state=owner
			elif [ -z "$owner" ]; then
				state=no-lease
			else
				state=stale
			fi
		fi
		state="$state$(orphan_suffix "$role" "$ppid")"

		started=$(runtime_process_started "$pid" 2>/dev/null || true)

		printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
			"$role" "$pid" "$ppid" "$etime" "$state" "$runtime_dir" \
			"$(printf '%s' "$argv0" | awk -F/ '{ if (NF>1) print $(NF-1) "/" $NF; else print $NF }')" \
			"$started" "$command"
	done
}

field() { printf '%s' "$1" | cut -f "$2"; }

# ----------------------------------------------------------------------- list ---

print_header() {
	printf '%-13s %-7s %-7s %-14s %-11s %-40s %s\n' \
		ROLE PID PPID STATE AGE RUNTIME-DIR BINARY
}

# print_row takes the indent as part of the ROLE field rather than as a prefix,
# so nesting a group does not shift every column to its right.
print_row() {
	printf '%-13s %-7s %-7s %-14s %-11s %-40s %s\n' \
		"${2:-}$(field "$1" 1)" "$(field "$1" 2)" "$(field "$1" 3)" \
		"$(field "$1" 5)" "$(field "$1" 4)" "$(field "$1" 6)" "$(field "$1" 7)"
}

# print_group prints a launcher and everything descended from it. mcp is a child
# of the tunnel rather than of the launcher, so descent is two levels.
print_group() {
	launcher_pid=$(field "$1" 2)
	print_row "$1"
	while IFS= read -r child; do
		[ "$(field "$child" 3)" = "$launcher_pid" ] || continue
		print_row "$child" '  '
		child_pid=$(field "$child" 2)
		while IFS= read -r grandchild; do
			[ "$(field "$grandchild" 3)" = "$child_pid" ] || continue
			print_row "$grandchild" '    '
		done <"$snapshot"
	done <"$snapshot"
}

# grouped_pids lists every pid printed under a launcher, so the stray pass can
# skip them without re-deriving the parentage.
grouped_pids() {
	while IFS= read -r record; do
		[ "$(field "$record" 1)" = launcher ] || continue
		launcher_pid=$(field "$record" 2)
		printf '%s\n' "$launcher_pid"
		while IFS= read -r child; do
			[ "$(field "$child" 3)" = "$launcher_pid" ] || continue
			child_pid=$(field "$child" 2)
			printf '%s\n' "$child_pid"
			while IFS= read -r grandchild; do
				[ "$(field "$grandchild" 3)" = "$child_pid" ] || continue
				field "$grandchild" 2
				printf '\n'
			done <"$snapshot"
		done <"$snapshot"
	done <"$snapshot"
}

# print_owner_record reports the machine-global launcher owner record. It is the
# one piece of state that says which launcher stack currently owns this machine,
# and a record left behind by a launcher that died ungracefully is what makes a
# later `acquire_runtime_owner` spend STOP_TIMEOUT_SECONDS reclaiming it.
print_owner_record() {
	[ -d "$RUNTIME_OWNER_DIR" ] || return 0
	pid=$(owner_field launcher.pid)
	started=$(owner_field launcher.started)
	profile=$(owner_field profile)
	ready=no
	[ -f "$RUNTIME_OWNER_DIR/ready" ] && ready=yes
	if [ -z "$pid" ]; then
		# mkdir is the ownership primitive, so a directory with no pid yet is a
		# launcher mid-acquisition rather than a broken record.
		state=initializing
	elif runtime_process_is_same "$pid" "$started"; then
		state=live
	else
		state=stale
	fi
	printf 'LAUNCHER OWNER (%s)\n' "$RUNTIME_OWNER_DIR"
	printf '%-13s %-7s %-11s %-7s %s\n' STATE PID PROFILE READY STARTED
	printf '%-13s %-7s %-11s %-7s %s\n' \
		"$state" "${pid:--}" "${profile:--}" "$ready" "${started:--}"
	printf '\n'
}

# dead_leases reports directories whose daemon.owner names a pid that is gone.
# This is what makes run-main-runtime.sh's "another daemon won the lease" and
# doctor's runtime_owner failures diagnosable instead of mysterious.
#
# A lease outlives its daemon however that daemon died. Measured: a daemon
# retired by run-main-runtime.sh's cleanup removed its daemon.sock and left
# daemon.owner behind naming its own dead pid. That is not a defect -- ownership
# is decided by whether the recorded pid is alive, so a crash and a clean exit
# recover through one path -- but it does mean every row here is an ordinary
# leftover rather than evidence of an ungraceful death. A stale lease does not
# block a successor: the launcher resolves ownership through `doctor`, which
# reported "no daemon owns the runtime directory" against exactly such a file.
#
# The scan root is resolved to its physical path first. On darwin /tmp is a
# symlink to /private/tmp, and find(1) does not follow a symlink given as its
# starting point unless asked -- searching "/tmp" directly finds nothing at all.
dead_leases() {
	root=$(CDPATH='' cd -- "$(dirname -- "$DEFAULT_RUNTIME_DIR")" 2>/dev/null && pwd -P) || return 0
	{
		# Runtime directories in use are known from the snapshot, which is the
		# only way to reach one a config file moved outside the tmp root.
		cut -f 6 <"$snapshot" | grep -v '^-$' || true
		find "$root" -maxdepth 1 -type d -name 'shellbeam-*' 2>/dev/null |
			while IFS= read -r base; do
				find "$base" -maxdepth 2 -name daemon.owner 2>/dev/null |
					while IFS= read -r owner_file; do dirname "$owner_file"; done
			done
	} | sort -u | while IFS= read -r dir; do
		pid=$(lease_pid "$dir")
		[ -n "$pid" ] || continue
		if kill -0 "$pid" 2>/dev/null; then continue; fi
		printf '%-7s %s\n' "$pid" "$dir"
	done
}

do_list() {
	print_owner_record

	total=$(wc -l <"$snapshot" | tr -d ' ')
	if [ "$total" = 0 ]; then
		printf 'no ShellBeam runtime processes\n'
	else
		launchers=$(grep -c '^launcher	' "$snapshot" || true)
		if [ "$launchers" != 0 ]; then
			printf 'MANAGED RUNTIMES\n'
			print_header
			while IFS= read -r record; do
				[ "$(field "$record" 1)" = launcher ] || continue
				print_group "$record"
			done <"$snapshot"
			printf '\n'
		fi

		grouped=$(grouped_pids | sort -u)
		strays=$(while IFS= read -r record; do
			pid=$(field "$record" 2)
			if printf '%s\n' "$grouped" | grep -qx "$pid"; then continue; fi
			printf '%s\n' "$record"
		done <"$snapshot" | sort)
		if [ -n "$strays" ]; then
			printf 'UNMANAGED\n'
			print_header
			printf '%s\n' "$strays" | while IFS= read -r record; do print_row "$record"; done
			printf '\n'
		fi
	fi

	dead=$(dead_leases)
	if [ -n "$dead" ]; then
		printf 'DEAD LEASES (lease file names a pid that is gone)\n'
		printf '%-7s %s\n' PID DIRECTORY
		printf '%s\n' "$dead"
		printf '\n'
	fi

	daemons=$(grep -c '^daemon	' "$snapshot" || true)
	orphans=$(grep -c '+orphan' "$snapshot" || true)
	printf '%s runtime process(es), %s daemon(s), %s orphaned\n' \
		"$total" "$daemons" "$orphans"
}

# ----------------------------------------------------------------------- kill ---

# signal_pid signals one pid only if it is still the process that was snapshotted.
# Between the snapshot and the signal a pid can be reaped and reissued -- the
# hazard run-main-runtime.sh's cleanup() guards against by clearing the variable
# after `wait` -- and here the window is wider because a launcher's own teardown
# runs inside it. The start-time fingerprint is what closes it: a relaunched
# daemon has byte-identical argv, so nothing derived from the command line could.
signal_pid() {
	pid=$1
	started=$2
	sig=$3
	label=$4
	if ! runtime_process_is_same "$pid" "$started"; then
		# Either already gone, or the pid now belongs to something else. Both
		# mean this is not ours to signal.
		return 0
	fi
	if [ -n "$dry_run" ]; then
		log "would send SIG$sig to $pid ($label)"
		return 0
	fi
	kill -"$sig" "$pid" 2>/dev/null || true
}

# wait_gone blocks until none of the pids in a file are alive, or the budget runs
# out. The helper's wait_process_exit is deliberately not reused: it covers one
# pid and calls die on timeout, whereas a stop pass here waits on a whole set and
# must escalate to SIGKILL rather than abort with the set half-signalled.
wait_gone() {
	pid_file=$1
	budget=$2
	deadline=$(($(date +%s) + budget))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		alive=""
		while IFS= read -r pid; do
			case "$(runtime_process_state "$pid")" in
			"" | Z*) ;;
			*) alive=1 ;;
			esac
		done <"$pid_file"
		[ -n "$alive" ] || return 0
		sleep 0.2
	done
	return 1
}

signal_role() {
	want=$1
	sig=$2
	while IFS= read -r record; do
		role=$(field "$record" 1)
		case "$want" in
		launcher) [ "$role" = launcher ] || continue ;;
		rest) [ "$role" != launcher ] || continue ;;
		esac
		signal_pid "$(field "$record" 2)" "$(field "$record" 8)" "$sig" \
			"$role $(field "$record" 7)"
	done <"$snapshot"
}

do_kill() {
	if [ ! -s "$snapshot" ]; then
		log "no ShellBeam runtime processes to stop"
		return 0
	fi
	do_list
	printf '\n'

	pids=$(mktemp -t shellbeam-runtimes-pids) || die "mktemp failed"
	# shellcheck disable=SC2064
	trap "rm -f '$snapshot' '$pids'" EXIT INT TERM

	if grep -q '^launcher	' "$snapshot"; then
		log "stopping launchers first so their own cleanup reaps tunnel and daemon and releases ownership"
		signal_role launcher TERM
		if [ -z "$dry_run" ]; then
			grep '^launcher	' "$snapshot" | cut -f 2 >"$pids"
			wait_gone "$pids" "$LAUNCHER_TIMEOUT_SECONDS" ||
				log "a launcher did not exit within ${LAUNCHER_TIMEOUT_SECONDS}s; signalling the rest anyway"
		fi
	fi

	log "sending SIGTERM to the remaining runtime processes"
	signal_role rest TERM
	if [ -n "$dry_run" ]; then
		log "dry run: nothing was signalled"
		return 0
	fi

	cut -f 2 <"$snapshot" >"$pids"
	if wait_gone "$pids" "$TERM_TIMEOUT_SECONDS"; then
		log "all runtime processes exited"
		return 0
	fi

	log "escalating to SIGKILL for what is still running"
	signal_role launcher KILL
	signal_role rest KILL
	if wait_gone "$pids" "$TERM_TIMEOUT_SECONDS"; then
		log "all runtime processes exited"
		return 0
	fi
	while IFS= read -r pid; do
		case "$(runtime_process_state "$pid")" in
		"" | Z*) ;;
		*) log "pid $pid survived SIGKILL" ;;
		esac
	done <"$pids"
	return 1
}

# ----------------------------------------------------------------------- main ---

# Sourcing this file defines its functions without taking a pass over the
# process table, which is what scripts/test-shellbeam-runtimes.sh needs in order
# to exercise classification and the signalling guard against fixtures. `return`
# is only reached when the variable is set, so an ordinary execution -- where it
# would be an error outside a function -- never evaluates it.
[ -n "${SHELLBEAM_RUNTIMES_LIB_ONLY:-}" ] && return 0

for arg in "$@"; do
	case "$arg" in
	list | kill) action=$arg ;;
	--dry-run) dry_run=1 ;;
	-h | --help)
		# The header comment is the help text. Ending the range at the first
		# non-comment line keeps the two from drifting apart the way a line
		# number would every time the header is edited.
		awk 'NR > 1 { if ($0 !~ /^#/) exit; sub(/^# ?/, ""); print }' "$0"
		exit 0
		;;
	*) die "unknown argument: $arg (want: list | kill [--dry-run])" ;;
	esac
done
[ "$action" = kill ] || [ -z "$dry_run" ] ||
	die "--dry-run applies to 'kill'"

snapshot=$(mktemp -t shellbeam-runtimes) || die "mktemp failed"
trap 'rm -f "$snapshot"' EXIT INT TERM

collect >"$snapshot"

case "$action" in
list) do_list ;;
kill) do_kill ;;
esac
