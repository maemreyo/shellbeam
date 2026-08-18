# Quickstart

1. Verify the adjacent source ZIP checksum with `sha256sum -c <file>.sha256` on Linux or `shasum -a 256 -c <file>.sha256` on macOS.
2. Unzip into a private directory. Install Go 1.26.6 and run `go mod verify && go test ./...`.
3. Build the source ZIP: `go build -trimpath -buildvcs=false -o shellbeam ./cmd/shellbeam`. A Git checkout may use `-buildvcs=true`.
4. Run `./shellbeam doctor --json`. Warnings for an absent daemon/tunnel client are expected before setup; unsafe directory checks are failures.
5. Start `./shellbeam daemon` manually. In another terminal, run doctor again.
6. Configure OpenAI Secure MCP Tunnel with the stdio target `/absolute/path/shellbeam mcp`. Keep tunnel-client credentials in tunnel-client's supported configuration, never in ShellBeam config.
7. Use one `operation_id` for an intended start and every retry. Poll by byte cursor. Retry stdin with the authoritative `next_input_offset`. Reuse one `kill_id` for a signal attempt.

Optional per-user daemon installation uses `./shellbeam install`. Test manual daemon mode first. `./shellbeam uninstall` does not erase data.

Default Linux paths follow XDG config/state/runtime variables; fallbacks are `~/.config/shellbeam`, `~/.local/state/shellbeam`, and `/tmp/shellbeam-<uid>`. macOS uses `~/Library/Application Support/ShellBeam` and `/tmp/shellbeam-<uid>`.
