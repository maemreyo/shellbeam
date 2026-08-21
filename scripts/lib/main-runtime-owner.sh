#!/bin/sh
# Machine-global ownership helpers for scripts/run-main-runtime.sh.
#
# Required caller variables:
#   RUNTIME_OWNER_DIR, TUNNEL_PROFILE, STOP_TIMEOUT_SECONDS
# Required caller functions:
#   log, die

RUNTIME_OWNER_HELD=${RUNTIME_OWNER_HELD:-}

runtime_process_state() {
    pid=$1
    ps -p "$pid" -o state= 2>/dev/null | awk 'NR==1 {gsub(/^[[:space:]]+|[[:space:]]+$/, ""); print}'
}

runtime_process_started() {
    pid=$1
    state=$(runtime_process_state "$pid")
    case "$state" in
    "" | Z*) return 1 ;;
    esac
    ps -p "$pid" -o lstart= 2>/dev/null |
        sed 's/^[[:space:]]*//; s/[[:space:]]*$//' |
        awk 'NR==1 {print}'
}

runtime_process_is_same() {
    pid=$1
    want_started=$2
    [ -n "$pid" ] && [ -n "$want_started" ] || return 1
    have_started=$(runtime_process_started "$pid" 2>/dev/null || true)
    [ -n "$have_started" ] && [ "$have_started" = "$want_started" ]
}

legacy_tunnel_pids_from_ps() {
    awk -v profile="$TUNNEL_PROFILE" '
    {
        pid=$1
        exe=$2
        sub(/^.*\//, "", exe)
        if (exe == "tunnel-client" &&
            $3 == "run" &&
            $4 == "--profile" &&
            $5 == profile &&
            NF == 5) {
            print pid
        }
    }'
}

legacy_tunnel_pids() {
    ps -ax -o pid= -o command= | legacy_tunnel_pids_from_ps
}

wait_process_exit() {
    pid=$1
    label=$2
    deadline=$(($(date +%s) + STOP_TIMEOUT_SECONDS))
    while [ "$(date +%s)" -lt "$deadline" ]; do
        state=$(runtime_process_state "$pid")
        case "$state" in
        "" | Z*) return 0 ;;
        esac
        sleep 0.1
    done
    die "$label pid $pid did not exit within ${STOP_TIMEOUT_SECONDS}s"
}

retire_legacy_tunnels() {
    pids=$(legacy_tunnel_pids)
    [ -n "$pids" ] || return 0

    for pid in $pids; do
        log "stopping incumbent tunnel for profile $TUNNEL_PROFILE (pid $pid)"
        kill -TERM "$pid" 2>/dev/null || true
    done
    for pid in $pids; do
        wait_process_exit "$pid" "tunnel-client"
    done
}

owner_field() {
    name=$1
    [ -f "$RUNTIME_OWNER_DIR/$name" ] || return 0
    cat "$RUNTIME_OWNER_DIR/$name" 2>/dev/null || true
}

remove_owner_record_if_unchanged() {
    want_pid=$1
    want_started=$2

    [ -d "$RUNTIME_OWNER_DIR" ] || return 0
    [ "$(owner_field launcher.pid)" = "$want_pid" ] || return 1
    [ "$(owner_field launcher.started)" = "$want_started" ] || return 1

    rm -f \
        "$RUNTIME_OWNER_DIR/ready" \
        "$RUNTIME_OWNER_DIR/profile" \
        "$RUNTIME_OWNER_DIR/launcher.started" \
        "$RUNTIME_OWNER_DIR/launcher.pid"
    if ! rmdir "$RUNTIME_OWNER_DIR" 2>/dev/null; then
        die "runtime owner directory contains unexpected files: $RUNTIME_OWNER_DIR"
    fi
}

retire_recorded_owner() {
    [ -d "$RUNTIME_OWNER_DIR" ] || return 0

    # mkdir is the ownership primitive. Give a just-created directory a brief
    # initialization grace before deciding that missing metadata is stale.
    if [ ! -f "$RUNTIME_OWNER_DIR/launcher.pid" ]; then
        sleep 0.2
    fi

    pid=$(owner_field launcher.pid)
    started=$(owner_field launcher.started)

    if runtime_process_is_same "$pid" "$started"; then
        [ "$pid" != "$$" ] || die "runtime owner record points at this launcher but ownership was not marked held"
        log "requesting incumbent runtime launcher exit (pid $pid)"
        kill -TERM "$pid" 2>/dev/null || true
        wait_process_exit "$pid" "runtime launcher"

        # A fixed incumbent normally removes the directory itself. If it died
        # between TERM and release, reclaim only the exact record inspected.
        if [ -d "$RUNTIME_OWNER_DIR" ]; then
            if ! remove_owner_record_if_unchanged "$pid" "$started"; then
                log "runtime owner changed during incumbent cleanup; retrying acquisition"
            fi
        fi
        return 0
    fi

    # Missing/dead/reused PIDs are never signalled. Reclaim only unchanged metadata.
    log "reclaiming stale runtime owner record (pid ${pid:-missing})"
    if ! remove_owner_record_if_unchanged "$pid" "$started"; then
        log "runtime owner changed during stale-record reclaim; retrying acquisition"
    fi
    return 0
}

acquire_runtime_owner() {
    [ -n "$RUNTIME_OWNER_HELD" ] && return 0

    self_started=$(runtime_process_started "$$" 2>/dev/null || true)
    [ -n "$self_started" ] || die "cannot fingerprint runtime launcher pid $$"

    deadline=$(($(date +%s) + STOP_TIMEOUT_SECONDS))
    while :; do
        if mkdir "$RUNTIME_OWNER_DIR" 2>/dev/null; then
            # PID is intentionally first so contenders can identify initialization.
            printf '%s\n' "$$" >"$RUNTIME_OWNER_DIR/launcher.pid"
            printf '%s\n' "$self_started" >"$RUNTIME_OWNER_DIR/launcher.started"
            printf '%s\n' "$TUNNEL_PROFILE" >"$RUNTIME_OWNER_DIR/profile"
            : >"$RUNTIME_OWNER_DIR/ready"
            RUNTIME_OWNER_HELD=1
            log "runtime launcher ownership acquired (pid $$)"
            return 0
        fi

        retire_recorded_owner
        [ "$(date +%s)" -lt "$deadline" ] ||
            die "could not acquire runtime launcher ownership within ${STOP_TIMEOUT_SECONDS}s"
    done
}

release_runtime_owner() {
    [ -n "$RUNTIME_OWNER_HELD" ] || return 0

    pid=$(owner_field launcher.pid)
    started=$(owner_field launcher.started)
    self_started=$(runtime_process_started "$$" 2>/dev/null || true)

    if [ "$pid" = "$$" ] && [ -n "$self_started" ] && [ "$started" = "$self_started" ]; then
        remove_owner_record_if_unchanged "$pid" "$started"
    else
        log "warning: runtime owner record changed; refusing to remove ownership for pid ${pid:-missing}"
    fi
    RUNTIME_OWNER_HELD=""
}
