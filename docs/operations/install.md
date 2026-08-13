# Install and uninstall

Prefer manual `shellbeam daemon` for the first test. `shellbeam install` atomically writes a per-user service definition, activates it, and restores the previous definition if activation fails. Linux uses systemd-user; macOS uses the current GUI launchd domain. Run `shellbeam doctor --json` afterward.

`shellbeam uninstall` stops/disables the daemon and removes only the service definition. It intentionally preserves configuration, state, output and receipts. ShellBeam does not manage `tunnel-client`.
