# ADR 0004: Authenticated Unix-socket IPC

The stateless MCP bridge talks to the daemon using closed version-1 JSON over HTTP/1.1 on a Unix socket. The runtime directory is `0700`, the socket is `0600`, and the accepted peer UID must equal the daemon UID before request decoding (`SO_PEERCRED` on Linux, `LOCAL_PEERCRED`/`Xucred` on macOS). There is no TCP fallback.

The daemon owns every process capability. A new daemon incarnation durably changes unresolved starting/running/finalizing sessions to `abandoned/ambiguous`; it never probes or signals a persisted PID/PGID.
