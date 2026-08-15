# ShellBeam A2.3 Bounded Output Views Design

Date: 2026-08-15
Status: approved for execution by continuation mandate
Scope: A2.3 output views only

## 1. Purpose

ShellBeam already retains an append-only canonical combined byte stream per session and exposes cursor-based start/poll previews. The missing A2.3 capability is explicit bounded rereading, slicing, rendering, and searching of retained output without rerunning the child or copying output into a second truth store.

This slice promotes `FeatureOutputViews` only after the complete `read_output` path is available through the existing single `local_shell` tool.

## 2. Non-goals

A2.3 does not:

- change receipt or raw cursor authority;
- create a second MCP tool;
- persist rendered/search indexes or duplicate output blobs;
- turn output views into execution evidence;
- add background output processing;
- add arbitrary unbounded grep;
- add stdout/stderr ordering claims that the canonical combined stream cannot prove;
- alter E21/E22/E29/A4/A5 authority or provider behavior.

## 3. Authority and retention model

`sessions/<session_id>/output.log` remains the canonical retained byte stream. Receipt `output_bytes` and `output_complete` remain terminal execution facts. Session metadata determines whether retained output is still available after compaction.

A view is a mechanical projection over retained bytes. It is never persisted as canonical state and never rewrites cursor or receipt data.

Retention states are explicit:

- `retained`: canonical bytes for the requested session are retained; zero-byte output is valid;
- `compacted`: the session is known but canonical output retention was intentionally removed;
- `unavailable`: the session/output authority cannot be resolved safely;
- selector-specific `out_of_range`: the requested byte/line domain does not exist in the retained cut.

Compacted output never falls back to receipt metadata as fabricated content.

## 4. Public action

`local_shell` keeps one closed union and adds the supported branch:

```json
{
  "action": "read_output",
  "session_id": "...",
  "selector": { "kind": "raw_range", "start_byte": 0, "max_bytes": 4096 },
  "continuation": "..."
}
```

`continuation` is accepted only for selectors that require iterative bounded work. Unknown or mismatched continuations fail closed.

Exactly one selector object is required. Selector kinds are:

1. `raw_range`
2. `tail`
3. `lines`
4. `preview`
5. `search`

The response contains one bounded `output_view` object rather than overloading execution `receipt.Result`.

## 5. Selector contracts

### 5.1 Raw range

Fields:

- `start_byte` >= 0
- `max_bytes` within the advertised hard cap

Returns exact retained bytes rendered as valid model text plus raw byte accounting. Invalid UTF-8 is replaced only in model-visible text; raw offsets remain byte offsets into canonical output. The selector never silently reads past the configured result byte ceiling.

### 5.2 Tail

Fields:

- exactly one of `bytes` or `lines`
- requested count within advertised cap

Byte tail uses canonical byte offsets and returns a UTF-8-safe visible projection. Line tail scans backward in bounded chunks and never reads more than the advertised work ceiling. If determining the requested line tail would exceed the work ceiling, the response is partial with a continuation instead of scanning the whole file.

### 5.3 Lines

Fields:

- `start_line` >= 1
- `max_lines` within advertised cap

Logical lines are split on LF. CR bytes remain part of raw content and may be collapsed only by rendered selectors. The implementation scans from a continuation-bound byte offset rather than maintaining a persistent line index. Work is bounded; partial traversal returns continuation.

### 5.4 Preview

Fields:

- `head_bytes`
- `tail_bytes`

The preview deterministically prefers the beginning and end of retained output, with an explicit omission marker when bytes are skipped. ANSI control sequences are stripped mechanically from the rendered text, invalid UTF-8 is replaced, and carriage-return progress frames are collapsed. Binary-like output is summarized instead of emitting large unsafe/control-heavy payloads.

Preview rendering never changes canonical raw offsets.

### 5.5 Search

Fields:

- `mode`: `literal` or `regex`
- `pattern`: bounded non-empty UTF-8 text
- `case_sensitive`: boolean
- `max_matches` within cap

Search is line-oriented. Regex uses Go RE2 syntax and is applied to one bounded logical line at a time; cross-line matches are intentionally unsupported. Each call scans at most the advertised byte/work ceiling. Oversized logical lines are handled with explicit bounded semantics and never allocate unbounded memory.

Each match returns:

- one-based line number when known;
- raw byte range for the matched line/match;
- bounded rendered excerpt;
- match truncation metadata when applicable.

If more retained bytes remain after the bounded scan, the response includes an opaque continuation bound to session identity, selector parameters, retained-cut identity, and next scan position.

## 6. Continuation integrity

Continuation is server-issued and opaque. It must be:

- versioned;
- integrity protected using the existing local cursor-key pattern;
- bound to session ID;
- bound to selector kind and normalized selector parameters;
- bound to the retained output cut used to issue it;
- bounded in encoded size.

A continuation from another session/selector/cut returns invalid input. If live output grows, an existing continuation continues against its frozen retained cut; a new read may observe the later cut. Compaction makes continuation resolution explicitly unavailable rather than rebinding it.

## 7. Application architecture

Add a focused `internal/app/outputview` package with:

- closed request/selector/result contracts;
- validation and hard limits;
- deterministic selector execution;
- continuation codec;
- rendering helpers kept small and independently tested.

The package depends on a narrow store port that exposes retained output metadata and bounded range reads. It does not import IPC/MCP adapters.

The store adapter adds bounded output-inspection primitives over the existing `output.log`; it does not persist new indexes or views.

`daemonActions` composes the output-view service. IPC and MCP adapters map the `read_output` branch to the same application request/result.

## 8. Limits and capability discovery

Capability limits add explicit output-view ceilings, including at minimum:

- max returned bytes;
- max work/scan bytes;
- max lines;
- max matches;
- max pattern bytes;
- max continuation bytes.

`FeatureOutputViews` remains unavailable until the daemon composition, IPC branch, MCP input/output schemas, discovery, and acceptance tests are all complete.

## 9. Error semantics

Stable failures include:

- invalid selector / conflicting selector fields;
- session unknown;
- output compacted;
- output unavailable;
- byte or line out of range;
- invalid/expired/mismatched continuation;
- invalid regex;
- configured limit exceeded.

The response never maps an output-view failure onto child success/failure. Existing receipts remain unchanged.

## 10. Performance and no-tax invariant

When `read_output` is not requested:

- no output-view service scan runs;
- no output search/index work runs;
- no continuation is encoded/decoded;
- no new file is opened by ordinary start/poll because of A2.3;
- no background goroutine is added for output views.

Explicit view work is bounded by byte/work/record ceilings. No selector reads the whole retained stream merely because it is small enough in the current test fixture; the implementation follows the same bounded path for all sizes.

The existing typed-command warm-binding budget remains unchanged.

## 11. Security and privacy

Output can contain secrets, so A2.3 is retrieval, not broader publication authority. It preserves existing local-user authority and retention policy.

The implementation:

- never introduces public deterministic hashes of arbitrary output as identifiers;
- never copies raw output into event/evidence metadata;
- keeps continuation payload opaque and integrity protected;
- bounds excerpts and error details;
- does not infer safety from content;
- does not expose arbitrary host filesystem paths.

## 12. Acceptance

A2.3 is complete only when all of the following are proven:

1. `read_output` is no longer deferred in IPC/MCP and is reachable through exactly one `local_shell` tool.
2. Raw range preserves canonical byte offsets and UTF-8-safe model rendering.
3. Tail-by-byte and tail-by-line are bounded and deterministic.
4. Line range can continue across a bounded scan without a persistent line index.
5. Preview deterministically renders head+tail and never mutates raw cursor authority.
6. Literal and RE2 line search are bounded, continuation-safe, and reject malformed/mismatched continuations.
7. ANSI, invalid UTF-8, CR progress, binary-like bytes, huge lines, empty output, and mixed ordinary output have regressions.
8. Compacted, unavailable, and out-of-range semantics are distinct.
9. Capability discovery advertises truthful limits and `output_views=available` only after full wiring.
10. Ordinary start/poll no-tax regression proves no output-view scan/search side effect when unused.
11. Production schema validation and bridge/IPC integration pass with unknown-field rejection.
12. Relevant race, `devctl check`, dirty tests, and diff checks pass; `.codegraph` remains untouched.

## 13. Rollout boundary

This spec intentionally stops at A2.3. Evidence ledger/expected outputs, environment fingerprint/process inspection, mutation scopes, and B1 named sessions receive separate modern specs/plans so their authority and lifecycle semantics cannot leak into output retrieval.
