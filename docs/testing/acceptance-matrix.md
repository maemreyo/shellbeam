# V1 acceptance matrix

| Invariant | Test/evidence owner | Layer |
|---|---|---|
| Closed/versioned schemas | `TestSchemaInventory`, MCP protocol tests | contract |
| Start fingerprint excludes response tuning | `TestFingerprintExcludesResponseTuning` | core |
| Same operation starts once | `TestStartRetrySpawnsOnce`, store concurrency test | app/store |
| Reservation before spawn | store reservation tests + fake owner | app/store |
| Cursor/output quota | `TestOutputCursorAndLimit`, output UTF-8 tests | store/core |
| Bounded stdin retry/EOF | input-ledger tests, native write integration | core/native |
| Kill ID dedupe | `TestKillLedger` | core |
| Process group, pipe and PTY | process adapter tests | native |
| Reap/drain/input terminal receipt | `TestRealRuntimeStartWritePoll` | native integration |
| Daemon restart ambiguity | `TestAbandonUnresolved` | store integration |
| Unix socket/peer UID | IPC integration and Darwin cross-compile | native/compile |
| One MCP tool/metadata/defaults | `TestMetadata`, `TestInMemoryConformance` | SDK integration |
| Service templates | service adapter golden assertions | contract |
| Redacted diagnostics | `TestLoggerRedactsValues` | unit |
| Tunnel/ChatGPT behavior | `docs/testing/tunnel-e2e.md` | credentialed user-run |

