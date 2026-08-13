<p align="center"><img src="assets/icon.png" width="120" alt="ShellBeam icon"></p>

# ShellBeam

ShellBeam gives ChatGPT one MCP tool, `local_shell`, backed by a persistent per-user daemon on macOS and Linux. ChatGPT remains the only reasoning agent. ShellBeam owns command sessions, retry keys, bounded input/output, process groups, and durable receipts; it is not an agent or sandbox.

## Build and test

Requirements: Go 1.26.5, Git, a POSIX shell, and macOS or Linux.

```sh
go mod verify
go test ./...
go build -trimpath -buildvcs=false -o ./shellbeam ./cmd/shellbeam
./shellbeam version --json
./shellbeam doctor --json
```

Source ZIPs intentionally omit `.git`, so the command uses `-buildvcs=false`. In a real Git checkout, release automation uses `-buildvcs=true` and explicit version/commit linker values.

Run the daemon in one terminal:

```sh
./shellbeam daemon
```

The MCP target command for OpenAI Secure MCP Tunnel is the absolute path to:

```text
/absolute/path/to/shellbeam mcp
```

The bridge uses stdio MCP and connects to the daemon through a `0700` runtime directory and `0600` Unix socket with same-UID peer authentication. There is no loopback TCP listener.

## Per-user service

`shellbeam install` manages only the ShellBeam daemon through systemd-user on Linux or launchd on macOS. It does not install, start, configure, or store credentials for OpenAI `tunnel-client`.

```sh
./shellbeam install
./shellbeam status
./shellbeam doctor --json
./shellbeam uninstall
```

Uninstall preserves config, session state, output, receipts, and tombstones. Read [quickstart](docs/quickstart.md), [security model](docs/security.md), and [tunnel acceptance guide](docs/testing/tunnel-e2e.md) before connecting ChatGPT.

## Execution truth

An MCP success response proves only that the tool action completed. Command success requires a durable terminal receipt with successful spawn evidence, a reaped zero exit, complete output drain, and fully delivered accepted input. `abandoned/ambiguous` means ShellBeam cannot prove whether external effects happened; never automatically repeat it with a new operation ID.

## Current verification scope

The source package carries `release-evidence.json`. Check it rather than assuming a cross-build is a native test. Credentialed Secure MCP Tunnel/ChatGPT tests and native tests on another OS are explicitly marked `NOT_RUN` or `BLOCKED` until executed there.
