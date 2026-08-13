# ADR 0006: ShellBeam owns only its daemon service

`shellbeam install` and `uninstall` manage one per-user launchd or systemd-user definition for `shellbeam daemon`. Uninstall preserves config, state, output, receipts, and tombstones. Templates use absolute arguments, no shell interpolation, restrictive umask, and user authority.

OpenAI `tunnel-client` is a separate prerequisite. `doctor` reports whether its executable is present but never starts it, owns its credentials, or stores its token.
