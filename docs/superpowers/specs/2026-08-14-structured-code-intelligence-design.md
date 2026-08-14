# ShellBeam Structured Code Intelligence Design

## Status

Review-ready E29 companion to the [Agent Execution Observation Roadmap](./2026-08-14-agent-execution-observation-roadmap-design.md). The scope was approved conceptually during the E21-E29 architecture review; this written revision remains subject to the design-review gate. This document defines provider contracts and reference integrations; it does not make any third-party library a permanent public dependency.

## 1. Decision

Add **E29 Structured Code Intelligence** as a bounded read-only fact/query capability behind the existing one-tool `local_shell` surface.

E29 has two promoted core slices and one deferred family:

- **E29A Semantic Diagnostics:** workspace/changed-file syntax, type, compiler-front-end, and analyzer diagnostics from a language-semantic provider.
- **E29B Semantic Navigation:** symbols, definition, references, imports, type information, and provider-supported call hierarchy using a small model-oriented vocabulary.
- **E29C Generic AST query/transformation:** explicitly deferred. ShellBeam does not expose arbitrary AST node traversal, rewrite DSLs, rename/refactor mutation, or a universal compiler model.

The reasoning agent remains the only component that decides what code change to make. E29 supplies structured code facts that remove repetitive grep/open/compile loops.

## 2. Goals

- Give a coding agent cheap semantic feedback before an expensive build/test loop.
- Reuse mature language tooling rather than implement parsers/type checkers/indexers in ShellBeam core.
- Normalize common navigation/diagnostic facts without erasing provider/build-configuration limitations.
- Keep provider lifecycle, LSP/JSON-RPC protocol choreography, document synchronization, and structural parser internals invisible on the normal model-facing path.
- Support saved on-disk workspace source first; make any future editor-overlay source view explicit and separately fingerprinted.
- Start providers lazily and keep ordinary `local_shell start` free of code-intelligence startup/indexing cost.
- Preserve the core product boundary: semantic observation is not test/build evidence and code intelligence never edits source by itself.

## 3. Non-goals

E29 does not provide:

- an AI code agent or fix recommender;
- authoritative build/test success;
- a replacement for E22 execution-derived structured results;
- automatic code actions, rename, formatting, import rewrite, or refactoring;
- arbitrary LSP methods exposed directly to the model;
- editor buffer ownership or unsaved-buffer synchronization in v1;
- a generic AST node-ID API;
- a Tree-sitter query DSL or ast-grep rewrite surface exposed as core;
- automatic language-server/indexer installation;
- automatic SCIP generation in the normal edit loop;
- cross-language semantic equivalence claims where providers differ.

## 4. Authority and evidence boundary

E29 semantic observations are mechanically produced under a versioned provider/input contract, but they are **not execution evidence**.

A provider may report:

```text
workspace semantic diagnostics = zero errors
```

without proving:

```text
go build succeeds
go test succeeds
release gate passes
```

Compiler/test/linter results produced by an actual admitted command remain E22/execution/evidence facts. E29 may identify an obvious undefined name before tests run; it cannot mark a verification profile current.

Every result therefore carries enough provenance to distinguish:

```text
source_kind: semantic_provider | structural_provider | index_provider
provider_id/version
provider_config_fingerprint
source_view
source_generation/content identity quality
build_config_fingerprint/quality when relevant
freshness
completeness/scope
```

A provider fact with stale/partial/unknown source or build configuration is never upgraded to global mechanical certainty.

## 5. Agent ergonomics: the model never speaks LSP or AST internals

The normal agent-facing contract uses a bounded vocabulary such as:

```text
diagnostics
symbols
definition
references
imports
type_info
callers
callees
```

The agent never has to supply or maintain:

```text
initialize/initialized
textDocument/didOpen
textDocument/didChange
publishDiagnostics
URI/document versions
JSON-RPC request IDs
LSP capability negotiation details
Tree-sitter node IDs/field IDs
SCIP protobuf occurrence encoding
provider process PIDs
```

Provider adapters translate model-oriented queries to the underlying protocol. Recovery/restart/index refresh is server-driven. If one code-intelligence query normally needs multiple hidden LSP calls, that is an adapter implementation detail, not multiple model tool calls.

## 6. One-tool query surface

E29 remains a branch of the closed `local_shell` inspect/query family; it does not add a second MCP tool.

Conceptually:

```json
{
  "action": "inspect",
  "target": "code",
  "workspace_id": "ws_...",
  "query": {
    "kind": "diagnostics",
    "scope": "changed_files"
  }
}
```

or:

```json
{
  "action": "inspect",
  "target": "code",
  "workspace_id": "ws_...",
  "query": {
    "kind": "definition",
    "path": "internal/app/service.go",
    "line": 81,
    "column": 12
  }
}
```

Positions use one documented ShellBeam coordinate convention. The adapter performs any LSP UTF-16/UTF-8/byte-position translation and validates the exact source content used for translation; the model does not reason about provider-specific coordinate encodings.

Every query is bounded by response bytes, record count, work/time, and provider-specific concurrency budgets.

## 7. E29A Semantic Diagnostics

Diagnostics are normalized into the same model-facing diagnostic shape used by E22 where fields overlap:

```text
path
range
severity
code?
message
provider_source
related_locations[]
authority
freshness
source_generation
provider/build_config refs
```

Provenance still distinguishes `gopls`, compiler command output, Pyright, clangd, rust-analyzer, SARIF, or other producers. Schema reuse is presentation/interoperability, not evidence equivalence.

Initial scopes:

```text
file
changed_files
workspace
```

`changed_files` is particularly important for the coding loop. ShellBeam derives the candidate file set from its bounded workspace observation/baseline contract; the semantic provider evaluates those files under its own supported scope. If the provider necessarily analyzes more packages/files internally, that is fine, but the returned model-facing result remains scoped/bounded.

A typical response is deliberately compact:

```text
status: ready
freshness: current
errors: 2
warnings: 1
files: 2
records:
  internal/foo.go:73:9  UndefinedName  undefined: ServerInfo
  internal/bar.go:19:2  TypeMismatch   cannot use X as Y
```

Detailed provider provenance is drill-down metadata rather than default token cost.

## 8. Diagnostic lifecycle and Event Journal integration

Semantic diagnostics use the A3a deterministic derived-record lifecycle:

```text
pending -> processing -> terminal
```

A workspace generation/source change makes a previously bound semantic snapshot stale unless the provider proves it has advanced to the new source identity. Provider push notifications are adapter input, not public truth by themselves.

When a new bounded diagnostic summary is materialized, E21 may emit:

```text
code_diagnostics_changed
```

with counts/provider/result reference only. The journal event does not duplicate diagnostics and does not make the semantic result execution evidence.

Agent happy path after editing can therefore be:

```text
edit source
  ↓
inspect code diagnostics(scope=changed_files)
  ↓
fix obvious syntax/type issues
  ↓
run affected verification
```

No background diagnostic event is allowed to block an ordinary command or force the agent into a polling protocol.

## 9. E29B Semantic navigation

Promoted v1 semantic queries are:

### 9.1 `symbols`

Returns bounded document/workspace symbols with name, kind, path/range, and provider provenance. Workspace fuzzy-search semantics remain provider-specific and are labeled accordingly.

### 9.2 `definition`

Given a source position bound to an exact/known source view, returns zero or more declaration locations. Multiple locations are represented honestly; ShellBeam does not pick one semantically.

### 9.3 `references`

Returns bounded references plus scope/build-configuration quality. A provider that only sees one build configuration cannot call the result globally complete.

### 9.4 `imports`

Returns mechanically observed import/module relationships for a file/package where the provider supports them. A syntax-only provider may satisfy this query with lower semantic authority when explicitly labeled.

### 9.5 `type_info`

Returns bounded provider-supplied symbol/type/signature facts. It does not synthesize a universal type model across languages.

### 9.6 `callers` / `callees`

Optional capability mapped to provider call hierarchy where available. Unsupported/partial call hierarchy is explicit and never approximated from text grep while labeled semantic.

## 10. Source-view contract

E29 v1 is **disk-bound**: its canonical input is the saved workspace source view ShellBeam can observe locally.

A provider session is bound to:

```text
workspace_id
repository_id?
source generation/content identity quality
provider config fingerprint
build configuration fingerprint/quality
provider executable/version
```

Provider adapters are responsible for synchronizing disk changes into their protocol model and for proving which source generation a response corresponds to.

Unsaved editor-buffer overlays are deferred. If a future attached editor/LSP integration exposes them, the overlay becomes a distinct source-view identity with exact content/version provenance. It can never silently overwrite or be equated with disk/evidence truth.

## 11. Provider freshness and source races

A code-intelligence request captures a source-view observation before issuing provider work. The response must be associated with the same source identity or downgraded:

```text
current
stale
source_changed_during_query
unknown
```

If the workspace changes while a definition/reference/diagnostic query is in flight and the adapter cannot prove the returned result belongs to the new source, ShellBeam returns the old result as stale or retries once within budget; it never relabels it current.

Provider caches/indexes have separate readiness states:

```text
unavailable
starting
indexing
ready
stale
failed
```

These are observation states, not shell execution gates.

## 12. Provider lifecycle

Semantic providers are lazy, persistent, workspace-scoped runtime helpers. They are not agent-managed shell sessions.

Lifecycle rules:

- ordinary `local_shell start` never launches a language server;
- first explicit E29 query may start the selected provider or return `starting/indexing` within the query budget;
- a compatible warm provider is reused across queries and source generations according to its protocol;
- provider instances are keyed by workspace plus provider executable/config/build-environment compatibility, not merely language name;
- idle TTL/capacity eviction is bounded and does not affect operation idempotency/evidence;
- provider crash/restart makes affected result derivations stale/pending and never damages canonical execution state;
- ShellBeam owns the provider subprocess only under its existing process ownership rules and never reconstructs signal authority from stale PIDs.

A provider that has significant cold indexing cost may complete asynchronously as provider state; the model receives `starting/indexing` instead of blocking an unrelated command.

## 13. Provider side-effect profile

Language servers are not pure parsers. A provider contract declares possible local/external effects such as:

```text
workspace_read
local_cache_write
child_tool_execution
module/dependency_metadata_access
network_possible
telemetry_possible
```

These are provider capability facts. E29 core does not grant new user authority, install tools, or hide side effects behind an ordinary shell start.

For example, a Go semantic provider may execute the configured `go` command for workspace/package metadata and may populate language/tool caches. Provider startup is therefore lazy/explicit and its side-effect profile is visible through capability/readiness inspection.

A provider must not directly apply source edits under E29 even if the underlying language server offers code actions/refactorings. Any future source mutation support requires a separate reviewed contract.

## 14. Reference semantic transport: LSP 3.18

The normative semantic-provider abstraction is **not** tied to one library, but LSP 3.18 is the preferred reference transport because mature language servers already expose diagnostics/navigation over it.

The ShellBeam adapter owns:

- initialize/capability negotiation;
- JSON-RPC framing/request IDs/cancellation;
- document/workspace synchronization for the disk source view;
- provider diagnostic notifications/pull requests;
- position/URI encoding conversion;
- restart/timeout/error translation;
- query-specific capability checks;
- source-generation/provenance binding.

None of those protocol details appear in the ordinary model-facing schema.

For the Go implementation, evaluated library candidates are:

```text
go.lsp.dev/protocol v1.0.1
go.lsp.dev/jsonrpc2  v1.0.1
```

At the design date both target Go 1.26, matching ShellBeam's current Go 1.26.5 toolchain. They are **reference implementation candidates, not public architectural dependencies**; implementation planning must review API stability, transitive cost, cancellation/framing correctness, and benchmark impact before adding them to `go.mod`.

If these libraries do not satisfy the contract, ShellBeam may use another standards-compliant LSP/JSON-RPC adapter without changing E29 public semantics.

## 15. Initial semantic provider: gopls over LSP

The first reference provider should be Go's official `gopls` language server over stdio LSP.

Reasons:

- ShellBeam itself is Go, giving a native dogfood path;
- gopls already exposes diagnostics, definition, references, document/workspace symbols, type/navigation/call-hierarchy features;
- semantic analysis comes from Go parser/type-checker/analyzers rather than ShellBeam reimplementing them;
- a persistent server amortizes package loading/indexing across edit-loop queries.

The provider must record relevant gopls/build configuration because semantic completeness depends on the analyzed build configuration. Reference results are not described as complete across excluded GOOS/GOARCH/build-tag configurations unless the provider contract explicitly covers them.

### 15.1 Do not use the gopls CLI as the production adapter

The gopls command-line feature is a debugging/scripting interface with weaker efficiency/stability guarantees than the LSP server. It may be useful during development probes, but E29 production semantics target LSP.

### 15.2 gopls MCP is an experimental prototype option only

Modern gopls also exposes an experimental MCP server for AI assistants, including detached mode backed by a headless LSP session. ShellBeam may spike this as a development/provider experiment, but it is **not** the normative E29 foundation because:

- the gopls MCP tool vocabulary is provider-specific rather than cross-language;
- its MCP surface is explicitly experimental;
- nesting provider MCP semantics beneath ShellBeam would complicate capability/error/provenance normalization;
- LSP is the more stable semantic interoperability boundary.

If a future gopls MCP surface becomes stable and gives a measurable implementation/latency advantage, it can be an adapter behind the same E29 contract without exposing another MCP tool to ChatGPT.

## 16. Structural code facts without self-implementing an AST engine

Syntax-only structural facts are useful when no semantic provider exists or when a cheap outline/import/function inventory is enough. They remain distinct from semantic/type facts.

E29 does **not** embed a universal AST engine in core.

### 16.1 Preferred first structural provider: ast-grep JSON

The recommended first structural integration is an external read-only `ast-grep` provider using its machine-readable JSON/streaming output. ShellBeam can reuse ast-grep's Tree-sitter grammar/query machinery instead of implementing parser grammars and AST matching itself.

Promoted structural operations are intentionally boring/model-oriented:

```text
outline
functions/types declarations
imports
bounded structural matches for a ShellBeam-defined query family
```

Core does not expose arbitrary ast-grep rewrite/fix options, YAML rule execution supplied by untrusted callers, or raw Tree-sitter node traversal. `--json=stream`-style processing lets ShellBeam enforce output/record budgets without loading an unbounded result array.

`ast-grep` is an optional executable provider discovered/readiness-checked like other tools. ShellBeam never auto-installs it.

### 16.2 Lower-level fallback: Tree-sitter

If a future capability cannot be expressed safely through ast-grep, the official Tree-sitter Go binding is a candidate lower-level provider implementation. Direct embedding is not the default because it would make ShellBeam own grammar lifecycle, native/C binding concerns, node/query lifetime, and language packaging.

Tree-sitter results are syntax/structure observations. They cannot infer semantic definitions/references/types merely because identifiers have matching text.

## 17. Optional persistent index provider: SCIP

SCIP is an optional language-agnostic index interchange for definitions/references/implementations. E29 may consume a `.scip` index when one already exists or is explicitly produced by a project/index provider.

Core rules:

- index identity is bound to exact source/index content identity, indexer/version/config, and project root;
- an index for a mismatched source digest is stale, never silently reused as current;
- index parsing/lookup is bounded/streaming where possible;
- ordinary edit-loop queries never trigger full SCIP index generation automatically;
- absence of SCIP never disables LSP or structural providers;
- large/cross-repository indexing remains provider/B2 work, not a hidden core database.

SCIP is therefore a scale/caching option, not a prerequisite for E29A/E29B.

## 18. Provider selection

Provider choice is deterministic and non-reasoning. Precedence is conceptually:

1. validated project/provider configuration when present;
2. explicit caller selection among advertised providers;
3. one unambiguous built-in language default, such as `gopls` for a Go workspace, when available;
4. otherwise `code_intelligence_provider_unavailable`/ambiguous status.

ShellBeam does not choose a semantic provider by regexing logs or downloading a server. If semantic and structural providers both answer a query, their results remain separately attributed; ShellBeam does not fuse conflicting facts into a new semantic conclusion.

## 19. Capability discovery

A bounded capability example:

```text
code_intelligence:
  version: 1
  source_views: [disk]
  providers:
    go_semantic:
      kind: lsp
      executable: gopls
      status: available
      queries: [diagnostics, symbols, definition, references, imports, type_info, callers, callees]
      side_effect_profile: [workspace_read, local_cache_write, child_tool_execution, network_possible]
    structural:
      kind: astgrep
      status: optional_available
      queries: [outline, imports, structural_facts]
    scip:
      kind: index
      status: unavailable
      queries: [definition, references, implementations]
limits:
  query_ms
  records
  response_bytes
  provider_instances
  provider_idle_ttl_ms
```

Default inspect responses summarize provider readiness and useful result quality. Full side-effect/config/protocol details are drill-down metadata.

## 20. Readiness and installation boundary

E25 readiness may report code-intelligence prerequisites/provider availability, for example:

```text
gopls: available / version observed
ast-grep: optional missing
SCIP index: absent
```

Readiness never installs a language server, downloads an indexer, runs a project bootstrap, or starts the provider merely to turn a status green. Provider-specific version probes are cached/bounded under the existing readiness/toolchain rules.

A missing semantic provider does not block normal shell work. The agent can choose to continue with execution-derived diagnostics, structural fallback, or ordinary source inspection.

## 21. Performance and capacity budgets

E29 must improve coding-loop economics rather than make every command pay IDE costs.

### 21.1 Ordinary shell path

With E29 supported but unused:

- zero language-server/ast-grep/SCIP subprocesses;
- zero indexing/query work;
- zero network attributable to E29;
- zero source-file reads solely for code intelligence on command admission.

This path is covered by the umbrella global incremental admission benchmark.

### 21.2 Explicit code-intelligence query

Every query has:

```text
startup/query wall budget
max result records
max response bytes
provider concurrency limit
cancellation deadline
```

A cold semantic provider may return `starting/indexing` plus a retryable observation handle instead of blocking beyond budget. A warm provider query should be measured independently per language/reference repository; numeric language-specific p95 targets are frozen in implementation planning only after baseline benchmarks because provider cold/warm costs differ materially.

### 21.3 Persistent provider capacity

Provider instances are bounded globally/per-workspace with idle TTL/LRU-like eviction that never kills an instance while an owned request is executing. Eviction affects only semantic-observation cache/lifecycle, not command sessions or evidence.

## 22. Privacy and security

E29 provider processes may read source code locally because semantic analysis requires it. Public/model-facing records remain bounded facts and do not echo arbitrary source contents merely because the language server returned hover/documentation text.

Rules:

- diagnostic messages/signatures/docs are bounded and source snippets are omitted by default;
- external/home/system paths are classified/redacted under the workspace privacy contract;
- raw environment values/credentials are never copied into code-intelligence records;
- provider side-effect/network capability is visible before explicit startup where practical;
- E29 never auto-applies source edits/code actions;
- provider subprocess ownership follows ShellBeam process safety rules;
- untrusted repository configuration cannot cause ShellBeam to execute an arbitrary “language server” path without the same validation/authorization boundaries used for project capabilities;
- index/result files are treated as untrusted structured input with parser size/depth/record budgets.

## 23. Failure/status model

Stable E29 observation conditions include:

```text
code_intelligence_provider_unavailable
code_intelligence_provider_ambiguous
code_intelligence_provider_starting
code_intelligence_provider_failed
code_intelligence_query_unsupported
code_intelligence_query_budget_exceeded
code_intelligence_result_partial
code_intelligence_result_stale
code_intelligence_source_changed_during_query
code_intelligence_build_config_incomplete
code_intelligence_position_invalid
code_intelligence_index_stale
code_intelligence_index_unavailable
```

These are observation/tool statuses. They do not rewrite a child command's outcome and do not make an arbitrary `local_shell start` fail.

A provider protocol/internal error is normalized into bounded stable status plus deep diagnostic details; raw LSP/JSON-RPC error objects are not exposed as public error-code vocabulary.

## 24. Diagnostics versus execution-derived structured results

E29 and E22 intentionally share a presentation shape but not authority semantics:

```text
E29 semantic provider
  current workspace model
  fast edit-loop diagnostics/navigation
  may depend on one build configuration/provider cache
  not verification evidence

E22 execution-derived result
  exact admitted command/receipt/output/artifact provenance
  terminal operation fact
  may contribute to evidence under validated policy
```

When both report the same issue, ShellBeam may present them side-by-side/dedupe model-facing display only if provenance remains separately addressable. It does not create a new “more authoritative because two tools agree” fact.

An E29 diagnostic disappearing after an edit does not invalidate historical E22 diagnostics from an earlier operation; each remains tied to its own source/capture identity.

## 25. Structural versus semantic facts

Provider kinds have explicit upper bounds on what they may claim:

- `semantic_provider`: may report types/definitions/references under its documented analysis/build scope;
- `structural_provider`: may report syntax tree structure/import/declaration/match facts but cannot infer semantic symbol identity from text coincidence;
- `index_provider`: may report relationships encoded by the exact indexer/index contract and source identity, with completeness constrained by the indexer.

A structural outline fallback can answer “which functions are declared in this file?” but cannot answer “which `Foo` does this selector call?” as a semantic fact.

## 26. Event Journal and handoff integration

E21 may surface only bounded code-intelligence changes useful to a new agent:

```text
code_provider_state_changed
code_diagnostics_changed
```

A handoff/inspect summary may contain:

```text
Go semantic provider: ready/current @ generation G
changed-file diagnostics: 2 errors / 1 warning
last semantic query refs: ...
```

It never contains hidden editor conversation state or a narrative recommendation. A new agent can decide whether to inspect diagnostics, source, or run verification.

## 27. Validation strategy

### 27.1 Contract tests

- closed E29 query/result schemas reject unknown/malformed branches;
- model-facing requests contain no LSP URI/document-version/JSON-RPC fields;
- capability negotiation cleanly handles absent/partial providers;
- provider raw errors normalize to stable ShellBeam statuses;
- coordinates round-trip across ShellBeam and provider encodings including non-ASCII source.

### 27.2 Source/freshness tests

- result is bound to the source generation/content observation used for query;
- concurrent source edit during query yields stale/retry/downgrade, never falsely current;
- provider restart/index refresh does not reuse stale derivation as current;
- disk source view never silently incorporates editor-only overlay data;
- build-config-limited reference result advertises its scope/quality.

### 27.3 Go/gopls semantic tests

On native macOS and Linux reference repositories:

- undefined symbol/type mismatch appears through E29 before running a build/test command;
- definition/references/document symbols/type info return provider-backed locations/facts;
- changed-file diagnostics remain bounded;
- gopls cold start/indexing and warm reuse/cancellation/restart are exercised;
- provider source/config/build fingerprints change when relevant configuration changes;
- gopls side-effect profile does not get triggered by unrelated `local_shell start`;
- no gopls CLI dependency is required by the production provider contract.

### 27.4 Structural-provider tests

For ast-grep-backed read-only structural facts:

- JSON/streaming parser obeys record/byte budgets;
- syntax-error-tolerant files return honestly scoped structure where supported;
- outline/import/function facts retain structural-provider provenance;
- no rewrite/fix mutation options are exposed through E29;
- absence of ast-grep falls back/unavailable without blocking shell or semantic provider;
- adversarial output/path/UTF-8 cases cannot escape response bounds.

### 27.5 SCIP/index tests when implemented

- exact index/source/indexer/config identity is required for `current`;
- stale index never silently answers as current;
- large index consumption is bounded/streamed;
- normal edit-loop query does not auto-generate an index;
- index absence does not degrade LSP provider availability.

### 27.6 Crash/capacity tests

- provider crash cannot corrupt operation/receipt/evidence state;
- query cancellation/timeout reclaims request resources;
- provider instance count/idle eviction remains bounded;
- no active provider request is killed by cache eviction;
- automatically derived semantic result uses deterministic derivation identity across retry/materialization.

## 28. Acceptance criteria

E29A/E29B are core-ready only when:

1. An agent can request changed-file semantic diagnostics in one model-oriented inspect call without running a build/test command.
2. Definition/references/symbols/imports/type information are available through a small provider-neutral vocabulary where the selected provider supports them.
3. The agent never speaks LSP/JSON-RPC, manages document versions/URIs, or traverses AST node IDs on the normal path.
4. Every result carries provider/source/build-config freshness/completeness sufficient to prevent stale or configuration-limited facts from being overclaimed.
5. E29 observations never become build/test/evidence truth merely by existing.
6. Provider startup/indexing/absence/failure never blocks ordinary shell execution.
7. Enabled-but-unused E29 performs zero provider/index/subprocess work on ordinary command admission.
8. The first Go provider uses mature semantic tooling rather than a ShellBeam reimplementation of Go parsing/type checking/navigation.
9. Structural fallback remains syntax-only/read-only and cannot masquerade as semantic symbol identity.
10. SCIP/index consumption, if present, is optional and exact-source-bound; automatic full indexing is not a normal-loop requirement.
11. Native macOS/Linux tests prove provider lifecycle, stale-source handling, cancellation, capacity, and bounded output.
12. E29 does not expose source-edit/refactor mutation; the reasoning agent remains the code editor.

## 29. Reference agent flows

### 29.1 Fast edit loop

```text
agent edits files
  ↓
inspect code diagnostics(scope=changed_files)
  -> 2 errors / 1 warning
  ↓
agent fixes obvious type/syntax mistakes
  ↓
run affected test project command
  ↓
E22 execution diagnostics/evidence as needed
```

### 29.2 Navigation without grep choreography

```text
inspect code definition(path,line,column)
  -> declaration location
inspect code references(path,line,column)
  -> bounded references + build-scope quality
```

### 29.3 Provider unavailable

```text
inspect code diagnostics
  -> provider_unavailable
  -> optional structural provider available
agent continues source inspection / command execution
```

No automatic installation or shell blockage occurs.

## 30. Reference technology stack and rationale

These are evaluated implementation recommendations, not normative public dependencies.

### 30.1 Semantic protocol

- Language Server Protocol 3.18: https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/
- Candidate Go protocol bindings: https://pkg.go.dev/go.lsp.dev/protocol
- Candidate Go JSON-RPC transport: https://pkg.go.dev/go.lsp.dev/jsonrpc2

### 30.2 Go semantic provider

- gopls: https://go.dev/gopls/
- gopls diagnostics: https://go.dev/gopls/features/diagnostics
- gopls navigation: https://go.dev/gopls/features/navigation
- gopls MCP experiment: https://go.dev/gopls/features/mcp

Production reference transport is LSP; gopls CLI/MCP may be development/prototype adapters only under the same provider boundary.

### 30.3 Structural provider

- ast-grep JSON/streaming output: https://ast-grep.github.io/guide/tools/json.html
- Tree-sitter: https://tree-sitter.github.io/tree-sitter/

Prefer ast-grep as the first external read-only structural provider so ShellBeam does not self-implement grammars/query machinery. Direct Tree-sitter embedding is lower-level fallback only after a demonstrated requirement.

### 30.4 Optional semantic index

- SCIP protocol: https://github.com/scip-code/scip

Prefer consuming exact-source-bound existing indexes. Automatic index generation/storage is deferred provider work.

### 30.5 Later language providers

Provider adapters may later target mature LSP implementations such as rust-analyzer, clangd, Pyright, or a suitable TypeScript language server. Each is independently capability-tested; E29 never assumes all language servers have identical diagnostic/reference/build-configuration semantics.

## 31. Deferred follow-ups

Separate reviewed designs are required before adding:

- E29C generic AST query DSL;
- AST/source transformation/refactoring/code actions;
- editor unsaved-buffer overlay authority;
- automatic cross-repository SCIP indexing;
- semantic provider auto-install/update;
- provider-generated fix recommendations;
- semantic facts used to narrow authoritative evidence validity.

None is required for the high-ROI E29A/E29B edit-loop capability.
