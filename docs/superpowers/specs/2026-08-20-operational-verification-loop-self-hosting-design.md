# ShellBeam Operational Verification Loop / Self-Hosting Design

Date: 2026-08-20
Status: design approved in chat; written freeze pending review
Scope: make the existing project-command, evidence-ledger, affected-surface, and verification-semantics primitives operational for ShellBeam development itself without adding a new verification engine or automatic test scheduler

## 1. Decision

ShellBeam SHALL self-host its verification semantics using repository-authored configuration and existing execution/evidence primitives.

The first operational loop is:

```text
workspace source generation
        |
        v
inspect.verification(phase)
        |
        +--> affected surface
        +--> active repository policy
        +--> obligations / evidence requirements
        |
        v
agent chooses a required project command
        |
        v
start(project_command_id=...)
        |
        +--> immutable typed project binding
        +--> terminal receipt
        +--> evidence ledger record
        |
        v
inspect.verification(phase)
        |
        v
clear | blocked | indeterminate
```

ShellBeam SHALL remain the authority layer for command identity, source freshness, evidence compatibility, contradiction preservation, and gate folding.

The repository-local `tools/devctl` SHALL remain the selector/executor for dirty-test impact where that logic already exists. This avoids duplicating test-impact semantics inside verification policy v1.

## 2. Problem being solved

The verification engine is implemented, but this repository currently reports:

```text
project_manifest_absent
verification policy_state=absent
gate=indeterminate reason=policy_absent
```

That means an agent can inspect affected surface but cannot obtain actionable repository obligations or a meaningful completion gate.

The goal is to turn those existing primitives into a normal development loop for this repository.

## 3. Goals

The self-hosting slice SHALL provide all of the following:

1. `.shellbeam/project.toml` schema v2 exists and resolves through the normal project loader.
2. Important repository checks are addressable as typed project commands with stable binding digests.
3. `.shellbeam/verification-policy.toml` expresses phase-specific required evidence using the existing policy v1 contract.
4. A repository-authored policy can be previewed, explicitly activated, and then consumed by `inspect.verification`.
5. Inner-loop verification uses existing dirty-impact selection instead of reflexively running the full suite.
6. Checkpoint/release phases can require stronger gates than the inner loop.
7. Evidence becomes stale or otherwise insufficient when source/policy authority changes according to existing validity semantics.
8. Failed or contradictory evidence remains visible and cannot be erased by a later pass.
9. Daemon restart does not lose the activated policy/evidence truth needed to reconstruct the gate.
10. End-to-end acceptance tests exercise the real daemon/public action surface rather than only unit-testing config parsing.

## 4. Non-goals

This slice SHALL NOT:

- add a second verification engine;
- add dynamic affected-relation-to-project-command parameter substitution;
- automatically execute every obligation returned by `inspect.verification`;
- infer verification policy from conversation text;
- silently activate repository-authored policy;
- replace `tools/devctl` impact selection;
- make a full suite mandatory after every source edit;
- treat exit code alone as proof of semantic coverage beyond the command contract;
- add Vitest/Jest structured adapters as part of this work;
- redesign the existing policy schema, evidence schema, or affected-surface model unless an acceptance test exposes a real contract gap.

## 5. Existing contracts reused unchanged

### 5.1 Project manifest

The project loader already reads:

```text
.shellbeam/project.toml
```

Typed project commands require manifest schema v2 and argv mode. Schema v2 already supports `repo_package`, `repo_path`, enum, integer, and string parameters, but this self-hosting slice does not require dynamic policy parameter expansion.

### 5.2 Verification policy

The policy loader already reads:

```text
.shellbeam/verification-policy.toml
```

Policy v1 already supports:

- phase selectors;
- path/class selectors;
- required/optional obligations;
- project-command evidence requirements;
- minimum authority;
- current-source requirements;
- environment/stability/quiescence requirements;
- execution resource semantics.

### 5.3 Evidence matching

Obligations bind a policy evidence requirement to the exact project-command binding digest. Sufficiency then matches current evidence against that expected binding and the current source/policy state.

No new obligation ID needs to be injected into command execution for ordinary evidence matching. Existing `verification_attempt` remains reserved for rerun/flake lineage semantics.

### 5.4 Dirty test selection

`tools/devctl test --dirty` already:

- computes changed paths against a base ref;
- uses `dev/test-impact.toml` mappings;
- maps otherwise-unmapped Go files to their package;
- deduplicates selected suites;
- runs only selected Go suites;
- records source fingerprint, changed files, selected packages, reasons, and outcome.

The operational loop SHALL reuse this instead of recreating a package-selection DSL in verification policy v1.

## 6. Repository project manifest

Add `.shellbeam/project.toml` using schema version 2.

The initial command catalog SHOULD stay small and map to stable repository behaviors rather than every Make target.

Required commands:

### `dirty_test`

Equivalent behavior:

```text
go run ./tools/devctl test --dirty --base origin/main --json
```

Properties:

- kind: `test`
- cost: `medium`
- source scope: `affected`
- mutates source: false
- external effect: false

This is the default inner-loop evidence provider.

### `checkpoint_verify`

Equivalent behavior:

```text
go run ./tools/devctl verify --checkpoint --base origin/main --json
```

Properties:

- kind: `test`
- cost: `medium`
- source scope: `affected`
- mutates source: false
- external effect: false

This is the default checkpoint evidence provider and mirrors the repository checkpoint workflow.

### `full_test`

Equivalent behavior:

```text
go test -count=1 ./...
```

Properties:

- kind: `test`
- cost: `expensive`
- source scope: `full`
- mutates source: false
- external effect: false

### `vet_all`

Equivalent behavior:

```text
go vet ./...
```

Properties:

- kind: `inspect`
- cost: `medium`
- source scope: `full`
- mutates source: false
- external effect: false

### `fmt_check`

Equivalent behavior:

```text
make fmt-check
```

Properties:

- kind: `format`
- cost: `fast`
- source scope: `full`
- mutates source: false
- external effect: false

### `pytest_structured_gate`

Equivalent behavior:

```text
./scripts/test-pytest-structured-results.sh
```

Properties:

- kind: `test`
- cost: `medium`
- source scope: `full`
- mutates source: false
- external effect: true

The external-effect declaration is conservative because the gate creates temporary virtual environments and may install qualified pytest versions.

### `hardening`

Equivalent behavior:

```text
./scripts/test-hardening.sh
```

Properties:

- kind: `test`
- cost: `expensive`
- source scope: `full`
- mutates source: false
- external effect: false

### `security`

Equivalent behavior:

```text
./scripts/test-security.sh
```

Properties:

- kind: `inspect`
- cost: `expensive`
- source scope: `full`
- mutates source: false
- external effect: true

The exact external-effect flag MUST follow the script behavior verified during implementation; if the script never installs or reaches the network in the admitted path, this declaration may be narrowed with test evidence.

## 7. Verification profiles in the manifest

The manifest SHALL expose profiles as discoverable repository intent, but repository policy remains the authority that turns those commands into obligations.

Initial profiles:

```text
coding:
  dirty_test

checkpoint:
  fmt_check
  vet_all
  checkpoint_verify

release:
  fmt_check
  vet_all
  full_test
  hardening
  security
```

`pytest_structured_gate` is intentionally not unconditional in a generic profile because it is subsystem-specific and relatively expensive. The authored verification policy will require it when relevant paths are affected.

## 8. Repository-authored verification policy

Add `.shellbeam/verification-policy.toml` schema v1.

The policy SHALL be explicit and fail closed. It SHALL use existing project-command providers with `minimum_authority = "mechanical"`, `require_current = true`, and `stability = "no_contradiction"` unless a rule has a documented reason to be stricter.

### 8.1 Inner-loop baseline rule

At `inner_loop`, require `dirty_test`.

This rule has no path selector. A source generation with no relevant changes may satisfy the command by producing an empty devctl selection; the authority is still a real typed command execution rather than an inferred pass.

This makes the inner loop selective because devctl, not the policy engine, chooses affected suites.

### 8.2 Checkpoint baseline rules

At `checkpoint`, require:

- `fmt_check`;
- `vet_all`;
- `checkpoint_verify`.

These are stronger than the inner-loop rule but still avoid an unconditional duplicate full test when `checkpoint_verify` already runs the repository-selected test set.

### 8.3 Structured-results rule

At `checkpoint` and `pre_merge`, changes touching the pytest structured-result/capture surface SHALL additionally require `pytest_structured_gate`.

The path selector SHALL include the producer/parser/capture/storage/public-contract surfaces that can invalidate the Pytest JUnit qualification, including the dedicated fixtures/scripts and relevant structured-result packages.

The implementation plan MUST enumerate the exact patterns from the current tree and include a negative test proving an unrelated docs change does not trigger this expensive gate.

### 8.4 Verification-semantics rule

At `checkpoint` and `pre_merge`, changes touching verification policy/affected-surface/obligation/sufficiency/evidence integration SHALL additionally require a focused verification-semantics command.

If no stable focused command already exists, the manifest MAY add one argv-mode command whose test package/`-run` selection is frozen by acceptance tests. This is preferable to requiring `full_test` for every verification-subsystem edit.

### 8.5 Self-host configuration rule

Changes under `.shellbeam/**` modify the authority that names commands or defines required evidence. They SHALL therefore trigger an additional focused project/verification contract command at `checkpoint` and `pre_merge`.

This rule is required because `tools/devctl test --dirty` currently has no `.shellbeam/**` mapping and may legitimately select no Go suite for a configuration-only change. Self-host configuration must not become a blind spot in the self-hosted gate.

The focused command SHOULD cover manifest parsing/binding, policy loading/materialization, and public verification protocol acceptance without requiring the entire repository suite.

### 8.6 Release rules

At `release`, require the strongest repository-local portable set:

- `fmt_check`;
- `vet_all`;
- `full_test`;
- `hardening`;
- `security`.

Platform-specific provider qualification remains CI/release-environment evidence and SHALL NOT be falsely represented as having run locally when the required provider/platform is unavailable.

## 9. Policy activation boundary

Repository-authored policy SHALL NOT silently become active merely because a file appears or changes.

The operational setup is:

```text
repository policy file
        |
        v
verification.policy.preview
        |
        v
human/authorized activation
        |
        v
verification.policy.activate
        |
        v
materialized active policy snapshot
```

This preserves the existing approval/authority boundary.

For initial self-hosting, activation is a one-time explicit repository setup action after the policy file is committed. If the policy content changes, the new digest requires explicit review/activation again; old evidence cannot be silently reinterpreted under the new policy.

## 10. Agent execution protocol

A coding agent SHALL use the operational loop as follows.

### Before changing code

1. Inspect workspace/project readiness when relevant.
2. Inspect verification at `inner_loop` to establish policy/generation state.

### During implementation

1. Make a coherent source change.
2. Call `inspect.verification` at `inner_loop`.
3. Read required-now obligations and their evidence requirements.
4. Execute the cheapest required typed project command(s); normally `dirty_test`.
5. Inspect evidence/structured results only as needed to diagnose failures.
6. Re-inspect verification.
7. If source changes again, do not reuse a prior completion claim without a fresh verification inspection.

### At checkpoint

1. Call `inspect.verification` with `phase=checkpoint`.
2. Execute missing required evidence only.
3. Run subsystem-specific gates only when their rules are triggered.
4. Re-inspect until the gate is `clear`, or report `blocked`/`indeterminate` with reason codes.

### Before a completion claim

The agent SHALL NOT claim the work is verified merely because the most recent command passed.

A positive completion claim requires fresh `inspect.verification` evidence for the intended phase and no unresolved required obligation. `indeterminate` is not equivalent to `clear`.

## 11. Source and policy drift semantics

Existing validity semantics are authoritative.

At minimum, the acceptance suite SHALL prove:

```text
generation A
  -> required command PASS
  -> gate clear

source mutates to generation B
  -> old evidence is not sufficient for B
  -> gate no longer clear until compatible/current evidence exists
```

Likewise:

```text
policy digest P1
  -> evidence satisfies P1

repository policy changes to P2
  -> P1 evidence does not silently satisfy P2
  -> P2 requires explicit activation
```

The implementation SHALL NOT add special-case freshness shortcuts merely to make self-hosting convenient.

## 12. Contradictory evidence

The existing no-contradiction semantics SHALL be preserved.

A failed current attempt followed by an unqualified pass SHALL NOT erase the failure from historical truth. Rerun/flake lineage continues to use the existing `verification_attempt` contract where appropriate.

Self-hosting policy SHALL not weaken stability requirements to make gates easier to clear.

## 13. Failure behavior

The loop SHALL fail closed in these cases:

- project manifest absent/invalid/unsupported;
- typed project command cannot resolve;
- repository policy absent/invalid/unactivated;
- affected-surface authority is below the rule minimum;
- required provider is unavailable;
- command binding digest differs from the policy-bound expectation;
- evidence is stale/unknown when current evidence is required;
- contradictory evidence violates stability requirements;
- quiescence is required but not established;
- source/policy changes during an attempted completion check.

The public result should remain `blocked` or `indeterminate` according to existing semantics; self-hosting SHALL NOT invent a new truth state.

## 14. Structured result integration

Structured results are complementary evidence detail, not a replacement for terminal/evidence truth.

For commands with a qualified structured adapter, the agent may use `inspect.structured` to locate failing tests and understand semantic coverage.

For `pytest-junit-xml`, immutable `artifact_blob` authority and completeness status remain the parser source of truth even if the workspace report file later changes.

The verification gate SHALL continue to rely on the evidence contract/project binding/source validity. It SHALL NOT infer gate success by re-aggregating structured testcase records.

## 15. No automatic obligation executor in V1

`inspect.verification` tells the agent what evidence is missing; it does not autonomously execute commands.

This is intentional because execution may involve:

- expensive tests;
- external effects;
- exclusive resources;
- platform limitations;
- debugging choices after failure.

A future planner may choose among admissible evidence providers using verification economics, but that is outside this slice.

## 16. Implementation shape

The expected first implementation should be configuration-heavy and code-light:

```text
.shellbeam/project.toml                       new
.shellbeam/verification-policy.toml           new
cmd/shellbeam/*self_host* acceptance tests    new or focused additions
docs/... operational usage                    small update if needed
```

Core engine changes are NOT planned.

If an end-to-end test reveals a missing behavior in an existing contract, implementation MUST stop and classify that as a separate contract gap before changing public semantics.

## 17. Acceptance matrix

The implementation is complete only when fresh real-daemon tests prove all of these cases.

### A. Discovery and binding

1. `inspect.project` reports a valid manifest v2 for this repository fixture.
2. Every policy-referenced project command resolves to a stable binding digest.
3. An unknown or mutated command binding fails closed.

### B. Policy lifecycle

1. Repository policy previews successfully.
2. Before activation, verification does not treat the proposal as active truth.
3. After explicit activation, `inspect.verification` exposes the expected obligations.
4. Policy-content mutation invalidates the old active-policy match until explicitly handled under existing semantics.

### C. Inner loop

1. A local Go change triggers the inner-loop baseline obligation.
2. `dirty_test` passes and satisfies the obligation for the current source.
3. A subsequent source edit makes the prior evidence insufficient/currentness-invalid as required by existing validity semantics.
4. A new pass clears the obligation again.

### D. Checkpoint

1. Checkpoint exposes format, vet, and checkpoint verification requirements.
2. Missing one required evidence item keeps the gate non-clear.
3. Supplying all current compatible evidence clears the gate.

### E. Subsystem-specific gate

1. A pytest structured-result surface change triggers `pytest_structured_gate`.
2. An unrelated docs-only change does not trigger that gate.
3. A failing subsystem gate is blocking evidence and cannot be hidden by unrelated passing evidence.

### F. Self-host configuration authority

1. A `.shellbeam/project.toml` change triggers the focused project/verification contract rule even if devctl dirty selection is empty.
2. A `.shellbeam/verification-policy.toml` change cannot inherit active-policy truth from the previous digest.
3. Invalid manifest or policy content leaves the gate non-clear with typed diagnostics.

### G. Contradiction and restart

1. Contradictory current evidence produces the existing inconsistent/blocked semantics.
2. Daemon restart reconstructs policy/evidence/gate truth without rerunning commands.

### H. Truth-boundary regression

1. `inspect.structured` cannot turn a failed terminal command into a passing verification result.
2. A modified report file after terminal capture does not rewrite retained artifact-derived structured truth.
3. `indeterminate` is never serialized or surfaced as `clear`.

## 18. Rollout sequence

Implementation SHALL proceed in this order:

1. Add manifest fixture/config and parsing/binding acceptance.
2. Add repository policy and preview/activation acceptance.
3. Prove inner-loop dirty-test evidence end to end.
4. Prove source drift invalidates prior sufficiency.
5. Add checkpoint baseline requirements.
6. Add self-host-config, structured-results, and verification-subsystem specialized rules.
7. Prove contradiction and daemon-restart behavior.
8. Run focused tests, then the repository checkpoint gate.
9. Only after all acceptance evidence is fresh should the repo use the self-hosted gate as a normal completion signal.

## 19. Deferred follow-ups

The following are explicitly deferred:

- policy parameter templates such as binding `{affected_package}` from mechanical affected relations;
- automatic cheapest-evidence execution;
- generic language-agnostic JUnit fallback;
- Vitest/Jest structured adapters;
- remote/CI evidence federation;
- policy auto-activation;
- converting devctl selection receipts into a new first-class affected-scope proof unless a separate design demonstrates the need.

## 20. Final architectural position

The operational self-hosting loop is intentionally an integration of existing authoritative parts, not another layer of heuristics.

```text
Project Manifest v2
      -> names executable repository capabilities

Verification Policy v1
      -> states what evidence is required by phase/surface

Affected Surface
      -> states what changed and with what authority

Typed Project Command + Receipt + Evidence Ledger
      -> states what actually ran against which source/binding

Structured Results
      -> adds bounded diagnostic/test detail where qualified

Verification Sufficiency
      -> decides whether current evidence satisfies current obligations
```

`tools/devctl` remains an implementation detail of repository-local selective testing. ShellBeam owns the truth boundary around that execution.

This gives the agent a practical rule for completion:

> Do not ask whether some tests passed. Ask whether the current active policy has any unresolved required verification obligations for the current source generation and phase.
