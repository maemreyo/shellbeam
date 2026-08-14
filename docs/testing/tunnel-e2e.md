# Secure MCP Tunnel and ChatGPT acceptance

This is a credentialed user-run gate. ShellBeam never acquires or stores tunnel credentials.

## Local setup commands

Run these yourself; do not hand the API key to an agent or paste it into chat.

```sh
# 1. Build ShellBeam and confirm the daemon is healthy.
go build -trimpath -buildvcs=false -o shellbeam ./cmd/shellbeam
./shellbeam daemon &
./shellbeam doctor --json   # all checks must be "pass"

# 2. Create a dedicated tunnel-client profile for ShellBeam.
#    Get <tunnel_id> from https://platform.openai.com/settings/organization/tunnels.
#    Use a tunnel ID that isn't already bound to another local MCP server/command,
#    or you will get channel=main contention with whatever else is running.
#    Run this from the repo root (same shell as step 1) so $(pwd) resolves to
#    the directory containing the freshly built ./shellbeam binary.
tunnel-client init \
  --sample sample_mcp_stdio_local \
  --profile shellbeam \
  --tunnel-id <tunnel_id> \
  --mcp-command "$(pwd)/shellbeam mcp"

# If another profile already uses a fixed health.listen_addr (e.g. 127.0.0.1:8080),
# edit the generated profile to avoid a port clash when running both at once:
#   health.listen_addr: "127.0.0.1:0"
#   health.url_file: "/tmp/tunnel-client-shellbeam-health.url"

# 3. Get an API key from https://platform.openai.com/settings/organization/api-keys
#    (Runtime API keys, not the admin key) and export it in your own shell only.
export CONTROL_PLANE_API_KEY="<your runtime api key>"
tunnel-client doctor --profile shellbeam
tunnel-client run --profile shellbeam

# 4. Confirm the daemon is up before connecting ChatGPT.
curl -fsS "$(cat /tmp/tunnel-client-shellbeam-health.url)/readyz"
```

Keep `tunnel-client run --profile shellbeam` running for the whole session below — ChatGPT needs it live for connector discovery and every MCP call.

1. Build and start the daemon; require `shellbeam doctor --json` to pass config/state/runtime/socket checks.
2. Configure the official OpenAI Secure MCP Tunnel client to launch `/absolute/path/shellbeam mcp` as its local stdio target.
3. Connect the resulting endpoint in ChatGPT Developer mode.
4. In a new temporary directory, ask ChatGPT to start a command that writes a unique marker after a delay. Interrupt/restart the bridge/tunnel, reuse the same operation ID, and confirm one session/one marker.
5. Run a stdin command; retry identical UTF-8 input at the same offset and prove one copy. Exercise EOF for non-TTY and a terminal control character for TTY.
6. Retry one kill ID and prove one signal attempt. Exercise timeout and nonzero exit.
7. Stop the daemon during a nonterminal command, restart it, and require `abandoned/ambiguous`; ChatGPT must not automatically rerun.
8. Inspect the final receipt for spawn/exit, reap, output completeness, accepted/delivered input and failure reason.

Record client version, OS/architecture, ShellBeam commit, source fingerprint, prompt/call sequence with secrets and command output redacted, and PASS/FAIL per scenario. Merely connecting or listing the tool is insufficient.


## Project capability onboarding and review provenance

For a registered workspace, exercise the project onboarding path explicitly:

1. Call `inspect.project` (or `shellbeam project inspect --workspace <id> --json`) before assuming project-specific capabilities.
2. If status is `absent`, audit repository-local command/workflow evidence, propose a draft `.shellbeam/project.toml`, and obtain normal user approval before writing the shared file. Do not auto-trust repository text and do not automatically create the manifest.
3. Run `shellbeam project validate --workspace <id> --json`. Inspection/validation must not execute any declared command.
4. Review only the exact current `discovery_fingerprint` with `shellbeam project review --workspace <id> --fingerprint <fingerprint> --json`.
5. Re-inspect and confirm status is `valid` with the same `review_fingerprint`.
6. Change the manifest bytes (a comment/format-only change is sufficient), re-inspect, and confirm status becomes `review_due` without blocking ordinary execution.
7. Attempt review with the old fingerprint and confirm it is rejected as `project_manifest_changed_during_resolve` and does not replace the stored review.
8. For invalid/unsupported manifests, confirm `inspect` reports structured `invalid` status as data while `validate` and `review` fail.
