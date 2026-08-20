# ShellBeam Vitest/Jest Structured Results Design

Date: 2026-08-20
Status: design freeze for review; implementation planning is not yet authorized
Scope: add two artifact-backed JavaScript/TypeScript test-runner adapters, `vitest-json@v1` and `jest-json@v1`, on top of the already-deployed `StructuredInputRef` artifact-capture foundation, without weakening E22 raw-output identity, terminal receipt truth, P1 sufficiency, or filesystem provenance

Predecessor: `docs/superpowers/specs/2026-08-19-multilanguage-structured-results-design.md` (the "pytest design"). Section references written as `pytest §N` refer to that document.

## 1. Decision

ShellBeam SHALL add two artifact-backed structured-result adapters:

```text
vitest-json@v1
jest-json@v1
```

Each SHALL consume its own producer's built-in JSON result document, captured from an explicitly requested output file through the existing artifact-capture pipeline.

Both adapters SHALL use the existing `artifact_blob` branch of `StructuredInputRef`. No new input kind is introduced.

ShellBeam SHALL NOT inject `--reporter`, `--json`, or `--outputFile`, SHALL NOT install `jest-junit` or any reporter package, SHALL NOT parse runner console output, and SHALL NOT treat a mutable workspace pathname as parser authority.

The architecture is unchanged from the pytest design:

```text
qualified execution intent
        │
        ▼
ArtifactCaptureIntent            (frozen + durable before spawn)
        │
        ▼
vitest run / jest child execution
        │
        ▼
terminal Phase A                 (pin exact file object)
        │
        ├────────────► terminal receipt publication continues
        ▼
terminal Phase B                 (bounded immutable materialization)
        │
        ▼
private ArtifactBlob → ArtifactBlobRef
        │
        ▼
vitest-json@v1  OR  jest-json@v1
        │
        ▼
E22 test_case / test_suite records
        │
        ▼
existing P1 evidence/sufficiency plane
```

The delta relative to pytest V1 is therefore small and bounded: per-adapter invocation qualification, per-adapter strict JSON parser, per-adapter semantics coverage. Capture, blob storage, reference retention, compaction, recovery, and the P1 bridge are reused unchanged.

## 2. Evidence basis for this design

The pytest design states that later adapter formats are "intentionally not guessed" and that each adapter "SHALL either reuse a qualified existing `StructuredInputRef` kind or obtain an explicit design amendment" (pytest §4). This document is that qualification.

Every producer fact asserted below was verified before this freeze. Verification classes:

```text
[RUN]  executed locally and observed
[SRC]  read from published package source or upstream repository source
[DOC]  official documentation
[UNVERIFIED]  explicitly not established from a primary source
```

Producer versions used:

| Producer | Executed [RUN] | Source-only [SRC] | Not established |
|---|---|---|---|
| Vitest | `4.1.11`, `3.2.7` | `1.6.1`, `2.1.9`, `4.0.0`, `4.0.5`, `4.0.18`, `4.1.0`, `5.0.0-rc.2` | — |
| Jest | `30.4.2`, `29.7.0` | `main` (post-`30.4.2`), `30.2.0`, `30.3.0` | `28.x` |

Anything marked `[UNVERIFIED]` in this document SHALL be unqualified in V1. It SHALL NOT be handled by best-effort interpretation, and it SHALL NOT be silently accepted by the parser.

## 3. Retained contracts

This design composes with, and does not weaken, the following. All remain authoritative:

- MCP/tool transport success is not child execution success.
- Terminal child outcome comes from durable receipt/spawn/reap/exit/signal evidence.
- Structured results are deterministic projections of qualified machine-readable producer facts.
- Mechanical authority is never created by prose/message heuristics.
- Unknown or unavailable information cannot be converted into negative evidence.
- Structured-result parsing SHALL NOT mutate execution semantics.
- Structured-result detail SHALL NOT overwrite terminal receipt truth.
- Core `TestStatus` remains exactly `pass | fail | skip | error` (pytest §59).
- Producer-specific distinctions use `ProducerTestDisposition` / `ProducerSemanticsCoverage` (pytest §60, §65).
- One MCP tool, `local_shell`. No JavaScript/TypeScript-specific top-level tool.

## 4. What this design reuses without modification

The following pytest V1 mechanisms SHALL be reused as-is. This design introduces no changes to them:

```text
StructuredInputRef schema v2 (raw_output | artifact_blob)
ArtifactCaptureIntent + durable pre-spawn capture authority
pre-spawn descriptor-relative absent baseline
managed same-path collision registry
terminal Phase A ArtifactSourceHandle acquisition + 250ms bound
Phase B immutable materialization + source-stability rejection
ArtifactBlobRef identity, terminal_cut, observation_cut
private blob store, byte/store budgets, no implicit eviction
reference-aware retention, compaction, tombstones, orphan rules
startup recovery ordering (never re-executes the producer)
artifact resolver + bounded range reads
StructuredEvidenceDetail read bridge into P1
```

Reusing the capture layer unchanged is a hard requirement, not a convenience. Any adapter that needs a different capture contract is out of scope for this document and requires its own amendment.

## 5. Delivery scope and order

This document specifies both JavaScript/TypeScript test-runner adapters together because they share one qualification architecture and differ only in producer specifics.

Delivery order within this scope:

```text
1. shared JS/TS invocation-qualification + strict-profile parser scaffolding
2. jest-json@v1
3. vitest-json@v1
```

Jest is sequenced first because its payload is the more stable of the two across the versions verified here (§30) and because its qualification surface has fewer config-mediated redirection paths (§19 versus §25).

ESLint remains after this work (pytest §95). The TypeScript compiler remains deferred pending its own qualification (pytest §96).

## 6. Non-goals

V1 SHALL NOT:

- inject `--json`, `--reporter`, `--outputFile`, or any other producer flag;
- install or require any reporter package, including `jest-junit`;
- implement a generic "JavaScript test JSON" adapter;
- treat Vitest output and Jest output as one interchangeable format;
- implement effective Vitest or Jest config discovery;
- attest the exact installed Vitest/Jest distribution version;
- spawn `vitest --version` or `jest --version` as pre-execution tax;
- parse `message` / `failureMessages` / `retryReasons` prose to recover semantics;
- reconstruct hook phase, focus state, or flake state from text or from record shape;
- read the producer's own `success` boolean as any form of authority;
- recompute child success from normalized records;
- support watch-mode invocations;
- support multi-project/workspace invocations (§34);
- create a JS/TS-specific evidence ontology parallel to P1.

## 7. Input kind decision: artifact_blob, not raw_output

The pytest design left the next adapter free to use "raw terminal machine output; or artifact blob input" (pytest §94).

Both adapters SHALL use `artifact_blob`.

`raw_output` SHALL NOT be used, because for both producers stdout purity is not mechanically provable. Sections 8 and 9 record the verified evidence.

This is the same reasoning that made `RawOutputRef` correct for `go test -json`: Go emits a documented newline-delimited JSON event stream on a dedicated channel, so the terminal output range is the machine contract. Vitest and Jest instead emit a single JSON document onto a stream they share with arbitrary test-controlled writes. The shared-stream property is the disqualifier, not the choice of JSON.

## 8. Why Vitest stdout cannot be qualified

Verified on Vitest `4.1.11` and `3.2.7` [RUN]:

```text
--reporter=json alone
  → stdout is byte-for-byte pure JSON, stderr empty

--reporter=json --reporter=default
  → human report and JSON interleaved on the SAME stdout

test calls process.stdout.write(...)
  → raw bytes precede the JSON on stdout; JSON.parse fails

--outputFile=<path>
  → stdout still receives the human line "JSON report written to <absolute path>"
```

`JsonReporter` has no `onUserConsoleLog` hook, so intercepted `console.log` is dropped rather than emitted [SRC]. That closes one leak but not the raw-write leak.

Upstream documents the hazard directly for the v5 line: report output "can be interleaved with other output written directly to the terminal — for example `process.stdout.write` in a test file, or logs from the main process such as a global setup file — which can make the JSON or XML unparsable" [DOC].

Additionally, Vitest `5.0.0-rc.2` moves the default `json` sink off stdout to `.vitest/json/output.json` [SRC][DOC]. A raw-output contract would therefore also be version-fragile across the next major.

## 9. Why Jest stdout cannot be qualified

Jest routes far more carefully than Vitest. `--json` is silently aliased to `useStderr`, and `runCLI` selects `outputStream = argv.json || argv.useStderr ? process.stderr : process.stdout` with the upstream comment "If we output a JSON object, we can't write anything to stdout, since it'll break the JSON structure and it won't be valid" [SRC]. `BaseReporter.log` is hardcoded to stderr [SRC]. With `--json`, even test `console.log` is redirected to stderr [SRC].

That is a deliberate and effective design. It is still not sufficient, because it does not cover writes that bypass the console abstraction. Verified on `30.4.2` and `29.7.0` [RUN]:

```text
test calls process.stdout.write(...)
  → jest-runner pipes worker stdout to the parent's stdout
  → raw bytes precede the JSON; JSON.parse fails
  → --silent does NOT suppress it (it only affects console.*)
  → --runInBand does NOT avoid it

NODE_OPTIONS="--require ./preload.js" where preload writes to stdout
  → JSON corrupted
```

The corrupting writer is ordinary application code, not an adversary. Native addons, spawned child processes inheriting stdout, and transformer noise all reach the same stream.

Conversely, `--json --outputFile=<path>` wrote **0 bytes** to stdout in every verified run, and the JSON went to the file only, never to both [RUN][SRC].

## 10. Consequence: no new input kind and no capture change

Because both adapters bind an explicitly requested output file, they fit the existing artifact contract exactly:

```text
declared_path_token           ← the producer's --outputFile value
normalized_workspace_path     ← resolved against frozen ResolvedCWD, containment proven
expected_kind = regular_file
```

Therefore this design requires no `StructuredInputRef` amendment, no new baseline semantics, and no capture-layer change. It consumes pytest §9–§42 unchanged.

## 11. Adapter identity and naming

Adapter identities SHALL be:

```text
vitest-json@v1
jest-json@v1
```

They SHALL NOT be named `js-test-json`, `javascript-test-json`, `node-test-json`, or any other ecosystem-level label, per pytest §43.

They SHALL NOT be collapsed into a single `jest-compatible-json` adapter. Vitest documents its JSON reporter as "compatible with Jest's `--json` option" [DOC], and its own source says the types were "extracted from the Jest repository (and simplified)" [SRC]. That is a compatibility claim by one producer, not a ShellBeam-qualified equivalence, and Section 12 shows it is materially false at the field level.

## 12. Verified payload divergence between the two producers

Diffed against Jest's own `AssertionResult` / `AggregatedResult` types [SRC both sides]:

| Dimension | Vitest | Jest |
|---|---|---|
| retry attempt count | absent | `invocations` (integer) |
| expected-failure marker | absent | `failing` (boolean, 30.x) |
| retry error history | absent | `retryReasons` (opt-in) |
| per-assertion metadata | `meta`, `tags` (4.1+), `benchmarks` (5.x) | absent |
| file-level status union | `failed \| passed` | `failed \| passed \| skipped \| focused` |
| suite-level counters | run-level only | run-level only, different buckets |
| `numRuntimeErrorTestSuites` | absent | present |
| `wasInterrupted` | absent | present |
| assertion key count | version-dependent | 11 (29.x) / 13 (30.x) |

One adapter cannot own both without either discarding Jest-only mechanical dimensions or claiming Vitest dimensions that do not exist. Both outcomes violate pytest §75 (no invented mechanical authority) or pytest §65 (coverage must be honest). Two adapters with two independent `ProducerSemanticsCoverage` declarations is therefore the only correct structure.

## 13. Six independent qualification gates

Mechanical derivation for either adapter requires independent qualification of:

```text
1. producer
2. invocation
3. reporter/output binding
4. immutable artifact
5. structural profile
6. semantic shape
```

No gate is inferred from another.

This extends the pytest five-gate model (pytest §55) with an explicit **structural profile** gate. Pytest's xunit2 dialect was declarable in the invocation (`-o junit_family=xunit2`); neither Vitest nor Jest exposes any comparable in-invocation schema selector, and neither payload carries a schema version (§28). The profile gate is where that gap is handled mechanically instead of by assumption.

Qualification SHALL preserve per-axis state, using the closed axis-state vocabulary of pytest §56:

```text
qualified | qualified_complete | qualified_partial
unavailable | unsupported | contradictory | not_evaluated
```

## 14. Unified invocation binding per adapter

Each adapter SHALL have exactly one option-aware resolver producing one canonical binding. Independent parsers for selection, capture path, and reporter qualification are forbidden, for the reason given in pytest §46: they can disagree about the effective invocation.

Conceptually:

```text
VitestInvocationBindingV1
  schema_version
  producer_form
  run_mode_binding
  json_reporter_binding
  output_file_binding
  excluded_flag_state

JestInvocationBindingV1
  schema_version
  producer_form
  json_flag_binding
  output_file_binding
  excluded_flag_state
  jest_jasmine_environment_fact
```

Each binding's canonical digest SHALL be the `producer_binding_digest` stored by `ArtifactCaptureIntent`, exactly as `PytestInvocationBindingV1` is today. It SHALL commit every identity-bearing field above.

Crash recovery SHALL validate and reuse the persisted canonical binding. It MUST NOT re-observe the environment or filesystem to recreate qualification (pytest §46, §100).

## 15. Vitest producer qualification

V1 auto-qualifies exactly one producer form, determined from the frozen resolved execution contract:

```text
resolved argv[0] basename == "vitest"
```

This covers the ordinary local-install resolution `node_modules/.bin/vitest`.

Not auto-qualified:

```text
npm test / npm run test / yarn test / pnpm test / bun test
npx vitest / pnpm vitest / yarn vitest / bunx vitest
node <path>/vitest.mjs
vite test
turbo / nx / make / bash -c ...
```

A validated project command may still execute Vitest, but producer qualification examines its final resolved argv rather than trusting a `kind=test` label (pytest §45, §47).

`node`-based invocation forms are deliberately excluded. Pytest qualified `python -m pytest` because it is an exact documented producer form with a stable module contract; Vitest has no equivalent documented `node -m` form, and the concrete entry-script path is an implementation detail of the installed distribution.

## 16. Vitest watch mode is unqualified

Plain `vitest` defaults to watch mode outside CI. A watch-mode invocation has no terminal producer cut, so it cannot participate in Phase A ordering at all.

V1 SHALL require a mechanically proven non-watch invocation:

```text
first non-option argument == "run"
  OR --run present
```

Any of the following makes the invocation unqualified: `--watch`, `-w`, `--watch-all`, watch mode implied by the absence of both proofs above.

`run_mode_binding` SHALL record which proof was used.

## 17. Vitest reporter binding

The `json` reporter is built into Vitest core with no extra install, confirmed from the `ReportersMap` registry in `packages/vitest/src/node/reporters/index.ts` for `1.6.1`, `2.1.9`, `3.2.7`, `4.1.11`, and `5.0.0-rc.2` [SRC].

V1 SHALL require an explicit CLI `--reporter=json`.

This is a strong lever, because a CLI `--reporter` **replaces** config-file reporters rather than merging with them, verified identical across `1.6.1 / 2.1.9 / 3.2.7 / 4.x / 5.0.0-rc.2` [SRC], and confirmed by upstream documentation: "If you configure your own reporters, the configured list replaces the default list" [DOC].

Consequences ShellBeam relies on:

- an unrelated config reporter cannot silently add output;
- `GITHUB_ACTIONS=true` appends the `github-actions` reporter only when no reporters are configured, so an explicit `--reporter` neutralizes it [SRC].

ShellBeam SHALL NOT append `--reporter=json`. Its absence makes the invocation unqualified.

## 18. Vitest output-file binding

V1 SHALL require an explicit CLI output-file binding for the `json` reporter, in exactly one of two forms:

```text
--outputFile.json=<path>          always qualified
--outputFile=<path>               qualified ONLY when json is the sole reporter
```

The second form is restricted because `getOutputFile` returns the same string for every reporter when `outputFile` is a plain string [SRC]. With two file-writing reporters this was observed to produce a single clobbered file containing half JSON and half XML [RUN 4.1.11]. A capture that can be overwritten by a second reporter is not immutable producer authority.

`tap` and `tap-flat` ignore `outputFile` entirely [RUN 4.1.11]; they are irrelevant here but confirm that not every reporter honors the binding.

CLI `--outputFile` beats config `outputFile` by deep merge with CLI last [SRC], verified for both the string form (replaces the whole config mapping) and the dotted form (merges per key) [RUN 4.1.11].

## 19. Vitest config-mediated redirection, and why it fails closed

One redirection path is not mechanically detectable without config discovery, which is deferred (§27).

Reporter-level options beat the top-level mapping: `this.options.outputFile ?? getOutputFile(this.ctx.config, 'json')` [SRC, all majors]. From `4.0.5` onward, a CLI `--reporter=json` preserves the config file's options for reporters named on both sides via `configReportersMap` [SRC; absent in `4.0.0`, present in `4.0.5`; the exact introducing patch in `4.0.1`–`4.0.4` is [UNVERIFIED]]. A config entry `reporters: [['json', { outputFile: '<other path>' }]]` can therefore win over CLI `--outputFile.json=<declared path>`.

This is safe, and it is safe for a structural reason rather than by luck:

```text
config redirects the report away from the declared path
→ declared path was proven absent at the pre-spawn baseline
→ terminal Phase A finds no object at the pinned parent + final name
→ capture_result = unavailable
→ no ArtifactBlobRef, no derivation
```

The failure mode is a missing structured result, never a misattributed one. Redirection cannot cause ShellBeam to read another run's bytes, because the only path it ever opens is the one whose absence it proved before spawn, under the pinned descriptor authority (pytest §11, §16).

The residual risk that a *different* writer targets the declared path is already covered by baseline-absence plus managed same-path collision (pytest §12, §13), and is not specific to this adapter.

This design therefore accepts config-mediated redirection as a capture-availability limitation, and SHALL NOT add config discovery to close it.

## 20. Vitest environment authority

There is no `PYTEST_ADDOPTS` analogue in Vitest. Established three ways:

1. exhaustive enumeration of every `process.env` access in the published `dist/` of `4.1.11`; none injects CLI arguments or selects reporters [SRC];
2. `VITEST_ADDOPTS` / `VITEST_ARGS` / `VITEST_OPTIONS` had no effect [RUN 4.1.11];
3. `vitest run @args.txt` treated `@args.txt` as a plain test-file filter — there is no argument-file expansion [RUN 4.1.11].

Only three environment variables reach resolved config at all, and none is identity-bearing for this adapter: `GITHUB_ACTIONS` (neutralized by §17), `VITEST_MAX_WORKERS` (worker count), `VITEST_SKIP_INSTALL_CHECKS` (install prompt) [SRC].

Historical `VITEST_JUNIT_CLASSNAME` / `VITEST_JUNIT_SUITE_NAME` existed in `1.x` and were removed in `2.0`; neither ever affected the `json` reporter [SRC].

Therefore `VitestInvocationBindingV1` SHALL NOT carry an environment-presence fact. Unlike pytest, there is no argument-injection channel to prove absent.

## 21. Jest producer qualification

V1 auto-qualifies exactly one producer form:

```text
resolved argv[0] basename == "jest"
```

Not auto-qualified: the same wrapper set as §15, plus `node <path>/jest.js`.

## 22. Jest json and output-file binding

`--json` and `--outputFile=<path>` are built in and require no extra package [DOC][SRC]. The decisive producer code is `processResults` in `@jest/core`, which writes the file when `outputFile` is set and otherwise writes stdout [SRC].

V1 SHALL require both flags explicitly on the CLI:

```text
--json                 required
--outputFile=<path>    required   (--outputFile <path> also accepted; requiresArg)
```

Verified behavior that makes this binding safe [RUN 30.4.2 + 29.7.0]:

```text
--json --outputFile=F   → file only; stdout 0 bytes; never both
--outputFile=F alone    → file not created, flag silently ignored
```

Two config interactions were verified and both favor requiring the CLI form:

- config `json: true` has **no effect** — `case 'json'` exists only in `setFromArgv`, never in `normalize`; with no `--json` on the CLI, stdout was 0 bytes [SRC][RUN]. `--json` is effectively CLI-only.
- config `outputFile` works but emits `● Validation Warning: Unknown option "outputFile"` because `outputFile` is missing from `ValidConfig` in both `29.7.0` and `30.4.1` [SRC][RUN]. CLI wins regardless (`setFromArgv` places CLI flags last) [SRC].

A config `reporters` setting cannot suppress or alter `--json`: with `reporters: []` and `--json`, valid JSON was produced on stdout and stderr was 0 bytes [RUN 30.4.2]. `--json` is produced by `processResults`, entirely outside the reporter chain [SRC]. This is a stronger separation than Vitest's and is why Jest is sequenced first (§5).

## 23. Jest payload-shape-affecting flags SHALL be absent

The following were verified to change the payload shape, the payload schema, or execution completeness. Each SHALL make the invocation unqualified:

```text
--listTests        --json emits a JSON ARRAY OF PATH STRINGS, a different schema;
                   and --outputFile is honored WITHOUT --json on this path,
                   writing newline-joined absolute paths with no trailing newline
                   [RUN 30.4.2]

--collectTests     every assertion becomes "pending" [RUN 30.4.x]

--watch / --watchAll   no terminal producer cut

--bail / -b        run may terminate before all tests execute

--onlyFailures / -o    intentionally partial selection

--shard            [UNVERIFIED] interaction with --json

--testResultsProcessor   a processor runs BEFORE JSON serialization and can
                         rewrite the payload (§26)
```

`--randomize` / `--seed` affect only ordering, which this design already treats as non-authoritative (§39), and are therefore permitted; whether the seed is emitted into the JSON is [UNVERIFIED] and SHALL NOT be relied upon.

The `excluded_flag_state` field of the binding SHALL record that this closed exclusion set was evaluated and found absent, so the fact is committed into the digest rather than re-derived at recovery time.

## 24. Jest requires proven absence of JEST_JASMINE

`JEST_JASMINE=1` swaps the test framework to `jest-jasmine2` [SRC `jest-runner`]. That changes the observable status vocabulary — jasmine can produce `'disabled'`, and `location.column` is 0-indexed under jasmine versus 1-indexed under jest-circus [DOC]. It therefore changes the semantic contract this adapter qualifies.

`JestInvocationBindingV1` SHALL carry a `jest_jasmine_environment_fact` using the existing `EnvironmentPresenceFact` machinery built for `PYTEST_ADDOPTS`:

```text
name = JEST_JASMINE
present = false
authority_schema_version
authority_digest
```

Semantics are identical to pytest §46: the digest binds the mechanically observed absence at pre-spawn qualification time, is deterministic, and is replayable from durable capture authority without re-observing the current process environment. No environment value is stored.

`present = true` makes the invocation unqualified. jest-jasmine2 behavior itself is [UNVERIFIED] and out of scope.

## 25. Jest environment facts that are deliberately NOT identity-bearing

Two environment influences were verified to affect only stderr, never the `--json` payload. Neither SHALL be added to the binding, and neither SHALL make an invocation unqualified.

**Agent-reporter switching.** From Jest `30.3.0`, `TestScheduler._setupReporters` selects `AgentReporter` instead of `DefaultReporter` when any of `AI_AGENT`, `AUGMENT_AGENT`, `CLAUDE_CODE`, `CLAUDECODE`, `CODEX_SANDBOX`, `CODEX_THREAD_ID`, `CURSOR_AGENT`, `GEMINI_CLI`, `GOOSE_PROVIDER`, `OPENCODE`, `REPL_ID` is present and non-empty [SRC; bisected absent in `30.2.0`, present in `30.3.0`]. `--json` stdout content was byte-identical with and without those variables [RUN 30.4.2], as expected since `--json` bypasses the reporter chain.

This matters for ShellBeam specifically: ShellBeam is normally driven by an agent, so these variables will frequently be set in real runs. Recording this as a non-fact prevents a future change from treating an ordinary agent-driven run as unqualified.

**Color forcing.** `FORCE_COLOR` / `NO_COLOR` inject ANSI escapes into `message` and `failureMessages` string values [RUN 30.4.2]. The document remains valid JSON. This adapter never reads those fields (§33), so the effect is out of contract.

`NODE_OPTIONS` is read by Node, never by Jest [SRC: zero `process.env` references in the `jest-cli` and `jest-config` bundles]. It can corrupt stdout via preload writes (§9), which is one more reason the artifact path is the qualified one, and it can shift timings. It is not identity-bearing for artifact capture.

## 26. Producer mutation limits

Two mutation channels are outside the V1 producer contract, exactly as pytest §52 treats plugin mutation of `junit_family`:

- **Jest `testResultsProcessor`** runs before JSON serialization and can rewrite the payload wholesale [SRC]. The CLI form is excluded (§23); the config form is not detectable without config discovery.
- **Vitest custom reporters and config reporter options** can redirect or replace output (§19).

Mechanical parsing is not cryptographic attestation against a deliberately mutating local configuration. Both adapters assume the standard qualified built-in producers.

Both channels are safe in the provenance sense for the same structural reason as §19: they can only cause the declared path to be absent or to hold non-conforming bytes, both of which fail closed at Phase A or at the structural-profile gate. Neither can cause bytes from a different run to be attributed to this operation.

## 27. Config discovery is deferred

V1 SHALL NOT implement effective config discovery for either producer.

Vitest config sources are `vitest.config.{ts,mts,cts,js,mjs,cjs}` then `vite.config.*`, with `vitest.config` overriding `vite.config` entirely, plus `--config` [SRC][DOC]. Jest config sources are `jest.config.{js,ts,mjs,mts,cjs,cts,json}` in that exact extension order, plus the `package.json` `"jest"` key, plus `--config` which also accepts an inline JSON string; multiple candidates in one directory raise `Multiple configurations found`; discovery walks up to filesystem root [SRC].

Implementing this would require a discovery subsystem with per-version precedence rules, for the same reasons pytest §51 rejected it. The strict V1 mechanism is instead: require explicit CLI bindings (§17, §18, §22), and rely on the structural fail-closed argument (§19, §26).

Note that `test.reporters` and `test.outputFile` are root-only in Vitest — project-level values were silently ignored with no warning or error [SRC][RUN 4.1.11]. This is one more reason multi-project invocations are unqualified (§34).

## 28. No payload schema version exists

Neither producer emits a schema version.

Verified against the actual emitted top-level key sets: no `version`, `schemaVersion`, `$schema`, `vitestVersion`, or `jestVersion` key in Vitest `1.6.1 / 2.1.9 / 3.2.7 / 4.1.11 / 5.0.0-rc.2` or in Jest `29.7.0 / 30.4.2` [SRC + RUN]. Jest's `@jest/schemas` covers config shapes only, not test results [SRC].

Neither producer documents a stability guarantee for this output. A search of Vitest's reporter documentation for `stable|stability|semver|guarantee|breaking|schema version` returned zero hits [DOC]. Jest's only shape documentation is the `testResultsProcessor` blob, which describes the **raw** `AggregatedResult` a processor receives, not the formatted `--json` payload, and is wrong in at least three ways: it omits `"todo"` from the status list, shows `coverage: {}` unconditionally when the key is often absent, and documents `perfStats` / `testFilePath` fields that `--json` does not emit [DOC vs SRC vs RUN].

Consequently V1 SHALL NOT attest a producer version, consistent with pytest §53, and SHALL NOT read the declared TypeScript types as the contract. Two verified type/runtime mismatches make the declared Jest types unusable as a specification: `formatTestResults` spreads the full `AggregatedResult`, so fields absent from `FormattedTestResults` (`numTodoTests`, `openHandles`, `runExecError`) are nonetheless emitted; and `formatTestResult` assigns `assertionResults: testResult.testResults` unmodified, so the emitted objects are full `AssertionResult`s rather than the narrower declared `Pick<>` [SRC].

The parser SHALL be specified against observed emitted shape, not against declared types.

## 29. Structural profile is the mechanical discriminator

Because no version field exists, the **observed key set** SHALL be the mechanical discriminator, and the strict-decode boundary SHALL be the enforcement mechanism.

The repository already provides exactly the required primitive: `internal/core/jsonstrict.Decode` rejects unknown members, duplicate names, invalid UTF-8, and case-mismatched names.

The design consequence is direct:

```text
one closed decode struct per qualified structural profile
+ RejectUnknownMembers
→ an unknown or future key set fails closed as unsupported profile
→ never as best-effort partial interpretation
```

Duplicate-name rejection matters independently: a JSON document may repeat an object member, and permissive decoders silently take the last occurrence. At a provenance boundary that is a semantic ambiguity, and it SHALL be rejected.

`ObservedStructuralProfile` is not a producer version. It is a mechanically observed key-set identity. Two different distributions may share a profile, and V1 claims nothing about which distribution produced the bytes.

## 30. Qualified structural profiles per adapter release

Each adapter release SHALL declare a closed set of qualified profiles. Any document outside the set is `unsupported` at gate 5.

Verified profile-bearing key deltas:

**Jest** [SRC + RUN]

| Profile | Assertion keys | Discriminator |
|---|---|---|
| `v29` | 11 | no `failing`, no `startAt` |
| `v30` | 13 | adds `failing`, `startAt`, both always present in 30.x output |

Top-level key set, per-file key set, file-level status union, `Status` union, counters, and retry representation were **identical** between `29.7.0` and `30.4.2` [RUN]. The 29→30 change was purely additive at the assertion level. Jest `28.x` is [UNVERIFIED] and SHALL NOT be qualified.

**Vitest** [SRC]

| Key | 1.6.1 | 2.1.9 | 3.2.7 | 4.0.x | 4.1.11 | 5.0.0-rc.2 |
|---|---|---|---|---|---|---|
| `snapshot` (top) | — | ✓ | ✓ | ✓ | ✓ | ✓ |
| `coverageMap` (top) | — | — | ✓ | ✓ | ✓ | ✓ |
| `meta` (assertion) | — | ✓ | ✓ | ✓ | ✓ | ✓ |
| `tags` (assertion) | — | — | — | — | ✓ | ✓ |
| `benchmarks` (assertion) | — | — | — | — | — | ✓ |

V1 SHALL qualify only the profiles corresponding to the executed versions: Jest `v29` and `v30`, Vitest `v3` and `v4`. Vitest `1.x` and `2.x` were established from types and source only, and `5.0.0-rc.2` was not executed at all; all three are [UNVERIFIED] at runtime and SHALL be unqualified until a release-qualification run covers them.

Optional-key handling within a qualified profile SHALL be explicit, because these keys are conditionally absent rather than null [RUN 4.1.11]:

```text
coverageMap   absent unless coverage enabled
location      absent unless includeTaskLocation
duration      absent for skipped/todo entries
coverage      (Jest, per-file) absent for ordinary files
```

## 31. Release qualification matrix

Adapter releases SHALL be qualified against a bounded producer matrix, per pytest §54, and fixtures SHALL be producer-realistic, per pytest §90.

Minimum matrix:

```text
jest-json@v1     jest 29.7.0, jest 30.4.2
vitest-json@v1   vitest 3.2.7, vitest 4.1.11
```

For each producer line the matrix SHALL cover, at minimum: ordinary pass; ordinary failure; `skip`; `todo`; a skipped `describe` block; a `beforeAll` failure; a `beforeEach` failure; an `afterAll` failure; a module-level/collection throw; a retry sequence that ends failed; a retry sequence that ends passed; and for Jest additionally an `it.failing` that fails and an `it.failing` that passes.

Fixtures SHALL be frozen with recorded producer version, exact generating invocation, and SHA-256, and committed tests SHALL consume frozen bytes with no network and no installed runner, exactly as the pytest fixture manifest does.

The end-to-end release script MAY create throwaway installs to exercise the real daemon path. That is deliberate qualification cost, not ordinary spawn tax.

## 32. Parser bounds

Both parsers SHALL be bounded and SHALL fail closed.

```text
input bytes            inherited from the artifact resolver
                       (DefaultMaxArtifactBlobBytes = 16 MiB)
persisted records      1024 max
per string field       64 KiB max
decode                 jsonstrict strict decode into a closed per-profile struct
parse duration         bounded by existing structured worker limits
```

Strict whole-document decode within the existing blob ceiling is permitted. The parser SHALL bound peak allocation and SHALL reject a document whose entry counts exceed the record cap before normalizing any record, rather than normalizing until the cap trips.

A document that decodes successfully but exceeds structural caps is `budget_exceeded`, not `malformed`. These have distinct provenance and distinct diagnostics (pytest §99 reasoning).

Jest ≤ `30.4.2` aborts serialization entirely if any `BigInt` reaches the results, producing no output and exit 1 [RUN 30.4.2; fixed only on `main`]. Jest `globalSetup` / `globalTeardown` throws also produce no output at all [RUN 30.4.2]. Both cases surface as an absent artifact and fail closed at Phase A; no special handling is required or permitted.

## 33. No prose extraction, and failure text is not persisted

Neither parser SHALL read `message`, `failureMessages`, `retryReasons`, `retryMessages`, or `failureDetails` for any semantic purpose.

V1 SHALL additionally **not persist** those fields at all. Three reasons:

- they carry no mechanical authority (pytest §75);
- they can contain ANSI escape sequences under color forcing (§25);
- Jest `message` embeds rendered failure blocks including stack frames, and Vitest `failureMessages` entries are `e.stack || e.message` with absolute paths — persisting them would move host filesystem paths into structured records for no mechanical gain.

`failureDetails` is separately unusable: for a thrown `Error` it serializes to `{}` because `Error` own-properties are non-enumerable [SRC + RUN].

Nothing derived from these fields SHALL be persisted either. In particular §37 forbids the one tempting derivation.

## 34. Multi-project and workspace invocations are unqualified

Vitest `test.projects` and Jest `projects` are both unqualified in V1.

For Vitest, project-level `reporters` and `outputFile` are silently ignored [SRC][RUN 4.1.11], so a project-scoped configuration cannot be bound at all. For Jest, `displayName` is present on the internal `TestResult` and dropped by `formatTestResult`, so per-project attribution is probably absent from `--json` — but this is [UNVERIFIED].

Without provable per-project attribution, records from different projects would be indistinguishable in one flat document. V1 declines rather than guesses.

## 35. Vitest status mapping

The emitted union is `passed | failed | skipped | pending | todo | disabled`, identical from `1.6.1` through `5.0.0-rc.2` [SRC]. The producer maps internal state through `StatusMap[t.result?.state || t.mode] || 'skipped'` [SRC], and only a subset is reachable.

Mechanical mapping:

```text
passed   → pass
failed   → fail
skipped  → skip  + vitest:skipped
todo     → skip  + vitest:todo
pending  → contradictory; record fails closed
disabled → unsupported; document fails closed
```

`pending` at report time means a test was still in `run`/`queued` state, and the producer itself emits the warning "Some tests are still running when generating the JSON report. This is likely an internal bug in Vitest" [SRC]. ShellBeam SHALL NOT normalize a self-declared producer bug into a mechanical record.

`disabled` is dead code: no `TaskState` maps to it and it can never be emitted [SRC]. If it is ever observed, the profile assumption is void and the document SHALL be rejected rather than mapped.

The closed V1 disposition vocabulary for this adapter is:

```text
vitest:skipped
vitest:todo
```

## 36. Vitest focus state is unavailable

`it.only` runs the focused test normally, and emits its non-focused siblings as `skipped` — mechanically indistinguishable from an explicit `.skip` [RUN 4.1.11 + 3.2.7].

Therefore `vitest:focus_state` SHALL be declared unavailable, and a `skip` record SHALL NOT be interpreted as evidence that the suite was fully selected.

## 37. Vitest retry and flake state are unavailable

Retries collapse to a single entry with the final status. There is no retry-count field and no per-attempt entries [SRC + RUN].

The only surviving signal is that `failureMessages` accumulates every attempt's error, so a flaky test appears as `status: "passed"` with a non-empty `failureMessages` [RUN 4.1.11 + 3.2.7: two failures then a pass produced `passed` with `failureMessages.length == 2`].

That is an inference from an implementation detail, not a producer contract. V1 SHALL NOT use it. `vitest:retry_attempts` and `vitest:flake_state` SHALL be declared unavailable, and §33 already forbids persisting the count that would invite the inference.

The underlying data exists internally — `TestCase.diagnostic()` exposes `retryCount`, `repeatCount`, and `flaky` — but the built-in `json` reporter never calls it in any version from `1.x` to `5.x` [SRC]. Recovering this dimension would require a custom reporter, which is out of scope because ShellBeam does not install reporters (§6).

`duration` reflects the final attempt only, not cumulative time [SRC].

## 38. Vitest can report success on a failed run

Verified on `4.1.11` and `3.2.7` [RUN]: a test that schedules `setTimeout(() => { throw ... }, 1)` and then passes yields process exit code 1, while the JSON reports `success: true` and `numFailedTests: 0`, and the unhandled error text appears nowhere in the document. `JsonReporter.onTestRunEnd` ignores its `unhandledErrors` argument entirely [SRC]. With a json-only reporter, stderr was 0 bytes — the error is lost from the machine output completely.

This is direct evidence for the contract pytest §73 already fixed. Both adapters SHALL therefore:

- treat the durable terminal receipt as the only child-outcome authority;
- **never** read the producer's `success` field for any purpose;
- never treat a document full of `pass` records as contradicting a failing receipt.

Jest exhibits a milder form of the same hazard: `success: true` when `--testNamePattern` filters out every test [RUN 30.4.2].

`vitest:unhandled_error_visibility` SHALL be declared unavailable, so a policy that needs "no unhandled errors occurred" cannot be satisfied from this provider.

## 39. Jest status mapping

The assertion-level union is `passed | failed | skipped | pending | todo | disabled | focused`, byte-identical in `@jest/types@29.6.3`, `@jest/types@30.4.1`, and `main` [SRC]. The producer is `parseSingleTestResult` in `jest-circus`, and only four values are reachable from it [SRC]:

```text
internal 'skip' → 'pending'
internal 'todo' → 'todo'
errors present  → 'failed'
otherwise       → 'passed'
```

Mechanical mapping:

```text
passed  → pass   (see §40 for the failing-flag refinement)
failed  → fail
pending → skip + jest:pending
todo    → skip + jest:todo
skipped | disabled | focused → unsupported; document fails closed
```

Observed statuses across the full fixture suite on both `29.7.0` and `30.4.2` were exactly `passed`, `failed`, `pending`, `todo` [RUN]. `disabled` appears in zero files of `jest-circus`, `@jest/reporters`, `@jest/core`, and `jest-util`; it is jest-jasmine2 legacy, and jasmine is excluded by §24 [SRC].

Verified source mapping:

| Source construct | assertion `status` | counter |
|---|---|---|
| `test.skip` / `it.skip` | `pending` | `numPendingTests` |
| `test.todo` / `it.todo` | `todo` | `numTodoTests` |
| `describe.skip` inner tests | `pending`, one entry each, skipped title retained in `ancestorTitles` | `numPendingTests` |
| `--testNamePattern` non-match | `pending` | `numPendingTests` |

The closed V1 disposition vocabulary for this adapter is:

```text
jest:pending
jest:todo
jest:failing_expected      (v30 profile only, §40)
jest:failing_unexpected    (v30 profile only, §40)
```

## 40. Jest exposes an expected-failure dimension that pytest JUnit could not

`it.failing` inverts the assertion outcome, and Jest emits a mechanical boolean `failing` alongside the resulting status [SRC; always present in 30.x output, absent in 29.x].

Verified: an `it.failing` that does fail is emitted as `status: "passed"` with `failing: true` and counted in `numPassedTests` [RUN 30.4.2].

This is the exact dimension pytest V1 had to declare unavailable. Pytest's non-strict XPASS collapses to an ordinary pass and strict XPASS collapses to a failure with no typed marker, so pytest §62 forbade recovering it. Jest instead publishes a typed boolean field, so it is mechanically observable — from a real field, not from prose.

For the `v30` profile only:

```text
status = passed  AND failing = true  → pass + jest:failing_expected
status = failed  AND failing = true  → fail + jest:failing_unexpected
failing = false                      → no disposition
```

`jest:failing_expected` SHALL NOT imply anything about which assertion failed, and `jest:failing_unexpected` SHALL NOT be described as a pytest-equivalent XPASS. It is a Jest-specific producer disposition in the Jest namespace, per pytest §60.

For the `v29` profile the `failing` key does not exist, so both codes SHALL be declared unavailable in that profile's coverage. This is a per-profile coverage difference within one adapter version, and the coverage declaration SHALL reflect the observed profile rather than the adapter version.

## 41. Jest retry attempt count is mechanically observable

`AssertionResult.invocations` is an integer count of total attempts, incremented in `jest-circus` [SRC]. Retries collapse to a single entry with the final status; there are no duplicate entries.

Verified with `jest.retryTimes(3)` on both `29.7.0` and `30.4.2` [RUN]:

| Behavior | `status` | `invocations` |
|---|---|---|
| fails twice, then passes | `passed` | 3 |
| always fails | `failed` | 4 |

Therefore `jest:invocations` SHALL be mechanically observable, from the integer field only:

```text
attempts = invocations
retries  = invocations - 1
```

This is a genuine mechanical fact, unlike the Vitest inference forbidden in §37. The distinction is exactly the one pytest §75 draws: an integer field published by the producer is authority; a message-array length is not.

A record with `status = pass` and `invocations > 1` SHALL NOT be relabeled `fail`, and SHALL NOT be described as "flaky" by the adapter. It records attempts; a flake policy is a P1 concern with its own contract (pytest §66).

`retryReasons` is populated only when `jest.retryTimes(n, { logErrorsBeforeRetry: true })` is set, and holds formatted error strings [SRC + RUN: 0 without the option, 2 with it]. It is config-mediated and prose, so `jest:retry_reasons` SHALL be declared unavailable and §33 forbids persisting it. `retryMessages` exists only on `main` and is unqualified.

## 42. Hook phase and suite execution error are unavailable in both adapters

Neither producer publishes a typed hook-phase marker. Verified representations:

**Vitest** [RUN 4.1.11 + 3.2.7]: a throwing `beforeEach` produces an ordinary test entry with `status: "failed"` and the hook error in `failureMessages`. Nothing in the document identifies it as a hook — recovering that would require pattern-matching a stack frame. There is no `hookErrors`, no `failureDetails`, no error `type`.

**Jest** [RUN 30.4.2 + 29.7.0]: three different representations depending on the hook.

```text
beforeAll throws   → attributed to individual assertions; each test in the
                     block gets status "failed" carrying the hook error
beforeEach throws  → same per-assertion attribution
afterAll throws    → file-level exec error; tests keep their real statuses,
                     the file gets status "failed" and message = the error
module-level throw → file-level exec error; assertionResults is empty
```

The producer's internal `testExecError` field is **not present** in `--json`; `formatTestResult` collapses it into `status: "failed"` plus `message` [SRC].

A detection heuristic exists — a file has a suite-level error iff `status == "failed"` and no assertion entry is `failed` — and V1 SHALL NOT use it. It is inference over record shape, which pytest §75 excludes from mechanical authority just as firmly as prose parsing.

Consequently both adapters SHALL declare unavailable:

```text
<ns>:hook_phase
<ns>:suite_execution_error_distinction
```

## 43. Core error status is never produced in V1

Because §42 removes the only candidate sources, neither adapter SHALL ever emit a record with `TestStatus = error`.

This is a deliberate, declared gap, not an oversight, and it SHALL appear in both coverage declarations as `<ns>:error_status` unavailable.

Core `TestStatus` SHALL NOT be narrowed, and `error` SHALL NOT be repurposed. Any P1 obligation requiring an error-versus-failure distinction is unsatisfiable from these providers, which is precisely the outcome pytest §66 specifies for insufficient semantic coverage — and is not negative evidence about the code under test.

## 44. Suite and file record mapping

Each qualified document normalizes to:

```text
one test_case record per assertion entry
one test_suite record per file entry
```

File-level status mapping is from the producer's own status field only:

**Vitest** — union is `failed | passed` [SRC]:

```text
failed → fail
passed → pass
```

Note that a file containing only skipped tests reports `passed` [RUN], so a `pass` suite record SHALL NOT be read as "tests executed".

**Jest** — union is `failed | passed | skipped | focused` [SRC]:

```text
failed  → fail
passed  → pass
skipped → skip
focused → pass + jest:suite_focused
```

`focused` means no failures **and** at least one pending test. It is assigned as `allTestsPassed ? (allTestsExecuted ? 'passed' : 'focused') : 'failed'` where `allTestsExecuted` is `numPendingTests == 0` [SRC]. A file with one passing test plus one `it.skip` therefore reports `focused`, not `passed` — reproduced on both `29.7.0` and `30.4.2` [RUN]. `skipped` occurs when `--testNamePattern` filters out every test in the file [RUN].

Mapping `focused` to `pass` is the honest coarse reduction, because no failure occurred; the disposition preserves the fact that execution was incomplete. A `focused` suite record SHALL NOT be read as complete suite execution.

## 45. Suite aggregate counters are unavailable in V1

Neither adapter SHALL emit `TestSuiteAggregate`.

Per-file counters do not survive into either payload: Jest's `formatTestResult` drops `numFailingTests`, `numPassingTests`, `numTodoTests`, and `perfStats` [SRC], and Vitest emits no per-file counters at all [SRC].

Run-level counters do exist, but their buckets do not map onto the JUnit-shaped `TestSuiteAggregate{Tests, Failures, Errors, Skipped}` without synthesis:

- both producers separate `pending` from `todo`, and collapsing them into one `Skipped` value is arithmetic ShellBeam would be inventing;
- Jest's `numRuntimeErrorTestSuites` counts *suites*, not tests, so placing it in a test-level `Errors` field is a category error;
- Vitest's `numTotalTestSuites` counts the file itself as a suite, so it is not comparable to a per-file aggregate [RUN 4.1.11: three `describe` blocks across two files yielded 6, not 4];
- Vitest's `numPendingTests` counts `skip` but not `todo`, and `numPassedTestSuites` is computed by subtraction, so the buckets are not a guaranteed partition [SRC].

Recomputing aggregates from normalized records is forbidden by pytest §72, and synthesizing them from mismatched producer buckets is forbidden by pytest §75. V1 therefore declares `<ns>:suite_aggregate_counters` unavailable.

A future amendment MAY add a producer-aggregate envelope in the producer namespace, versioned like `ProducerTestDisposition`. It SHALL NOT reuse the JUnit-shaped aggregate.

## 46. Producer address and path handling

Presentation and correlation fields SHALL be carried in `ProducerTestAddress`, per pytest §69:

```text
ProducerTestAddress
  namespace          = vitest | jest
  vocabulary_version = 1
  suite_name         = normalized workspace-relative test file path
  classname          = joined ancestor titles
  name               = title
```

Both producers emit the test file path as an **absolute** path (`testResults[].name`) [SRC + RUN both]. V1 SHALL normalize it to a workspace-relative path when containment is provable under the same frozen workspace authority used by capture. When containment is not provable, the record SHALL be partial with the suite address unavailable; the absolute host path SHALL NOT be persisted.

`ancestorTitles` SHALL be stored as a bounded array or a bounded joined form. The producer's own `fullName` SHALL NOT be treated as an identity: it is built by joining ancestor titles and title with a single space, which is ambiguous whenever a title contains a space [SRC]. Vitest v5 additionally changes `-t` matching to a `' > '`-joined form while `fullName` stays space-joined [DOC], so the two strings are not interchangeable even within one producer.

Per pytest §69, this address is not a globally unique identity, not a dedup key, not P1 evidence identity by itself, and not a cross-run stability key.

## 47. Entry identity and ordinals

Artifact entry identity SHALL reuse the existing structural scheme (pytest §67):

```text
ArtifactTestEntryRef
  artifact_blob_id
  suite_ordinal      = index of the file entry
  testcase_ordinal   = index of the assertion entry within that file

RecordID = H(derivation_key + "testcase" + suite_ordinal + testcase_ordinal)
```

This guarantees that the same immutable bytes in the same derivation yield the same record identity, and nothing more.

## 48. File order is not deterministic, in either producer

This warrants explicit statement because it is stronger than the pytest case.

**Jest**: `testResults[]` lands in worker-completion order. Five identical runs over the same eight files produced three distinct orders [RUN 30.4.2].

**Vitest**: `testResults[]` follows the sequencer, whose `BaseSequencer.sort()` consults the on-disk run cache to run previously failed files first and previously longer files first, falling back to larger files first for uncached files [SRC 4.1.11]. Order is therefore **history-dependent**: the same command on the same tree yields different orders depending on the previous run's outcome. Confirmed unstable across consecutive runs [RUN 4.1.11].

Within a file, assertion order was stable declaration order in both producers [RUN], unless randomization is enabled.

Consequences:

- ordinals from §47 are valid within one derivation over one immutable document, and SHALL NOT be used as cross-run identity — pytest §68 already forbids this, and here even a rerun of an unchanged tree will reorder;
- record ordering SHALL be preserved as emitted, and SHALL NOT be re-sorted, because re-sorting would change the ordinal identity of already-persisted records;
- no policy may depend on file order.

## 49. Duration normalization

Duration handling SHALL follow pytest §74: a qualified non-negative finite value normalizes deterministically to whole milliseconds, truncated toward zero, without locale-dependent parsing; invalid, negative, non-finite, or overflow values make that duration unavailable or the record partial, and are never guessed.

Producer-specific facts requiring care:

- Vitest `duration` is a full-precision float such as `0.9421249999999901` and is **absent** for skipped and todo entries [RUN 4.1.11];
- Vitest `testResults[].endTime` is a non-integer float, because an integer epoch millisecond is added to a high-resolution float duration [RUN 4.1.11];
- Jest `startTime` / `endTime` on the exec-error and skipped branches are both `Date.now()` captured at *formatting* time and are equal to each other, bearing no relation to execution timing [SRC + RUN: `startTime == endTime`];
- Jest `perfStats` is not present in `--json` [SRC].

Wall-clock timestamps SHALL NOT be persisted as mechanical facts, and SHALL NOT enter derivation identity. Only normalized durations may be persisted.

## 50. Duplicate entries and record count

Two assertion entries with identical ancestor titles and title SHALL remain two distinct records, per pytest §70. ShellBeam SHALL NOT deduplicate, merge statuses, select the worst status, or sum durations.

Record count semantics follow pytest §71:

```text
E22 test_case record count = normalized assertion entry count
```

It SHALL NOT be described as a logical test count. For Jest specifically, a `describe.skip` emits one `pending` entry per inner test [RUN], and retried tests collapse to one entry regardless of attempt count (§41).

## 51. Semantics coverage declarations

Coverage SHALL be declared explicitly per adapter and per observed profile, using the existing `ProducerSemanticsCoverage` envelope.

**`vitest-json@v1`**

```text
namespace = vitest
vocabulary_version = 1
format = vitest_json
family = <observed profile: v3 | v4>

mechanically_observable:
  core:test_status_pass
  core:test_status_fail
  core:test_status_skip
  vitest:skipped
  vitest:todo

unavailable:
  vitest:error_status
  vitest:hook_phase
  vitest:suite_execution_error_distinction
  vitest:focus_state
  vitest:retry_attempts
  vitest:flake_state
  vitest:suite_aggregate_counters
  vitest:unhandled_error_visibility
```

**`jest-json@v1`**

```text
namespace = jest
vocabulary_version = 1
format = jest_json
family = <observed profile: v29 | v30>

mechanically_observable:
  core:test_status_pass
  core:test_status_fail
  core:test_status_skip
  jest:pending
  jest:todo
  jest:invocations
  jest:suite_focused
  jest:failing_expected      (v30 profile only)
  jest:failing_unexpected    (v30 profile only)

unavailable:
  jest:error_status
  jest:hook_phase
  jest:suite_execution_error_distinction
  jest:suite_aggregate_counters
  jest:retry_reasons
  jest:failing_expected      (v29 profile only)
  jest:failing_unexpected    (v29 profile only)
```

Note that `core:test_status_error` appears in neither list as observable, per §43.

Coverage is not an authority downgrade (pytest §65). It is also not a scalar confidence score (pytest §92): `jest-json@v1` observing `jest:invocations` while `vitest-json@v1` cannot does not make Jest evidence "stronger" in general.

## 52. Coverage and P1 sufficiency

P1 integration SHALL reuse the existing read-time bridge introduced with pytest V1, without modification:

```text
StructuredEvidenceDetail
  DerivationKey
  Completeness
  MechanicalTestStatuses
  SemanticsCoverage
```

The join SHALL NOT mutate the durable Evidence Record. `EvidenceCandidate.Result`, authority, freshness, source/environment/command compatibility, and stability continue to derive exactly as before. Pending, compacted, or unavailable structured detail yields absent or explicitly incomplete enrichment; absence is never negative evidence.

Worked consequences of §43 and §45:

```text
obligation requires error-vs-failure distinction
→ neither adapter observes core:test_status_error
→ provider semantic coverage insufficient
→ obligation not satisfied, and NOT reported as failure

obligation requires "no test was retried"
→ jest-json@v1 observes jest:invocations  → satisfiable
→ vitest-json@v1 does not                 → insufficient coverage
```

Any policy consuming `jest:invocations`, `jest:failing_expected`, or `jest:suite_focused` SHALL be its own explicit bounded and versioned P1 requirement contract. Free-form coverage strings do not automatically gain policy semantics (pytest §66). This document adds no policy rule.

## 53. Selection precedence and auto-selection

Precedence remains as pytest §45:

```text
validated project command > explicit caller adapter > exact direct-argv safe rule
```

Auto-selection from direct argv SHALL occur only when the full binding for that adapter is qualified — producer form, run-mode proof for Vitest, JSON/reporter binding, output-file binding, excluded-flag absence, and for Jest the `JEST_JASMINE` absence fact. Partial evidence SHALL NOT auto-select.

Because Vitest and Jest have disjoint producer basenames and disjoint required flags, no argv can qualify for both adapters. An argv that somehow satisfies both SHALL be treated as `contradictory` rather than resolved by precedence.

## 54. Explicit adapter mismatch

If a caller explicitly requests `structured_adapter = vitest-json` or `jest-json` and the execution contract does not prove the corresponding qualified shape, ShellBeam SHALL return a typed structured-adapter contract error **before spawn**, per pytest §49.

It SHALL NOT append the missing flags, and SHALL NOT fall back to console parsing.

Capture failures discovered later from filesystem state — pre-existing artifact, managed path collision, absent output, materialization failure — SHALL NOT rewrite child execution semantics.

## 55. Security boundaries

Unchanged from pytest §86, and restated because the producer set is new:

- the parser reads only retained private blob bytes, never a workspace pathname;
- absolute host paths from producer output are not persisted (§46);
- failure text, stacks, and ANSI sequences are not persisted (§33);
- no producer flag, config file, or plugin is written or modified;
- no network access and no package installation occurs on the execution path;
- V1 does not attest against a hostile same-user writer on an ordinary workspace path (pytest §14).

Note one adapter-specific hazard already handled structurally: Vitest's `html` reporter triggers an automatic install of `@vitest/ui` [SRC]. V1 requires the reporter set to be an explicit CLI `--reporter=json` (§17), so ShellBeam never selects a reporter that installs anything. A caller-supplied `--reporter=html` alongside json is an unqualified multi-reporter form under §18 unless the dotted output binding is used, and ShellBeam neither adds nor removes it.

## 56. Resource limits

V1 reuses the shipped artifact limits unchanged. Names below are the constants as implemented:

```text
DefaultMaxArtifactBlobBytes             16 MiB
MaxActiveArtifactPathAuthoritiesGlobal  4
DefaultPinnedArtifactHandlesGlobal      4
DefaultArtifactAcquisitionConcurrency   2
DefaultMaterializationQueueDepth        4
MaxTerminalAcquireDuration              250 ms
```

Blob byte charging remains delegated to store state authority through `ReserveBlobBytes`, protecting `ControlReserve`; there is no separate adapter-level store ceiling.

Each adapter SHALL define its own record cap analogous to `maxPytestJUnitRecords`, with the same value:

```text
maxVitestJSONRecords = 1024
maxJestJSONRecords   = 1024
```

Each adapter has exactly one artifact capture intent. The generic capture-intent protocol ceiling remains 8.

State exhaustion SHALL NOT serve as flow control, and live referenced blob bytes SHALL NOT be implicitly evicted.

## 57. Capability advertisement and inspection

Capability advertisement SHALL be additive:

```text
structured_adapter_ids = [
  go-test-json, go-vet-json, pytest-junit-xml, vitest-json, jest-json
]
structured_schema_versions = [1, 2]
```

No second MCP tool is added. Artifact input kind and limits are already advertised.

`inspect.structured` remains the same action and SHALL expose, for these adapters, the same bounded facts it exposes for pytest: derivation semantics coverage, retained/compacted/unavailable source state, and bounded records. Blob bytes and workspace paths are not exposed beyond bounded logical provenance.

Forbidden completion claims — `task_complete`, `work_complete`, `safe_to_finish` — SHALL remain absent from structured transport.

## 58. Observability and audit

ShellBeam SHALL retain enough bounded machine-readable metadata to explain:

- why the adapter was selected, and from which precedence source;
- which producer form was qualified;
- the run-mode proof (Vitest) or the `--json` binding (Jest);
- the effective output path bound, and in which of the two forms (§18, §22);
- the excluded-flag evaluation result;
- the `JEST_JASMINE` absence authority (Jest);
- pre-spawn baseline result and capture result/diagnostic;
- blob identity, content digest, size;
- the observed structural profile and how it was matched;
- semantic-shape completeness and producer semantics coverage;
- compaction/retention state.

Typed diagnostics SHALL identify which of the six gates failed (§13).

## 59. No hidden auto-repair

ShellBeam SHALL NOT silently make an unqualified invocation qualified by:

- adding `--json`, `--reporter=json`, `--outputFile`, `run`, or `--run`;
- removing `--watch`, `--bail`, `--listTests`, or any other excluded flag;
- deleting a stale output file;
- changing CWD;
- modifying a Vitest or Jest config file;
- installing a reporter or a runner;
- redirecting output to a ShellBeam-private path;
- unsetting `JEST_JASMINE` or any other environment variable.

A future opt-in execution-transform feature would require its own mutation/authority design.

## 60. Design invariants

Frozen V1 invariants for this document, in addition to every pytest V1 invariant:

1. Both adapters use `artifact_blob`; neither uses `raw_output`.
2. No new `StructuredInputRef` kind, and no capture-layer change.
3. Vitest and Jest are separate adapters with separate coverage; the payloads are not interchangeable.
4. Explicit CLI bindings are mandatory; ShellBeam never appends a producer flag.
5. Watch-mode invocations are unqualified.
6. Config discovery is deferred; config-mediated redirection degrades to capture-unavailable, never to misattribution.
7. Structural profile is a mechanically observed key-set identity, never a producer version attestation.
8. Strict decode with unknown-member and duplicate-name rejection is the profile gate.
9. Only executed producer versions are qualified profiles.
10. The producer `success` field is never read.
11. Core `TestStatus = error` is never produced by either adapter in V1.
12. Hook phase and suite-execution-error distinction are unavailable in both adapters.
13. Suite aggregate counters are unavailable; producer buckets are never synthesized into the JUnit-shaped aggregate.
14. `jest:invocations` is mechanical because it is an integer field; Vitest flake state is unavailable because it would be an inference.
15. Failure text, stacks, and absolute host paths are never persisted.
16. File-entry order is non-deterministic in both producers; ordinals are derivation-scoped only and records are never re-sorted.
17. Multi-project invocations are unqualified.
18. Terminal receipt remains child execution truth; structured projection never mutates execution semantics.

## 61. Deferred beyond V1

- Vitest `1.x`, `2.x`, and `5.x` structural profiles until executed qualification;
- Jest `28.x` until executed qualification;
- jest-jasmine2 semantics;
- multi-project / workspace attribution;
- `--shard`, `--bail`, `--onlyFailures`, `--collectTests`, `--listTests` invocations;
- Vitest retry/flake dimensions (would require a custom reporter, which is out of scope);
- Jest `retryMessages` and `wouldRun` (`main`-only);
- Vitest `meta`, `tags`, `benchmarks` semantic consumption;
- producer-aggregate envelope for run-level counters;
- effective config discovery for either producer;
- `node`-based and wrapper producer forms;
- exact producer distribution-version attestation;
- generic "JavaScript test JSON" adapter — permanently out of scope, per §11.

## 62. Implementation sequencing

```text
A. shared JS/TS strict-profile decode scaffolding on jsonstrict
B. jest invocation binding + JEST_JASMINE presence authority + excluded-flag resolver
C. jest-json@v1 parser, profiles v29/v30, normalized semantics/identity
D. jest selection/admission/capability/inspect integration
E. jest frozen fixtures + release qualification matrix + real-daemon E2E
F. vitest invocation binding + run-mode/reporter/output-file resolver
G. vitest-json@v1 parser, profiles v3/v4
H. vitest selection/admission/capability/inspect integration
I. vitest frozen fixtures + release qualification matrix + real-daemon E2E
J. deploy
```

Steps A–E and F–I are independently shippable. No capture-layer, blob-store, retention, or P1 change is scheduled, and any need for one is a signal to stop and amend this design.

## 63. Acceptance boundary

This design is successful only if implementation proves all of the following without weakening existing E22/P1 authority:

```text
an explicitly bound vitest/jest invocation executes unchanged
→ a stale pre-existing output file cannot be misattributed
→ config-redirected output yields capture-unavailable, never wrong bytes
→ the exact terminal file object is pinned before managed-shell release
→ immutable bytes are durably captured or capture fails closed
→ an unknown key set fails closed as unsupported profile
→ a duplicate JSON member fails closed
→ statuses normalize coarsely and conservatively, with error never invented
→ jest invocations is observable; vitest flake state stays unavailable
→ jest failing:true yields a typed disposition, never a prose-derived one
→ a producer success:true on a failing receipt changes nothing
→ duplicate entries remain distinct and record order is preserved
→ no absolute host path or failure text is persisted
→ crash recovery never re-executes the runner or reopens the workspace path
→ P1 consumes the evidence with no JavaScript-specific parallel ontology
```

## 64. Final architectural position

The pytest design ended by stating that ShellBeam treats producer artifacts as engineering truth only after provenance-safe capture converts a mutable execution-side pathname into immutable ShellBeam-owned source authority, and that `pytest-junit-xml@v1` was the first consumer of that primitive rather than a special case.

This document is the test of that claim. Two adapters in a different language ecosystem, with a different serialization format, different status vocabularies, a different retry model, and no schema version at all, are added with **zero** changes to the capture, storage, retention, recovery, or evidence layers. The entire delta is one invocation resolver, one strict parser, and one coverage declaration per producer.

The extension path is unchanged:

```text
execution truth
      ↓
qualified native machine contract
      ↓
RawOutputRef OR ArtifactBlobRef
      ↓
producer-specific deterministic adapter
      ↓
shared E22 structured records
      ↓
shared P1 evidence/sufficiency semantics
```

What this design adds to that path is the discipline for producers that publish no schema version and offer no dialect selector: the observed key set becomes the gate, strict decode becomes the enforcement, and only executed versions become qualified. That discipline is reusable by ESLint and by the TypeScript compiler when their own qualification is written.
