# Secure MCP Tunnel and ChatGPT acceptance

This is a credentialed user-run gate. ShellBeam never acquires or stores tunnel credentials.

1. Build and start the daemon; require `shellbeam doctor --json` to pass config/state/runtime/socket checks.
2. Configure the official OpenAI Secure MCP Tunnel client to launch `/absolute/path/shellbeam mcp` as its local stdio target.
3. Connect the resulting endpoint in ChatGPT Developer mode.
4. In a new temporary directory, ask ChatGPT to start a command that writes a unique marker after a delay. Interrupt/restart the bridge/tunnel, reuse the same operation ID, and confirm one session/one marker.
5. Run a stdin command; retry identical UTF-8 input at the same offset and prove one copy. Exercise EOF for non-TTY and a terminal control character for TTY.
6. Retry one kill ID and prove one signal attempt. Exercise timeout and nonzero exit.
7. Stop the daemon during a nonterminal command, restart it, and require `abandoned/ambiguous`; ChatGPT must not automatically rerun.
8. Inspect the final receipt for spawn/exit, reap, output completeness, accepted/delivered input and failure reason.

Record client version, OS/architecture, ShellBeam commit, source fingerprint, prompt/call sequence with secrets and command output redacted, and PASS/FAIL per scenario. Merely connecting or listing the tool is insufficient.

