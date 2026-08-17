# Rich Local Media Parser / Toolchain Gate Evidence

## Verdict

```text
parser_toolchain_status = PASS
parser_toolchain_pass = true
candidate = go1.26-pinned-json-library-boundary
mode = library-boundary
GOEXPERIMENT = <empty>
module = github.com/go-json-experiment/json@v0.0.0-20260623181947-01eb4420fa68
source_commit = be54aee89e9c47b95aa15c709ca5d88ed310215f
```

The parser boundary amendment replaces the rejected whole-build Go 1.26 JSON-v2 experiment with an exact pinned library used only behind ShellBeam's reviewed `jsonstrict.Decode` boundary. Protocol-v1 decoding remains unchanged. Global `GOEXPERIMENT` is required to be empty.

The earlier `go1.26-jsonv2-experiment` evidence remains historical FAIL evidence: it changed JSON APIs process-wide and broke `go.lsp.dev/protocol v1.0.1`. It is not used by this PASS.

## Strict tracer

Canonical macOS tracer: `docs/superpowers/evidence/reports/parser-go126-library-boundary/strict-tracer-report.json`

- five ambiguity classes: **5/5 PASS**
- frozen modern-v2 semantic corpus: **18/18 PASS**
- frozen legacy-v1 corpus: **4/4 PASS**
- candidate identity: `go1.26-pinned-json-library-boundary`
- exact module: `v0.0.0-20260623181947-01eb4420fa68`
- global `GOEXPERIMENT`: empty

A second Linux-native tracer ran the same fixture manifest and also passed; its platform-specific report is `docs/superpowers/evidence/reports/parser-go126-library-boundary/strict-tracer-linux-report.json`.

## Candidate matrix

Both native lanes executed the same exact committed source `be54aee89e9c47b95aa15c709ca5d88ed310215f`.

| Check | Status | Provenance |
|---|---|---|
| five-rejections | PASS | strict tracer |
| valid-v2-semantic | PASS | strict tracer |
| legacy-v1 | PASS | strict tracer |
| error-code-compat | PASS | macOS + Linux race logs |
| ordinary-build | PASS | native macOS + Linux builds |
| full-tests | PASS | `go test ./... -count=1` on macOS + Linux |
| race | PASS | IPC/MCP/contract race on macOS + Linux |
| macos-native | PASS | `macos-native.json` |
| linux-native | PASS | `linux-native.json` |
| shell-acceptance | PASS | built `shellbeam version` on both native lanes |
| non-media-json-regression | PASS | LSP package compiles/tests normally on both lanes with no global experiment |
| mode-consistency | PASS | Go 1.26.5, module `v0.0.0-20260623181947-01eb4420fa68`, empty `GOEXPERIMENT`, same source commit on both lanes |

Supplementary mandatory checks also passed on both lanes: hardening and security. `govulncheck` remains an optional NOT_RUN inside the security script when not installed; the script's mandatory checks passed.

## Native platform provenance

### macOS

- Go: `go1.26.5 darwin/arm64`
- kernel: Darwin 25.6.0
- CGO: enabled
- native execution: yes
- build + shell runtime: PASS

### Linux

- Go: `go1.26.5 linux/arm64`
- kernel: LinuxKit 6.12.76
- Docker Desktop server: 29.4.1
- image: `golang:1.26.5-bookworm`
- image digest: `sha256:53eeac89074db483fdf0ab3be1df32bf6e47562263d2d0d6baa7f26acb4957dd`
- authoritative test user: UID 1000 `runner`
- native Linux execution: yes; this was not a cross-build
- build + shell runtime: PASS

An initial diagnostic container run as UID 0 was deliberately excluded from scoring because root bypasses the repository's read-only permission-failure fixtures; that run also lacked `ripgrep`. The complete matrix was rerun as non-root UID 1000 with `lsof` and `ripgrep`, and every row passed.

## Derived decision

```text
go127_ga = false
candidate = go1.26-pinned-json-library-boundary
parser_toolchain_pass = true
parser_toolchain_status = PASS
```

This closes the parser/toolchain half of the pre-implementation conjunction. It does **not** convert Phase A to PASS. The top-level media authorization remains blocked until the unchanged fresh-conversation/host-UI Phase A scorecard passes and the machine checker itself returns exit `0`.
