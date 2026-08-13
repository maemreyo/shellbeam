# ADR 0002: File-backed durable authority

ShellBeam uses per-operation JSON, per-session metadata/receipt JSON, and append-only output files beneath a verified `0700` per-user root. Replacement writes are temp-file, file-sync, rename, directory-sync. The store reports no durable change, durable change, or ambiguous change; callers never turn sync uncertainty into permission to spawn.

The daemon reserves control-plane bytes and capacity before a durable operation binding, then spawns. Persisted PID/PGID values are never ownership capabilities. V1 startup abandons unresolved sessions instead of reattaching. Compact terminal tombstones remain indefinitely and preserve the operation binding and receipt while output may be removed.
