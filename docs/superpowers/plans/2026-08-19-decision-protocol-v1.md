# Decision Protocol V1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the frozen Decision Protocol V1 so ShellBeam can durably preregister candidate predictions, bind one normal execution to a sealed experiment, derive non-cherry-pickable machine observations, enforce a bounded activated decision policy, and authorize an auditable candidate commitment without moving engineering reasoning or command scheduling into the daemon.

**Architecture:** Add a new `decisionprotocol` core/application domain backed by an append-only canonical ledger in the existing filesystem store. Decision policy/episode/candidate/experiment state is projection over immutable records; ordinary execution remains the only command-start primitive, with `experiment_id` added to immutable observation/replay identity at admission. Typed observation materialization consumes canonical receipt/structured/verification truth, policy evaluation produces a candidate-scoped gate and semantic `projection_digest`, and terminal selection uses that digest as CAS plus durable idempotency. Verifier provenance and override authority stay separate from engineering evidence; authority is provider-qualified at commit time.

**Tech Stack:** Go module floor `go 1.26.0`; authoring host `go1.26.6 darwin/arm64`; Go standard library in core; existing filesystem store/atomic writer, daemon, IPC v2, MCP `local_shell`, JSON Schema 2020-12, existing verification/structured-result/evidence primitives.

**Spec:** `docs/superpowers/specs/2026-08-19-decision-protocol-design.md` — frozen SHA-256 `6cf49426243f26e8bec862c29651304ccc4abd5e1f91947f9899fe21fd72f7fa`.

**Compatibility Amendment:** `docs/superpowers/specs/2026-08-20-decision-protocol-structured-capture-compatibility.md` authorizes the reviewed upstream owner patch through `39de4426a95cfb58cbb99a75165b9feb5cc7169c`; owner patch SHA-256 `d9d1429a2a6e0ce413b978fc89cdfc1505a1b1a24c0527d7ae1c54a51c91c28d`.

**Plan authoring base:** `27207d94b097040b571081c8c49d9c09487460c5`.

**Traceability Gate:** `docs/superpowers/plans/2026-08-19-decision-protocol-v1-traceability.json` is normative plan-review metadata. Before Task 0 implementation begins, run `python3 scripts/check-decision-protocol-v1-plan-traceability.py`; it MUST report `PASS invariants=48/48 sections=57/57 tasks=14/14`. A failure blocks implementation and requires plan amendment rather than executor improvisation.

## Global Constraints

- The frozen written design is authoritative. Do not reopen architecture during implementation; implementation ambiguity is a plan/spec review issue.
- ShellBeam owns protocol state, machine-grounded expectation evaluation, policy/budget enforcement, and authorization boundaries; the caller/model owns hypotheses, experiment choice, semantic preference, and engineering judgment.
- Decision Protocol never schedules commands. `decision.experiment.define`, `prediction.bind`, `experiment.seal`, `experiment.close`, `decision.evaluate`, and selection operations must not call process start.
- Existing operations without `experiment_id` keep unchanged semantics. Upstream structured-capture authority is composed, not replaced: observation identity is `legacy -> experiment(if present) -> verification attempt(if present) -> structured capture(if present)`, so omitted experiment identity is byte-for-byte upstream-compatible.
- The reviewed owner-overlap ratchet is `39de4426a95cfb58cbb99a75165b9feb5cc7169c`. Future owner drift after that SHA fails closed for another compatibility review.
- V1 allows at most one observation-producing `ExperimentExecutionLink` per experiment.
- `experiment_id` is immutable first-admission observation/replay identity. Same `operation_id` with omitted/changed experiment binding conflicts.
- Successful experiment-linked reservation plus durable `ExperimentExecutionLink` is recovery-indivisible and happens before spawn.
- Canonical Decision Protocol records are immutable; lifecycle is projected. Event journal `ChangeSeq` is not canonical replay authority.
- The Decision Protocol canonical ledger assigns a monotonic `canonical_record_seq`; seal-time replay cuts use canonical-ledger high-water plus exact policy binding.
- Caller input never authors `ExperimentObservationBinding.prediction_results`, `qualified_context_class`, `context_qualification`, or canonical authority attestation bodies.
- Observation truth is server-derived from the complete attributable linked-operation cut; no favorable-fact cherry-picking and no arbitrary historical workspace evidence.
- V1 predicate kinds are exactly `OPERATION_OUTCOME`, `STRUCTURED_TEST_STATUS`, `STRUCTURED_DIAGNOSTIC_PRESENCE`, and `VERIFICATION_RESULT`; no expression language, regex selectors, JSONPath, Boolean nesting, or generic comparator.
- `REQUIRED_PREDICTION + determinate MISMATCH` creates the implicit candidate-contract blocker; `NOT_EVALUATED` and `INDETERMINATE` do not.
- Policy requirements are implicit top-level AND over at most one of each V1 kind: challenge, prediction evaluation, discrimination, verifier assessment.
- Budget admission is a separate ceiling and never changes protocol satisfaction.
- `DecisionProjectionDigest` is semantic CAS state; literal interchangeable audit record IDs belong in `DecisionAuditDigest`, not the projection digest.
- New episodes server-resolve the current effective applicable Decision Policy activation; callers cannot select a historical weaker activation.
- Decision Policy activation V1 authority is exactly `explicit_caller`; Decision Authority Attestation is not a policy-activation authority.
- `SelectionProposal` has zero transition authority.
- Selection commit and close-unresolved share one terminal serialization boundary. Normal and override commits are epistemically distinct.
- Source-generation mismatch is non-overrideable in V1.
- Authority-class and scope matching are exact in V1; no implicit lattice, wildcard, fuzzy equivalence, or generic core revocation ledger.
- Only provider/server-qualified verifier provenance can satisfy `required_context_class`; caller declaration alone has zero hard-gate authority.
- Only provider/resolver-backed canonical `DecisionAuthorityAttestation` records are usable; unknown/unavailable qualification fails closed.
- Override authority is revalidated at terminal commit authorization time; failed/non-durable retry revalidates, durable idempotent replay does not.
- Candidate committed does not imply implementation correct or task done; existing affected-surface/verification semantics remain downstream.
- No V1 path materializes multiple full mutation candidates concurrently in one workspace.
- Keep the single MCP tool `local_shell`; add actions to its existing bounded action surface rather than creating a second MCP tool.
- Core packages use the Go standard library only.
- Production hard cap 500 lines/file, test hard cap 800 lines/file, function hard cap 80 lines, interface hard cap 8 methods. Split by responsibility before exceeding a cap.
- Preserve unrelated work. No push, PR, merge, or cleanup unless explicitly requested.
- Every product-code task follows focused RED → minimum GREEN → targeted tests → `go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json` → `git diff --check` → tracked commit. Task 0 persists the exact implementation base in baseline evidence and creates `scripts/decision-protocol-v1-implementation-base.sh`; every Task 1–13 must re-read and verify that durable authority before using `SHELLBEAM_BASE_REF`. A process-local export from an earlier task is never authoritative.

---

## File Structure Locked by This Plan

Core domain:

```text
internal/core/decisionprotocol/
  identity.go          typed IDs, canonical digest helpers, record envelope/cut refs
  policy.go            policy content, four requirement payloads, budget, override policy
  episode.go           episode/candidate/terminal record types and lifecycle enums
  experiment.go        experiment/seal/prediction/link/observation/close/abort record types
  predicate.go         closed V1 typed predicates, dimension keys, evaluation status
  assessment.go        verifier assessment + qualification types
  authority.go         authority class/scope/attestation/qualification/override types
  selection.go         proposal/commit intents, semantic idempotency fingerprint
  projection.go        requirement/gate/projection/digest types
  validation.go        closed-vocabulary and cross-field validation
  *_test.go
```

Application domain:

```text
internal/app/decisionprotocol/
  ports.go             narrow store/source/receipt/structured/verification/authority ports
  policy.go            snapshot + explicit-caller activation + effective lookup
  episode.go           create + lifecycle projection
  candidate.go         create/revise + lineage projection
  experiment.go        define/bind/seal/close/abort orchestration without scheduling
  observation.go       complete attributable observation derivation/materialization
  predicates.go        typed predicate evaluators
  evaluate.go          requirements, contract blockers, discrimination, protocol gate
  projection.go        bounded model-facing projection + semantic/audit digests
  selection.go         proposal, terminal CAS, durable idempotency, close-unresolved
  assessment.go        caller declaration + trusted context qualification admission
  authority.go         materialize/requalify + override authorization cut
  service.go           facade composed from the focused components
  *_test.go
```

Store:

```text
internal/adapter/store/
  decision_protocol_paths.go
  decision_protocol_ledger.go
  decision_protocol_policy.go
  decision_protocol_episode.go
  decision_protocol_experiment.go
  decision_protocol_selection.go
  decision_protocol_authority.go
  focused *_test.go and fault/race tests
```

Execution binding:

```text
internal/core/operation/intent.go
internal/core/operation/persistence.go
internal/core/operation/*decision_protocol*_test.go
internal/app/daemon/types.go
internal/app/daemon/admission.go
internal/app/daemon/project_command.go
internal/app/daemon/store_port.go
internal/app/daemon/*decision_protocol*_test.go
internal/adapter/store/admission.go
internal/adapter/store/*decision_protocol*_test.go
api/schema/operation-v2.json
api/schema/operation-v3.json
```

Transport/composition:

```text
internal/adapter/ipc/decision_protocol_v2.go
internal/adapter/ipc/decision_protocol_test.go
internal/adapter/ipc/protocol_v2.go
internal/adapter/mcp/decision_protocol_input.go
internal/adapter/mcp/decision_protocol_call.go
internal/adapter/mcp/decision_protocol_test.go
internal/app/bridge/decision_protocol.go
internal/app/bridge/decision_protocol_test.go
internal/app/bridge/handler.go
cmd/shellbeam/decision_protocol.go
cmd/shellbeam/decision_protocol_test.go
cmd/shellbeam/command_daemon.go
internal/core/capability/decision_protocol.go
internal/core/capability/decision_protocol_test.go
internal/core/capability/catalog.go
api/schema/ipc-v2.json
api/schema/mcp-input-v2.json
api/schema/mcp-output-v2.json
```

Planning/execution evidence:

```text
scripts/check-decision-protocol-v1-plan-traceability.py
docs/superpowers/plans/2026-08-19-decision-protocol-v1-traceability.json
docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md   # created during Task 0 implementation
```

---

### Task 0: Bind the implementation base and prove the unchanged baseline

**Files:**
- Create during implementation: `docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md`
- Create during implementation: `scripts/decision-protocol-v1-implementation-base.sh`
- Read-only gate: `docs/superpowers/specs/2026-08-19-decision-protocol-design.md`
- Read-only gate: `docs/superpowers/plans/2026-08-19-decision-protocol-v1-traceability.json`

**Interfaces:**
- Resolves the **current local `main` ref**, not the planning worktree HEAD, and requires the plan-authoring base to remain its ancestor.
- Audits owner overlap over `plan_authoring_base..current_main` before implementation. Any overlap in the frozen owner set stops execution and requires plan amendment/re-review.
- If current `main` advanced only outside the owner set, integrates/rebases the implementation workspace onto that exact accepted `main` SHA before product code changes.
- Persists that exact accepted `main` SHA as `implementation_base` in baseline evidence. The evidence file is the durable base authority; shell environment is not.
- Creates `scripts/decision-protocol-v1-implementation-base.sh`, which every Task 1–13 invokes to rebind `SHELLBEAM_BASE_REF`, verify current `main` still equals the recorded base, and verify the recorded base remains an ancestor of implementation HEAD.
- Produces baseline evidence recording accepted main SHA, frozen spec SHA, `go version`, full-suite result, targeted existing admission/policy primitive results, and owner-overlap audit.
- Does not modify product code.

- [ ] **Step 1: Verify plan/spec integrity before touching product code**

Run:

```bash
python3 scripts/check-decision-protocol-v1-plan-traceability.py
shasum -a 256 docs/superpowers/specs/2026-08-19-decision-protocol-design.md
git diff --check
git status --short --branch
```

Expected:

```text
PASS invariants=48/48 sections=57/57 tasks=14/14
6cf49426243f26e8bec862c29651304ccc4abd5e1f91947f9899fe21fd72f7fa  docs/superpowers/specs/2026-08-19-decision-protocol-design.md
```

Worktree must be clean before implementation begins.

- [ ] **Step 2: Resolve current `main`, audit plan-authoring drift, and integrate the implementation workspace**

Run:

```bash
PLAN_AUTHORING_BASE=27207d94b097040b571081c8c49d9c09487460c5
REVIEWED_OWNER_OVERLAP_BASE=39de4426a95cfb58cbb99a75165b9feb5cc7169c
REVIEWED_OWNER_PATCH_SHA256=d9d1429a2a6e0ce413b978fc89cdfc1505a1b1a24c0527d7ae1c54a51c91c28d
REVIEWED_OWNER_PATHS_SHA256=2f5ae958bce1c3d33ab3ea391e85c8d24cf48afdc140512cdbe41ae4635cd05f
BASELINE_EVIDENCE=docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md
OWNER_PATHS=(
  internal/core/operation internal/app/daemon internal/adapter/store internal/core/verification
  internal/app/verification internal/adapter/ipc internal/adapter/mcp internal/app/bridge
  internal/core/capability cmd/shellbeam api/schema
)
CURRENT_MAIN="$(git rev-parse main)"
if [ -f "$BASELINE_EVIDENCE" ]; then
  PREVIOUS_IMPLEMENTATION_BASE="$(awk -F'`' '/^- implementation_base: `/ {print $2; exit}' "$BASELINE_EVIDENCE")"
else
  PREVIOUS_IMPLEMENTATION_BASE="$PLAN_AUTHORING_BASE"
fi
if [[ ! "$PREVIOUS_IMPLEMENTATION_BASE" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'invalid previous Decision Protocol implementation base authority' >&2
  exit 1
fi
printf 'plan_authoring_base=%s\nprevious_implementation_base=%s\nreviewed_owner_overlap_base=%s\ncurrent_main=%s\n' \
  "$PLAN_AUTHORING_BASE" "$PREVIOUS_IMPLEMENTATION_BASE" "$REVIEWED_OWNER_OVERLAP_BASE" "$CURRENT_MAIN"

git merge-base --is-ancestor "$PLAN_AUTHORING_BASE" "$REVIEWED_OWNER_OVERLAP_BASE"
test "$(git diff --binary "$PLAN_AUTHORING_BASE".."$REVIEWED_OWNER_OVERLAP_BASE" -- "${OWNER_PATHS[@]}" | shasum -a 256 | awk '{print $1}')" = "$REVIEWED_OWNER_PATCH_SHA256"
test "$(git diff --name-only "$PLAN_AUTHORING_BASE".."$REVIEWED_OWNER_OVERLAP_BASE" -- "${OWNER_PATHS[@]}" | LC_ALL=C sort | shasum -a 256 | awk '{print $1}')" = "$REVIEWED_OWNER_PATHS_SHA256"
git merge-base --is-ancestor "$REVIEWED_OWNER_OVERLAP_BASE" "$CURRENT_MAIN"
OWNER_DRIFT="$(git diff --name-only "$REVIEWED_OWNER_OVERLAP_BASE".."$CURRENT_MAIN" -- "${OWNER_PATHS[@]}")"
if [ -n "$OWNER_DRIFT" ]; then
  printf '%s\n' "$OWNER_DRIFT"
  echo 'Decision Protocol owner overlap after reviewed compatibility base; stop for another plan amendment/re-review.' >&2
  exit 1
fi

git merge-base --is-ancestor "$PREVIOUS_IMPLEMENTATION_BASE" "$CURRENT_MAIN"
if ! git merge-base --is-ancestor "$CURRENT_MAIN" HEAD; then
  test "$(git merge-base HEAD "$CURRENT_MAIN")" = "$PREVIOUS_IMPLEMENTATION_BASE"
  git rebase --onto "$CURRENT_MAIN" "$PREVIOUS_IMPLEMENTATION_BASE"
fi

git merge-base --is-ancestor "$CURRENT_MAIN" HEAD
IMPLEMENTATION_BASE="$CURRENT_MAIN"
printf 'accepted_implementation_base=%s\n' "$IMPLEMENTATION_BASE"
```

The two base identities have different authority and MUST NOT be conflated:

- `PLAN_AUTHORING_BASE` remains the immutable origin of the original review.
- `REVIEWED_OWNER_OVERLAP_BASE` is the exact upstream owner patch explicitly reviewed by the compatibility amendment; its patch and path-list digests must match before integration.
- `PREVIOUS_IMPLEMENTATION_BASE` is the last durably accepted implementation base and remains the only replay/topology boundary for implementation commits.

The only allowed automatic integration is replaying reviewed Decision Protocol commits from `PREVIOUS_IMPLEMENTATION_BASE` onto a `current_main` descended from `REVIEWED_OWNER_OVERLAP_BASE` with no additional owner-path drift after that reviewed SHA. Non-owner drift may advance normally; any new owner overlap or unexpected topology stops.

- [ ] **Step 3: Run the unchanged full baseline and targeted primitive regression on the accepted base lineage**

Run:

```bash
go test ./...
go test ./internal/core/operation ./internal/core/verification ./internal/app/verification ./internal/adapter/store -count=1
```

Expected: both commands exit 0. A baseline failure is investigated before Decision Protocol code is written.

- [ ] **Step 4: Persist the implementation base authority and exact rebinding helper**

This step MUST be valid in a fresh shell; it does not consume variables exported by Step 2. Re-resolve and re-audit the accepted base before writing evidence:

```bash
PLAN_AUTHORING_BASE=27207d94b097040b571081c8c49d9c09487460c5
REVIEWED_OWNER_OVERLAP_BASE=39de4426a95cfb58cbb99a75165b9feb5cc7169c
REVIEWED_OWNER_PATCH_SHA256=d9d1429a2a6e0ce413b978fc89cdfc1505a1b1a24c0527d7ae1c54a51c91c28d
REVIEWED_OWNER_PATHS_SHA256=2f5ae958bce1c3d33ab3ea391e85c8d24cf48afdc140512cdbe41ae4635cd05f
BASELINE_EVIDENCE=docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md
OWNER_PATHS=(internal/core/operation internal/app/daemon internal/adapter/store internal/core/verification internal/app/verification internal/adapter/ipc internal/adapter/mcp internal/app/bridge internal/core/capability cmd/shellbeam api/schema)
if [ -f "$BASELINE_EVIDENCE" ]; then
  PREVIOUS_IMPLEMENTATION_BASE="$(awk -F'`' '/^- implementation_base: `/ {print $2; exit}' "$BASELINE_EVIDENCE")"
else
  PREVIOUS_IMPLEMENTATION_BASE="$PLAN_AUTHORING_BASE"
fi
IMPLEMENTATION_BASE="$(git rev-parse main)"
git merge-base --is-ancestor "$PLAN_AUTHORING_BASE" "$REVIEWED_OWNER_OVERLAP_BASE"
test "$(git diff --binary "$PLAN_AUTHORING_BASE".."$REVIEWED_OWNER_OVERLAP_BASE" -- "${OWNER_PATHS[@]}" | shasum -a 256 | awk '{print $1}')" = "$REVIEWED_OWNER_PATCH_SHA256"
test "$(git diff --name-only "$PLAN_AUTHORING_BASE".."$REVIEWED_OWNER_OVERLAP_BASE" -- "${OWNER_PATHS[@]}" | LC_ALL=C sort | shasum -a 256 | awk '{print $1}')" = "$REVIEWED_OWNER_PATHS_SHA256"
git merge-base --is-ancestor "$REVIEWED_OWNER_OVERLAP_BASE" "$IMPLEMENTATION_BASE"
git merge-base --is-ancestor "$PREVIOUS_IMPLEMENTATION_BASE" "$IMPLEMENTATION_BASE"
test -z "$(git diff --name-only "$REVIEWED_OWNER_OVERLAP_BASE".."$IMPLEMENTATION_BASE" -- "${OWNER_PATHS[@]}")"
git merge-base --is-ancestor "$IMPLEMENTATION_BASE" HEAD
GO_VERSION="$(go version)"
cat > docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md <<EOFBASE
# Decision Protocol V1 Implementation Baseline

- implementation_base: \`${IMPLEMENTATION_BASE}\`
- previous_implementation_base: \`${PREVIOUS_IMPLEMENTATION_BASE}\`
- plan_authoring_base: \`27207d94b097040b571081c8c49d9c09487460c5\`
- frozen_spec_sha256: \`6cf49426243f26e8bec862c29651304ccc4abd5e1f91947f9899fe21fd72f7fa\`
- go_version: \`${GO_VERSION}\`
- full_suite: PASS
- targeted_admission_policy_store: PASS
- owner_overlap_since_plan_authoring: REVIEWED_THROUGH_39de4426a95cfb58cbb99a75165b9feb5cc7169c
- reviewed_owner_patch_sha256: \`d9d1429a2a6e0ce413b978fc89cdfc1505a1b1a24c0527d7ae1c54a51c91c28d\`
- reviewed_owner_paths_sha256: \`2f5ae958bce1c3d33ab3ea391e85c8d24cf48afdc140512cdbe41ae4635cd05f\`
EOFBASE

cat > scripts/decision-protocol-v1-implementation-base.sh <<'EOFSCRIPT'
#!/usr/bin/env bash
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
EVIDENCE="$ROOT/docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md"
base="$(awk -F'`' '/^- implementation_base: `/ {print $2; exit}' "$EVIDENCE")"
if [[ ! "$base" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'invalid or missing Decision Protocol implementation_base evidence' >&2
  exit 1
fi
current_main="$(git -C "$ROOT" rev-parse main)"
if [ "$current_main" != "$base" ]; then
  printf 'Decision Protocol implementation base drift: recorded=%s current_main=%s\n' "$base" "$current_main" >&2
  exit 42
fi
git -C "$ROOT" merge-base --is-ancestor "$base" HEAD || {
  echo 'Decision Protocol implementation HEAD is not descended from recorded implementation base' >&2
  exit 1
}
printf '%s\n' "$base"
EOFSCRIPT
chmod +x scripts/decision-protocol-v1-implementation-base.sh
```

If `main` advances after this point, the helper fails closed before the next task. Do not edit evidence by hand. Repeat Task 0: read the existing durable `implementation_base` as `PREVIOUS_IMPLEMENTATION_BASE`, verify the frozen reviewed owner patch, audit only new owner drift after `REVIEWED_OWNER_OVERLAP_BASE`, require the previous base to be an ancestor of new `main`, replay from the previous base only, rerun baseline verification, then replace the durable base with the newly accepted `current_main`. Re-review on any post-ratchet owner overlap or unexpected topology.

- [ ] **Step 5: Verify the durable authority and commit baseline evidence/helper**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
test "$SHELLBEAM_BASE_REF" = "$(git rev-parse main)"
git add docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md scripts/decision-protocol-v1-implementation-base.sh
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: bind decision protocol implementation baseline"
```

---

### Task 1: Define the closed Decision Protocol core contracts

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/core/decisionprotocol/identity.go`
- Create: `internal/core/decisionprotocol/policy.go`
- Create: `internal/core/decisionprotocol/episode.go`
- Create: `internal/core/decisionprotocol/experiment.go`
- Create: `internal/core/decisionprotocol/predicate.go`
- Create: `internal/core/decisionprotocol/assessment.go`
- Create: `internal/core/decisionprotocol/authority.go`
- Create: `internal/core/decisionprotocol/selection.go`
- Create: `internal/core/decisionprotocol/projection.go`
- Create: `internal/core/decisionprotocol/validation.go`
- Create focused `*_test.go` files beside each responsibility.

**Interfaces:**
- Produces all 17 frozen canonical record bodies plus `CanonicalRecordEnvelope`, `DecisionProjectionCutRef`, closed enums, policy content, typed predicates, gate/projection types, and canonical digest/fingerprint helpers.
- Core must import only standard library plus existing ShellBeam core packages for already-canonical IDs such as workspace/operation/evidence/verification types; it must not import app/store/transport packages.
- JSON fields use the frozen spec spelling even when Go type names omit redundant `Decision` prefixes.

- [ ] **Step 1: Write RED tests for closed vocabulary, 17 canonical shapes, and cross-field validation**

Create table-driven tests that instantiate every canonical body and assert `Validate()` behavior. Minimum cases:

```go
func TestCanonicalRecordKindsAreExactlyFrozenV1Set(t *testing.T) {
    want := []RecordKind{
        RecordPolicySnapshot, RecordPolicyActivation, RecordEpisode, RecordCandidate,
        RecordExperiment, RecordExperimentSeal, RecordPredictionBinding,
        RecordExperimentExecutionLink, RecordExperimentObservationBinding,
        RecordExperimentClosure, RecordExperimentAbort, RecordVerifierAssessment,
        RecordSelectionProposal, RecordAuthorityAttestation, RecordOverride,
        RecordSelectionCommit, RecordClosure,
    }
    if got := CanonicalRecordKinds(); !slices.Equal(got, want) { t.Fatalf("kinds=%v", got) }
}

func TestOverridePolicyValidationIsExact(t *testing.T) {
    if err := (OverridePolicy{Allowed: true}).Validate(); err == nil { t.Fatal("allowed override accepted without authority class") }
    if err := (OverridePolicy{Allowed: false, RequiredAuthorityClass: &AuthorityClass{Domain:"repo", ClassID:"owner", Version:1}}).Validate(); err == nil { t.Fatal("disabled override accepted authority class") }
}

func TestAbortPhaseControlsExecutionLinkPresence(t *testing.T) {
    before := ExperimentAbort{Phase: AbortBeforeExecution, ExecutionLinkID: "link-1"}
    if err := before.Validate(); err == nil { t.Fatal("before-execution abort accepted link") }
}
```

Run:

```bash
go test ./internal/core/decisionprotocol -count=1
```

Expected: RED because the package/types do not exist.

- [ ] **Step 2: Implement typed IDs, canonical envelopes, policy/record vocabulary, and validation**

Use explicit string types and constructors rather than accepting arbitrary empty strings:

```go
package decisionprotocol

type EpisodeID string
type CandidateID string
type ExperimentID string
type PredictionID string
type RecordSeq uint64

type CanonicalRecordEnvelope struct {
    SchemaVersion      int             `json:"schema_version"`
    CanonicalRecordSeq RecordSeq       `json:"canonical_record_seq"`
    Kind               RecordKind      `json:"kind"`
    Body               json.RawMessage `json:"body"`
}

type DecisionProjectionCutRef struct {
    EpisodeID           EpisodeID `json:"episode_id"`
    CanonicalRecordHighWater RecordSeq `json:"canonical_record_high_water"`
}
```

Implement all frozen enums as closed constants. Do not expose a generic predicate/operator struct. Freeze the V1 machine-facing reason-code vocabulary exactly as:

```go
type ReasonCode string
const (
    ReasonCandidateRevisionConflict             ReasonCode = "CANDIDATE_REVISION_CONFLICT"
    ReasonExperimentAlreadySealed               ReasonCode = "EXPERIMENT_ALREADY_SEALED"
    ReasonExperimentExecutionLimitReached       ReasonCode = "EXPERIMENT_EXECUTION_LIMIT_REACHED"
    ReasonExperimentNotSealed                   ReasonCode = "EXPERIMENT_NOT_SEALED"
    ReasonObservationNotSettled                 ReasonCode = "OBSERVATION_NOT_SETTLED"
    ReasonExperimentObservationBindingConflict  ReasonCode = "EXPERIMENT_OBSERVATION_BINDING_CONFLICT"
    ReasonStaleEpisodeSourceGeneration          ReasonCode = "STALE_EPISODE_SOURCE_GENERATION"
    ReasonProjectionConflict                    ReasonCode = "PROJECTION_CONFLICT"
    ReasonPolicyConflict                        ReasonCode = "POLICY_CONFLICT"
    ReasonEpisodeTerminalConflict               ReasonCode = "EPISODE_TERMINAL_CONFLICT"
    ReasonTerminalSelectionConflict             ReasonCode = "TERMINAL_SELECTION_CONFLICT"
    ReasonIdempotencyConflict                   ReasonCode = "IDEMPOTENCY_CONFLICT"
    ReasonProtocolBlocked                       ReasonCode = "PROTOCOL_BLOCKED"
    ReasonProtocolIndeterminate                 ReasonCode = "PROTOCOL_INDETERMINATE"
    ReasonOverrideScopeStale                    ReasonCode = "OVERRIDE_SCOPE_STALE"
    ReasonOverrideAuthorityNotAdmissible        ReasonCode = "OVERRIDE_AUTHORITY_NOT_ADMISSIBLE"
    ReasonAuthorityRequirementUnavailable       ReasonCode = "AUTHORITY_REQUIREMENT_UNAVAILABLE"
)
```

Transport may wrap these in existing ShellBeam failure/result envelopes but may not rename or infer recommended next actions from them.

- [ ] **Step 3: Implement canonical policy digest, observation dimension key, projection digest, audit digest, and selection intent fingerprint helpers**

Canonicalization must sort semantically unordered collections before hashing and must reject duplicates rather than letting map iteration define identity. Public signatures:

```go
func PolicyDigest(PolicyContent) (string, error)
func ObservationDimensionKey(ObservationPredicate) (string, error)
func ProjectionDigest(ProjectionSemanticState) (string, error)
func AuditDigest(AuditState) (string, error)
func SelectionIntentFingerprint(SelectionCommitIntent) (string, error)
```

`ProjectionDigest` includes normalized verifier semantic state but excludes literal interchangeable basis/audit IDs. `SelectionIntentFingerprint` covers episode, candidate, actor, policy digest, projection digest, canonical source generation, override boolean, and override ref.

- [ ] **Step 4: Add focused digest/normalization tests**

Required cases:

```go
func TestProjectionDigestIgnoresEquivalentAuditRefsButIncludesVerifierSemanticState(t *testing.T)
func TestSelectionIntentFingerprintDistinguishesOverrideFromNormalCommit(t *testing.T)
func TestObservationDimensionKeyExcludesExpectedOutcome(t *testing.T)
func TestPolicyDigestRejectsDuplicateRequirementKind(t *testing.T)
```

Run:

```bash
go test ./internal/core/decisionprotocol -count=1
```

Expected: PASS.

- [ ] **Step 5: Run the task gate and commit**

```bash
go test ./internal/core/decisionprotocol -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/core/decisionprotocol
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: add decision protocol core contracts"
```

---

### Task 2: Add the append-only canonical ledger and Decision Policy authority

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/adapter/store/decision_protocol_paths.go`
- Create: `internal/adapter/store/decision_protocol_ledger.go`
- Create: `internal/adapter/store/decision_protocol_policy.go`
- Create: `internal/adapter/store/decision_protocol_ledger_test.go`
- Create: `internal/adapter/store/decision_protocol_policy_test.go`
- Create: `internal/adapter/store/decision_protocol_fault_test.go`
- Modify: `internal/adapter/store/repository.go` to add `decisionProtocolMu` and initialize the decision-protocol directories.
- Create: `internal/app/decisionprotocol/ports.go`
- Create: `internal/app/decisionprotocol/policy.go`
- Create focused app tests.

**Interfaces:**
- Canonical ledger sequence is store-owned and monotonic under `decisionProtocolMu`.
- **All 17 frozen canonical record kinds use the same ledger authority.** `DecisionPolicySnapshot` and `DecisionPolicyActivation` are not a parallel policy truth store.
- `policies/`, `activations/`, and `effective/` paths are secondary indexes/materializations only. They may accelerate lookup but are rebuildable from canonical policy records and never outrank the ledger.
- Event journal `observation.ChangeSeq` is never used as a `DecisionProjectionCutRef` high-water.
- Policy activation mirrors existing verification CAS semantics but is a separate store namespace/domain and uses exactly `explicit_caller`.
- New governed episode creation later consumes `CurrentEffectivePolicy(ctx, repositoryID, episodeKind)`; callers cannot pass a historical activation as authority.

Store/app interfaces are locked as:

```go
type CanonicalLedgerStore interface {
    AppendRecord(context.Context, decisionprotocol.RecordKind, any) (decisionprotocol.CanonicalRecordEnvelope, error)
    LoadRecord(context.Context, decisionprotocol.RecordSeq) (decisionprotocol.CanonicalRecordEnvelope, bool, error)
    ListEpisodeRecords(context.Context, decisionprotocol.EpisodeID, decisionprotocol.RecordSeq) ([]decisionprotocol.CanonicalRecordEnvelope, error)
    CurrentHighWater(context.Context) (decisionprotocol.RecordSeq, error)
}

type PolicyStore interface {
    PutPolicySnapshot(context.Context, decisionprotocol.PolicySnapshot) (bool, error)
    LoadPolicySnapshot(context.Context, string, string) (decisionprotocol.PolicySnapshot, bool, error)
    ActivatePolicyCAS(context.Context, decisionprotocol.PolicyActivationCommit) (decisionprotocol.PolicyActivationWriteResult, error)
    CurrentEffectivePolicy(context.Context, string, decisionprotocol.EpisodeKind) (decisionprotocol.PolicySnapshot, decisionprotocol.PolicyActivation, bool, error)
}

type PutPolicySnapshotRequest struct {
    RepositoryID string
    Content      decisionprotocol.PolicyContent
}

type ActivatePolicyRequest struct {
    RepositoryID                 string
    ActivationID                 string
    PolicyDigest                 string
    ProposalGeneration           string // exact gen_<64 lowercase hex>
    ExpectedPreviousPolicyDigest string // REQUIRED: "absent" or exact pol_<64 lowercase hex>
    ActorRef                     string
}

func (s *Service) PutPolicySnapshot(context.Context, PutPolicySnapshotRequest) (decisionprotocol.PolicySnapshot, error)
func (s *Service) ActivatePolicy(context.Context, ActivatePolicyRequest) (decisionprotocol.PolicyActivation, error)
```

`PutPolicySnapshotRequest.RepositoryID` is server-resolved by Task 11. The service validates `PolicyContent`, computes canonical `policy_digest`, constructs `DecisionPolicySnapshot{repository_id, policy_digest, content}`, and only then asks the store to append/replay the canonical snapshot plus secondary materialization. Caller input therefore cannot author `repository_id` or `policy_digest` inside a nested canonical snapshot.

`ActivatePolicyRequest.RepositoryID` is server-resolved by Task 11. `ProposalGeneration` MUST mirror the existing verification activation identity exactly: `gen_` followed by 64 lowercase hexadecimal characters. `ExpectedPreviousPolicyDigest` is REQUIRED on every activation and MUST be either the explicit sentinel `"absent"` for first activation or exact `pol_<64 lowercase hex>` for replacement. Omission/empty string has no activation semantics. `activation_generation` and `activated_at` are server/store assigned.

- [ ] **Step 1: Write RED tests for monotonic ledger order, replay cuts, canonical policy participation, immutable snapshots, and activation CAS**

Minimum tests:

```go
func TestDecisionProtocolLedgerAssignsMonotonicCanonicalRecordSeq(t *testing.T)
func TestDecisionProtocolCutListsOnlyEpisodeRecordsAtOrBelowHighWater(t *testing.T)
func TestDecisionProtocolLedgerDoesNotUseObservationChangeSeq(t *testing.T)
func TestDecisionPolicySnapshotCreatesCanonicalLedgerRecordAndSecondaryIndex(t *testing.T)
func TestDecisionPolicyActivationCreatesCanonicalLedgerRecordAndSecondaryIndexes(t *testing.T)
func TestDecisionPolicySnapshotConflictingDigestBodyFails(t *testing.T)
func TestDecisionPolicyActivationFirstAndReplacementUseExplicitCallerCAS(t *testing.T)
func TestDecisionPolicyActivationRequiresGenDigestAndExplicitPreviousDigestSentinel(t *testing.T)
func TestPutDecisionPolicySnapshotDerivesRepositoryAndDigestFromContent(t *testing.T)
func TestCurrentEffectivePolicyFiltersByEpisodeKind(t *testing.T)
```

Run:

```bash
go test ./internal/adapter/store ./internal/app/decisionprotocol -run 'DecisionProtocol|DecisionPolicy' -count=1
```

Expected: RED.

- [ ] **Step 2: Implement the durable canonical ledger and secondary-index layout**

Use this private layout:

```text
<state>/decision_protocol/
  ledger/
    high_water.json
    records/<20-digit-seq>.json
  policies/<repository-id>/<policy-digest>.json             # secondary materialization
  activations/<repository-id>/<activation-id>.json          # secondary materialization
  effective/<repository-id>/<episode-kind>.json             # secondary current index
  indexes/episodes/<episode-id>/<20-digit-seq>.json          # secondary canonical lookup index
```

All mutation paths share a private `appendCanonicalRecordLocked` helper under `decisionProtocolMu`. `PutPolicySnapshot` must append/replay `RecordPolicySnapshot` through that helper before/materialized together with `policies/...`; `ActivatePolicyCAS` must append/replay `RecordPolicyActivation` through that helper and update `activations/...` + `effective/...`. App code must never implement policy truth by writing only the secondary paths.

Canonical sequence allocation is `current_repaired_high_water + 1`; sequence paths are never reused. The record body/envelope is durable before any high-water advance. Required secondary indexes/materializations are created or repaired from the canonical record before high-water advances. `PutPolicySnapshot`/`ActivatePolicyCAS` return success only after the canonical record, required secondary materializations, and advanced high-water are all durable/cross-validated; a lost response replays the same canonical intent rather than creating a second record.

- [ ] **Step 3: Implement one exact startup/reopen recovery policy for ledger split points**

Recovery under `decisionProtocolMu` is deterministic and must implement these exact cases:

```text
record N durable / required secondary index absent / high-water=N-1
→ validate record N, rebuild all required indexes/materializations from record N, then advance high-water to N

record N + required secondary indexes durable / high-water=N-1
→ validate record/index agreement, then advance high-water to N

high-water=N / record N missing or corrupt
→ store-internal canonical-ledger corruption error; do not repair forward and do not admit another append

high-water=N / contiguous record N+1 absent / no higher record path
→ next append allocates N+1

high-water=N / record N+1 absent but any record path >N+1 exists
→ store-internal canonical-ledger corruption error; do not reuse the gap

high-water=N / valid contiguous record N+1 exists after crash
→ recover N+1 as above, advance, and continue scanning contiguous records until stable
```

A policy snapshot/activation canonical record is subject to exactly the same recovery. If a policy index disagrees with canonical body, canonical truth wins only by **deterministic rebuild**; ambiguous/conflicting canonical records are corruption, not a “choose newest file” heuristic. The next append sequence is determined only after this recovery completes, so crash recovery cannot collide with or reuse an already durable sequence.

- [ ] **Step 4: Implement immutable policy snapshots and explicit-caller activation CAS over canonical truth**

Mirror the proven verification pattern: immutable snapshot content digest, activation intent fingerprint, current-effective index, exact replay of the same activation intent, conflict on changed intent, and current-effective lookup by episode kind. Validation must reject any activation authority other than:

```go
const AuthorityExplicitCaller = "explicit_caller"
```

Do not accept `DecisionAuthorityAttestation` here. `CurrentEffectivePolicy` must be reconstructable from canonical `RecordPolicySnapshot`/`RecordPolicyActivation` even if all `policies/`, `activations/`, and `effective/` materializations are deleted.

- [ ] **Step 5: Add fault tests for every canonical/index/high-water split point**

Inject failures through the existing `atomicWriter` checkpoints. Required assertions cover every recovery case from Step 3, plus:

```text
policy snapshot canonical record durable + policy materialization absent -> rebuild from ledger
policy activation canonical record durable + activation/effective indexes absent -> rebuild exact effective state from ledger
secondary policy files present without a corresponding canonical record -> store-internal corruption error; never promoted to truth or silently ignored
```

Do not introduce a second transaction framework.

- [ ] **Step 6: Run the task gate and commit**

```bash
go test ./internal/adapter/store ./internal/app/decisionprotocol -run 'DecisionProtocol|DecisionPolicy' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/adapter/store internal/app/decisionprotocol
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: persist decision protocol ledger and policy"
```

---

### Task 3: Implement governed episode creation, candidate revision CAS, and base projection

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/app/decisionprotocol/episode.go`
- Create: `internal/app/decisionprotocol/candidate.go`
- Create: `internal/app/decisionprotocol/projection.go`
- Create corresponding focused tests.
- Create: `internal/adapter/store/decision_protocol_episode.go`
- Create: `internal/adapter/store/decision_protocol_episode_test.go`

**Interfaces:**
- `decision.create` server-resolves the current effective applicable policy and binds exact source generation; expected activation/policy fields are CAS guards only.
- Candidate revision is replacement, not branching: exactly one concurrent child may revise an ACTIVE parent.
- Base projection is replayable from canonical records and current source generation; it must not depend on event journal ordering.

Application signatures:

```go
type CreateEpisodeRequest struct {
    EpisodeID             decisionprotocol.EpisodeID
    Kind                  decisionprotocol.EpisodeKind
    RepositoryID          string
    WorkspaceID           string
    PredecessorEpisodeID  decisionprotocol.EpisodeID
    ExpectedPolicyDigest  string
    ExpectedActivationRef string
    ActorRef              string
}

func (s *Service) CreateEpisode(context.Context, CreateEpisodeRequest) (decisionprotocol.DecisionProjection, error)
func (s *Service) CreateCandidate(context.Context, decisionprotocol.Candidate) (decisionprotocol.DecisionProjection, error)
func (s *Service) ReviseCandidate(context.Context, parent decisionprotocol.CandidateID, child decisionprotocol.Candidate) (decisionprotocol.DecisionProjection, error)
func (s *Service) Inspect(context.Context, decisionprotocol.EpisodeID, decisionprotocol.CandidateID) (decisionprotocol.DecisionProjection, error)
```

Store CAS extension:

```go
type EpisodeMutationStore interface {
    CreateEpisode(context.Context, decisionprotocol.Episode) (decisionprotocol.CanonicalRecordEnvelope, bool, error)
    CreateCandidate(context.Context, decisionprotocol.Candidate) (decisionprotocol.CanonicalRecordEnvelope, bool, error)
    ReviseCandidateCAS(context.Context, decisionprotocol.CandidateID, decisionprotocol.Candidate) (decisionprotocol.CanonicalRecordEnvelope, error)
    FindEpisode(context.Context, decisionprotocol.EpisodeID) (decisionprotocol.Episode, bool, error)
    FindCandidate(context.Context, decisionprotocol.CandidateID) (decisionprotocol.Candidate, bool, error)
}
```

- [ ] **Step 1: Write RED tests for effective-policy binding and policy/source conflicts**

Required tests:

```go
func TestCreateEpisodeBindsServerResolvedCurrentEffectivePolicy(t *testing.T)
func TestCreateEpisodeHistoricalActivationGuardCannotSelectWeakerPolicy(t *testing.T)
func TestCreateEpisodeExpectedPolicyMismatchReturnsPolicyConflict(t *testing.T)
func TestCreateEpisodeCapturesCanonicalSourceGenerationOnly(t *testing.T)
```

The historical-activation test gives the service a valid older activation ID in `ExpectedActivationRef` while the store reports a newer current effective activation; result must be `POLICY_CONFLICT`, never an episode bound to the historical activation.

- [ ] **Step 2: Implement episode creation and lifecycle projection**

Build `OPEN|COMMITTED|CLOSED_UNRESOLVED` only from `Episode`, `SelectionCommit`, and `Closure` canonical record existence. `Inspect` loads canonical episode records at current high-water and returns structural corruption if both terminal kinds exist.

Source-generation compatibility is a distinct projection field:

```go
type SourceGenerationCompatibility string
const (
    SourceGenerationCurrent SourceGenerationCompatibility = "current"
    SourceGenerationStale   SourceGenerationCompatibility = "stale"
)
```

Audit/event counters must not affect this field.

- [ ] **Step 3: Write RED race tests for candidate replacement semantics**

Use two goroutines against the same store instance:

```go
func TestReviseCandidateCASAllowsExactlyOneConcurrentReplacement(t *testing.T)
func TestCandidateRevisionDoesNotInheritPredictions(t *testing.T)
func TestSiblingAlternativeRequiresCreateNotRevise(t *testing.T)
```

Expected concurrent result: one child succeeds; the other receives the typed `CANDIDATE_REVISION_CONFLICT` reason.

- [ ] **Step 4: Implement lineage roots and active/superseded projection**

Use immutable `revises_candidate_id` edges. Reject cycles, missing parents, cross-episode revisions, and revising a non-ACTIVE parent. Compute lineage root deterministically by walking parent edges with cycle detection.

- [ ] **Step 5: Run the task gate and commit**

```bash
go test ./internal/core/decisionprotocol ./internal/app/decisionprotocol ./internal/adapter/store -run 'DecisionEpisode|Candidate|ReviseCandidate' -count=1
go test -race ./internal/adapter/store -run 'ReviseCandidate' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/app/decisionprotocol internal/adapter/store
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: add decision episodes and candidate lifecycle"
```

---

### Task 4: Implement experiment definition, preregistration, sealing, and authoritative replay cuts

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/app/decisionprotocol/experiment.go`
- Create: `internal/app/decisionprotocol/experiment_test.go`
- Create: `internal/adapter/store/decision_protocol_experiment.go`
- Create: `internal/adapter/store/decision_protocol_experiment_test.go`
- Extend: `internal/app/decisionprotocol/projection.go`

**Interfaces:**
- `DefineExperiment`, `BindPrediction`, and `SealExperiment` append facts only; none may start commands.
- `PredictionBinding` can be appended only while experiment is DEFINED.
- Seal atomically freezes prediction digest, canonical-ledger base cut, and potential discrimination pairs.
- Potential discrimination eligibility is evaluated at seal time against then-active, non-blocked, same-source lineages and remains historical after later elimination.

Locked signatures:

```go
func (s *Service) DefineExperiment(context.Context, decisionprotocol.Experiment) (decisionprotocol.DecisionProjection, error)
func (s *Service) BindPrediction(context.Context, decisionprotocol.PredictionBinding) (decisionprotocol.DecisionProjection, error)
func (s *Service) SealExperiment(context.Context, decisionprotocol.ExperimentID, string) (decisionprotocol.ExperimentSeal, decisionprotocol.DecisionProjection, error)
func (s *Service) CloseExperiment(context.Context, decisionprotocol.ExperimentID, string) (decisionprotocol.DecisionProjection, error)
func (s *Service) AbortExperiment(context.Context, decisionprotocol.ExperimentID, decisionprotocol.AbortPhase, string, string) (decisionprotocol.DecisionProjection, error)
```

`SealExperiment` actor string is audit identity, not authority.

- [ ] **Step 1: Write RED tests for preregistration and sealed immutability**

Required cases:

```go
func TestPredictionBindAfterSealReturnsExperimentAlreadySealed(t *testing.T)
func TestSealRejectsPredictionFromOtherEpisodeOrSourceGeneration(t *testing.T)
func TestSealFreezesPredictionDigestAndCanonicalLedgerHighWater(t *testing.T)
func TestExperimentActionsDoNotCallExecutionStart(t *testing.T)
```

The no-scheduling test injects a panic/counting fake execution starter and asserts zero calls for define/bind/seal/close/abort.

- [ ] **Step 2: Implement exact closed prediction validation**

Validation accepts only these role/predicate combinations:

```text
roles: REQUIRED_PREDICTION | DISCRIMINATOR | OBSERVATION_TARGET
predicate kinds: OPERATION_OUTCOME | STRUCTURED_TEST_STATUS | STRUCTURED_DIAGNOSTIC_PRESENCE | VERIFICATION_RESULT
```

Structured test selectors are exact package/name; diagnostics require exact code and optional exact severity; no regex/substrings/location matcher. Observation dimension keys exclude expected value.

- [ ] **Step 3: Write RED tests for seal-time potential discrimination**

Required scenarios:

```text
A PASS / B FAIL on same TestRace dimension -> qualifying pair
A PASS / B PASS -> no pair
A TestRace PASS / B TestOther FAIL -> no pair
B superseded before seal -> no pair
B already REQUIRED-mismatch-blocked before seal -> no pair
B becomes blocked after seal -> frozen pair remains in E1
```

- [ ] **Step 4: Implement `ExperimentSeal` as one CAS append**

Algorithm:

```go
cut, err := store.CurrentHighWater(ctx)
base, err := s.projectAtCut(ctx, episodeID, cut)
predictions, err := s.predictionsForOpenExperiment(ctx, experimentID)
pairs := PotentialDiscriminationPairs(base, predictions)
seal := decisionprotocol.ExperimentSeal{
    ExperimentID: experimentID,
    SourceGeneration: episode.Baseline.SourceGeneration,
    SealedPredictionDigest: digestPredictions(predictions),
    BaseProjectionCutRef: decisionprotocol.DecisionProjectionCutRef{EpisodeID: episodeID, CanonicalRecordHighWater: cut},
    BaseCandidateProjectionDigest: base.ProjectionDigest,
    PotentialDiscriminationPairs: pairs,
    SealedAt: s.clock.Now(),
}
```

Store must reject a second different seal and replay an identical seal request without mutating history.

- [ ] **Step 5: Implement projected experiment lifecycle without mutable state fields**

Projection rules must exactly follow frozen record existence:

```text
DEFINED   = experiment and no seal/abort
SEALED    = seal and no link/close/abort
OBSERVING = seal + link and no close/abort
CLOSED    = closure
ABORTED   = abort
```

Close/abort mutual exclusion is enforced by store CAS, not only app checks.

- [ ] **Step 6: Run the task gate and commit**

```bash
go test ./internal/core/decisionprotocol ./internal/app/decisionprotocol ./internal/adapter/store -run 'Experiment|Prediction|Discrimination|Seal' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/app/decisionprotocol internal/adapter/store
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: preregister decision protocol experiments"
```

---

### Task 5: Bind experiments into immutable operation admission and replay identity

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Modify: `internal/core/operation/intent.go`
- Modify: `internal/core/operation/persistence.go`
- Create: `internal/core/operation/decision_protocol_binding_test.go`
- Modify: `internal/app/daemon/types.go`
- Modify: `internal/app/daemon/admission.go`
- Modify: `internal/app/daemon/project_command.go`
- Modify: `internal/app/daemon/structured_adapter.go`
- Modify: `internal/app/daemon/store_port.go`
- Create: `internal/app/daemon/decision_protocol_admission_test.go`
- Modify: `internal/adapter/store/admission.go`
- Create: `internal/adapter/store/decision_protocol_admission_test.go`
- Create: `internal/adapter/store/decision_protocol_admission_fault_test.go`
- Modify: `internal/app/daemon/structured_pytest_admission_test.go`
- Modify operation persistence schemas under `api/schema/operation-v2.json` and `api/schema/operation-v3.json` only where the current persisted reservation schema requires the new optional field; keep legacy v1 unchanged.

**Interfaces:**
- Add caller field `ExperimentID string \`json:"experiment_id,omitempty"\`` to `daemon.StartRequest`, transport later, `operation.Intent`, and durable `operation.Reservation`.
- Do **not** add experiment identity to request/execution meaning if it would conflate execution semantics; bind it into the existing observation-binding fingerprint using the same deterministic fingerprint path used for structured/evidence/verification observation metadata.
- Existing operation replay validates stored immutable observation binding before consulting live experiment metadata.
- Store adds one recovery-indivisible reservation method for experiment-linked starts; process spawn is impossible until it succeeds.

New narrow daemon-store interface:

```go
type DecisionExperimentAdmissionStore interface {
    ResolveExperimentAdmissionSession(context.Context, decisionprotocol.ExperimentID, operation.ID) (operation.SessionID, bool, error)
    ReserveExperimentOperation(context.Context, operation.Reservation, decisionprotocol.ExperimentExecutionLink) (operation.Reservation, decisionprotocol.ExperimentExecutionLink, bool, StoreResult)
}
```

- [ ] **Step 1: Write RED fingerprint matrix tests**

Test exact frozen matrix for the same operation intent/ID:

```go
func TestObservationBindingFingerprintIncludesExperimentIdentity(t *testing.T) {
    // omitted/E1, E1/E2, E1/omitted must differ; E1/E1 must match.
}
```

Also assert an ordinary request with omitted experiment keeps the accepted upstream observation fingerprint corpus unchanged. Add the compatibility matrix `experiment + verification`, `experiment + structured capture`, and `experiment + verification + structured capture`; all recomputation paths must produce the same deterministic composed identity. The correct omitted-experiment result is zero upstream corpus churn.

- [ ] **Step 2: Extend start intent/reservation validation without changing ordinary operations**

`ExperimentID` is optional. When absent, `Start` follows the exact old path. When present:

```text
workspace binding required
persistent session start rejected in V1 unless existing semantics prove one-shot observation execution
experiment must exist, be SEALED, source generation current, and have no existing execution link
```

Do not auto-create, auto-seal, or infer an experiment.

- [ ] **Step 3: Write RED replay tests before implementation**

Required daemon/store tests:

```go
func TestStartReplayOmittedThenExperimentConflictsBeforeLiveExperimentLookup(t *testing.T)
func TestStartReplayExperimentThenDifferentExperimentConflicts(t *testing.T)
func TestStartReplayExperimentThenOmittedConflicts(t *testing.T)
func TestStartReplaySameExperimentReturnsOriginalAdmission(t *testing.T)
func TestProjectCommandReplayUsesSameExperimentBindingRules(t *testing.T)
```

Each test must prove `owner.Start` call count remains zero for replay conflicts.

- [ ] **Step 4: Implement recovery-indivisible reservation + execution link before spawn with one lock order**

Refactor the existing reservation body into an unexported helper that assumes the current reservation locks are already held; ordinary `ReserveOperation` still acquires the same locks then calls the helper. `ReserveExperimentOperation` uses the only permitted combined lock order:

```text
1. per-operation r.lock(operation_id)
2. r.admit
3. r.decisionProtocolMu
```

No Decision Protocol path may acquire the per-operation/admit locks while already holding `decisionProtocolMu`. Add a lock-order comment beside the new method and a race test that concurrently performs ordinary reservation, experiment admission, and decision inspection without deadlock.

Before structured-capture preparation for an experiment-bound start, resolve a stable pre-admission `SessionID` through `ResolveExperimentAdmissionSession`. The read-only resolver checks the existing Decision Protocol admission claim first, then an existing structured capture authority for the same `operation_id`; if both exist they must agree. If neither exists, retain the normal fresh session ID. This closes both claim-only and capture-authority-only crash windows without adding a new durable record. After capture preparation, compute the final observation fingerprint using `legacy -> experiment -> verification attempt -> structured capture` and only then call `ReserveExperimentOperation`.

Before creating an operation reservation, create an **internal non-canonical recovery claim** exclusively at:

```text
<state>/decision_protocol/indexes/experiment_admission_claims/<experiment-id>.json
```

with this private shape:

```go
type experimentAdmissionClaim struct {
    SchemaVersion                  int       `json:"schema_version"`
    LinkID                         string    `json:"link_id"`
    ExperimentID                   string    `json:"experiment_id"`
    OperationID                    string    `json:"operation_id"`
    SessionID                      string    `json:"session_id"`
    WorkspaceID                    string    `json:"workspace_id"`
    SourceGeneration               string    `json:"source_generation"`
    AcceptedRequestFingerprint     string    `json:"accepted_request_fingerprint"`
    AcceptedExecutionFingerprint   string    `json:"accepted_execution_fingerprint"`
    AcceptedObservationFingerprint string    `json:"accepted_observation_binding_fingerprint"`
    AdmittedAt                     time.Time `json:"admitted_at"`
    LinkSemanticFingerprint        string    `json:"link_semantic_fingerprint"`
}
```

The claim is a private recovery/index fact, **not an 18th canonical record**. It reserves the experiment's single V1 execution slot across process restart. Before the claim is first written, allocate/freeze `LinkID`, server-resolved `WorkspaceID`, and one UTC `AdmittedAt` from the injected store clock; those values are then replay identity, not regenerated during recovery. Together with the existing IDs/fingerprints, the claim contains every non-derivable field required to reconstruct the exact frozen `ExperimentExecutionLink`. `LinkSemanticFingerprint` covers the complete canonical link semantic body but is never treated as a reversible source. Same experiment + same semantic claim replays; any different operation/fingerprint/link-body claim returns `EXPERIMENT_EXECUTION_LIMIT_REACHED`/metadata conflict before new reservation or spawn.

New-operation sequence under the three locks:

```text
validate experiment SEALED/current and no canonical link
create/replay exact experimentAdmissionClaim
create/replay durable operation reservation + session metadata via shared locked helper
append/replay the canonical ExperimentExecutionLink matching the claim
return successful admission only after link is durable and cross-validated
```

Existing-operation replay checks request/observation fingerprints **before** consulting current experiment metadata. If the exact private claim + operation reservation exist but the canonical link is missing, retry reconstructs the exact link from `LinkID`, `ExperimentID`, `OperationID`, `SessionID`, `WorkspaceID`, `SourceGeneration`, all three accepted fingerprints, and `AdmittedAt` frozen in the claim; it may not regenerate identity/time or reinterpret the operation from live caller fields. If reconstruction cannot become durable, return failure and do not spawn.

If reservation/session metadata became durable but the claim/link cannot be made consistent, use the existing pre-spawn admission-failure compensation pattern to terminalize the reserved session as abandoned/ambiguous. **Do not release/reassign the durable experiment claim to another operation in V1.** Once the first claim is durable, that experiment is pinned to that operation identity for retry/recovery; if the exact admission cannot be completed, the caller may abort E1 and create E2. This keeps `one experiment = one attempt` and removes a delete/reassignment race from the recovery index. Never leave an active capacity slot plus a silently reusable experiment.

- [ ] **Step 5: Add fault injection tests for every claim/reservation/link split point**

Required outcomes:

```text
claim write fails before durability -> no reservation/link/spawn; same request may retry
claim durable, reservation absent -> same request reuses the claim session before structured capture preparation; different operation for E1 conflicts
structured capture authority durable, claim/reservation absent -> same operation reuses the capture-authority session before Decision claim creation
claim + structured capture authority session disagreement -> fail closed before new reservation/link/spawn
reservation/session durable, link write interrupted -> no spawn; same retry repairs exact link from claim or terminally compensates reservation; claim remains pinned to that operation
canonical link durable, response/finalization interrupted -> no second link; same retry returns same semantic admission
private claim and canonical link disagree -> corrupt/recovery error; never choose one opportunistically
```

Assert `count(ExperimentExecutionLink) <= 1`, no double session capacity, and `owner.Start == 0` until the durable link boundary is complete.

- [ ] **Step 6: Add API persistence schema tests for experiment binding**

Update existing strict schema fixtures so modern reservation JSON accepts optional `experiment_id` and rejects wrong type/unknown placement. Legacy operation without the field must round-trip byte/semantic compatibility as required by current tests.

- [ ] **Step 7: Run race/recovery/task gates and commit**

```bash
go test ./internal/core/operation ./internal/app/daemon ./internal/adapter/store ./api/schema -run 'DecisionProtocol|Experiment.*Admission|ObservationBindingFingerprint|ProjectCommandReplay' -count=1
go test -race ./internal/app/daemon ./internal/adapter/store -run 'Experiment.*Admission|Replay' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/core/operation internal/app/daemon internal/adapter/store api/schema
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: bind decision experiments at execution admission"
```

---

### Task 6: Materialize complete typed observations and close/abort experiments safely

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/app/decisionprotocol/observation.go`
- Create: `internal/app/decisionprotocol/predicates.go`
- Create: `internal/app/decisionprotocol/observation_test.go`
- Create: `internal/app/decisionprotocol/predicates_test.go`
- Extend: `internal/adapter/store/decision_protocol_experiment.go`
- Create: `internal/adapter/store/decision_protocol_observation_test.go`
- Create: `internal/adapter/store/decision_protocol_observation_fault_test.go`

**Interfaces:**
- Observation materialization is server-owned and has at most one canonical binding per experiment.
- `CloseExperiment` does not execute; it requires linked operation terminal plus relevant derivations at a terminal cut, materializes/replays the unique binding, then atomically appends closure.
- `AbortExperiment` before link needs no observation. Abort after link may append abort immediately, but selection cannot commit until the linked observation domain has a terminal canonical binding.
- `VERIFICATION_RESULT` reads only complete qualified facts attributable to the linked operation and frozen derivation cut.

Narrow read ports:

```go
type ReceiptSource interface {
    FindReceiptByOperation(context.Context, operation.ID) (receipt.Receipt, bool, error)
}
type StructuredSource interface {
    InspectStructured(context.Context, structuredapp.InspectRequest) (structuredapp.InspectResult, error)
}
type VerificationObservationCut struct {
    EvidenceIndexGeneration uint64
}

type QualifiedEvidenceSet struct {
    Cut        VerificationObservationCut
    Candidates []verification.EvidenceCandidate
    Coverage   verification.Coverage
}

type VerificationSource interface {
    AcquireVerificationObservationCut(context.Context, operation.ID) (VerificationObservationCut, error)
    QualifiedEvidenceForOperation(context.Context, operation.ID, VerificationObservationCut) (QualifiedEvidenceSet, error)
}
```

`VerificationObservationCut.EvidenceIndexGeneration` is the evidence-inspection observation-index high-water (`index_generation`) frozen for this linked operation's verification read. It is **not** a `DecisionProjectionCutRef`, canonical Decision Protocol record sequence, generic event-journal replay authority, or structured-result `DerivationKey`. Observation materialization acquires this cut exactly once after the linked operation is terminal and relevant provider/structured settlement checks have reached the materialization point; the same cut value is then passed to the complete evidence scan and retained in the derivation basis. Do not re-acquire a later cut halfway through one binding. `QualifiedEvidenceForOperation` must enumerate the complete operation-attributable set at or below that exact cut and return explicit coverage; bounded/expired/incomplete coverage makes `VERIFICATION_RESULT` indeterminate. Structured-result derivation identity/version/completeness is frozen separately and participates independently in `derivation_cut_digest`.

Store materialization primitive:

```go
func (r *Repository) MaterializeExperimentObservationCAS(
    context.Context,
    decisionprotocol.ExperimentObservationBinding,
) (decisionprotocol.ExperimentObservationBinding, bool, error)
```

- [ ] **Step 1: Write RED predicate tests for all four closed fact families**

Minimum matrix:

```text
OPERATION_OUTCOME: terminal success/failure/timeout/killed -> MATCH/MISMATCH; nonterminal -> NOT_EVALUATED; unknown terminal -> INDETERMINATE
STRUCTURED_TEST_STATUS: exact identity one record -> MATCH/MISMATCH; absent -> NOT_EVALUATED; ambiguous cardinality -> INDETERMINATE
STRUCTURED_DIAGNOSTIC_PRESENCE: complete zero-match may prove ABSENT; partial/malformed/unavailable zero-match -> INDETERMINATE
VERIFICATION_RESULT: complete compatible qualified set -> determinate; advisory/stale/unqualified -> INDETERMINATE; conflicting qualifying set -> INDETERMINATE
```

No test may pass a generic comparator/expression into the evaluator because such a type must not exist.

- [ ] **Step 2: Implement frozen derivation-cut identity and complete observation evaluation**

Compute:

```text
derivation_cut_digest = hash(
  receipt identity/result
  + relevant structured derivation key/version/config/completeness
  + qualified attributable verification evidence identities/results
  + observation semantics version
)
```

For every sealed prediction, emit exactly one `PredictionResult`. Basis refs are server-derived audit pointers. Do not accept caller-supplied result/basis arrays.

- [ ] **Step 3: Write RED uniqueness and concurrency tests**

Required tests:

```go
func TestObservationBindingCountNeverExceedsOne(t *testing.T)
func TestObservationMaterializationSameSemanticCutReplaysSameBinding(t *testing.T)
func TestObservationMaterializationDifferentCutReturnsConflict(t *testing.T)
func TestCloseAndPostAbortSettlementShareSameBindingCAS(t *testing.T)
```

Race test two materializers with identical cut; exactly one physical binding is created and both callers observe the same `binding_id`.

- [ ] **Step 4: Implement close and abort settlement semantics**

`CloseExperiment` sequence:

```text
require linked operation terminal
require structured/verification sources at terminal evaluable cut
materialize/replay unique ObservationBinding
append ExperimentClosure naming that binding via close/abort mutual-exclusion CAS
```

`AbortExperiment`:

```text
BEFORE_EXECUTION -> append abort; no observation domain
AFTER_EXECUTION_LINK -> append abort naming link; do not delete facts; settlement may occur now or later through the same observation CAS
```

A post-link aborted experiment with unsettled providers projects `observation_state=SETTLING` and blocks selection terminalization without inventing a protocol requirement result.

- [ ] **Step 5: Implement experiment-local realized discrimination from the seal-time replay cut**

Load the authoritative `BaseProjectionCutRef`, reconstruct candidate state at that high-water, apply only this experiment's terminal `ExperimentObservationBinding`, and compute pair partition changes excluding the DISCRIMINATION requirement's own status and aggregate protocol gate. Never compare against live episode state containing interleaved E2/E3 effects.

Add test:

```go
func TestRealizedDiscriminationDoesNotCreditInterleavedExperiment(t *testing.T)
```

where E2 blocks challenger B before E1 settles; E1 realized result must be computed solely from E1 against its sealed cut.

- [ ] **Step 6: Add anti-cherry-pick verification-domain tests**

Construct linked operation evidence containing one qualifying PASS and one qualifying FAIL for the same required verification selector. Assert `INDETERMINATE`, not first-match PASS. Add unrelated historical workspace PASS and prove it is excluded because it is not attributable to the linked operation/frozen cut.

- [ ] **Step 7: Run task/race gates and commit**

```bash
go test ./internal/app/decisionprotocol ./internal/adapter/store -run 'Observation|Predicate|CloseExperiment|AbortExperiment|RealizedDiscrimination' -count=1
go test -race ./internal/adapter/store -run 'ObservationMaterialization' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/app/decisionprotocol internal/adapter/store
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: derive decision experiment observations"
```

---

### Task 7: Implement candidate-scoped policy evaluation, budget admission, and semantic projections

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/app/decisionprotocol/evaluate.go`
- Extend: `internal/app/decisionprotocol/projection.go`
- Create: `internal/app/decisionprotocol/evaluate_test.go`
- Create: `internal/app/decisionprotocol/projection_test.go`
- Extend core projection/digest tests as needed.

**Interfaces:**
- Evaluation is always `evaluate(episode_id, candidate_id)`; no episode-global inferred selected candidate.
- Gate fold is deterministic `BLOCKED > INDETERMINATE > CLEAR`.
- Candidate eligibility, protocol gate, commit-attempt preconditions, and budget admission remain distinct fields/types.
- `DecisionProjectionDigest` includes normalized verifier semantic state and excludes literal replaceable audit refs.
- Projection exposes `allowed_protocol_transitions[]` as capability, never recommended action.

Locked evaluator entry points:

```go
func (s *Service) Evaluate(context.Context, decisionprotocol.EpisodeID, decisionprotocol.CandidateID) (decisionprotocol.DecisionProtocolEvaluation, error)
func (s *Service) Project(context.Context, decisionprotocol.EpisodeID, decisionprotocol.CandidateID) (decisionprotocol.DecisionProjection, error)
func EvaluateRequirements(decisionprotocol.PolicyContent, decisionprotocol.EvaluationFacts) decisionprotocol.DecisionProtocolEvaluation
func EvaluateBudget(decisionprotocol.DecisionBudget, decisionprotocol.BudgetUsage) decisionprotocol.BudgetAdmission
```

- [ ] **Step 1: Write RED tests for the four bounded requirement kinds**

Required exact outcomes:

```text
CANDIDATE_CHALLENGE: lineage roots 2/2 -> SATISFIED; one lineage -> UNSATISFIED; revisions do not increase count
PREDICTION_EVALUATION: only sealed + linked + determinate MATCH/MISMATCH counts; NOT_EVALUATED/INDETERMINATE do not
DISCRIMINATION ATTEMPTED: seal-qualified + linked + terminal binding + CLOSED -> SATISFIED; ABORTED -> not satisfied
DISCRIMINATION REALIZED: uses experiment-local realized result; unavailable cut -> INDETERMINATE; complete nonpartitioning result -> UNSATISFIED
VERIFIER_ASSESSMENT: initially zero records -> UNSATISFIED; Task 8 adds qualified-context cases
```

- [ ] **Step 2: Implement implicit REQUIRED mismatch blockers separately from policy requirements**

A candidate gets one normalized blocker per determinately mismatched `REQUIRED_PREDICTION`:

```go
type CandidateContractBlocker struct {
    Code         ReasonCode
    PredictionID PredictionID
}
```

Do not synthesize blockers from `NOT_EVALUATED` or `INDETERMINATE`. Do not add a policy flag allowing mismatch to be ignored.

- [ ] **Step 3: Implement deterministic gate and blocker digest**

```go
func FoldGate(reqs []DecisionRequirementEvaluation, blockers []CandidateContractBlocker) DecisionProtocolGate {
    // any UNSATISFIED or blocker => BLOCKED
    // else any INDETERMINATE => INDETERMINATE
    // else CLEAR
}
```

`blocking_requirement_digest` includes normalized UNSATISFIED + INDETERMINATE requirement identities/status/reason semantics plus candidate-contract blockers. Equivalent basis record IDs do not perturb the digest.

- [ ] **Step 4: Implement the separate budget ceiling**

Track only:

```text
max_experiments_started
max_linked_operations
max_machine_wall_ms
```

Do not add token/model cost. Budget exhaustion never changes gate status. If `max_machine_wall_ms` cannot be hard-enforced for an admitted linked execution, projection labels the quality honestly as observed/not-hard rather than claiming a strict maximum.

- [ ] **Step 5: Implement semantic projection/audit digests and plateau stability**

Projection semantic state must include:

```text
episode/policy/source compatibility
candidate active/superseded + lineage + contract status + normalized expectation outcomes
experiment lifecycle/observation state + potential/realized discrimination
candidate-scoped requirement/gate state
normalized verifier semantic state
budget admission
selection semantic state
allowed protocol transitions
```

Audit digest additionally includes exact canonical record/basis IDs. Add test where three semantically identical failed experiments advance audit digest but leave projection digest equal, yielding `epistemic_progress=NONE`.

- [ ] **Step 6: Add a no-planner projection contract test**

Marshal a projection and assert it has no fields named or semantically equivalent to:

```text
next_best_action
recommended_experiment
generate_more_hypotheses
choose_candidate_B
```

Allowed transitions such as `candidate.create`, `experiment.define`, `close_unresolved` are fine because they describe protocol capability, not recommendation.

- [ ] **Step 7: Run the task gate and commit**

```bash
go test ./internal/core/decisionprotocol ./internal/app/decisionprotocol -run 'Requirement|Gate|Budget|Projection|Plateau' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/core/decisionprotocol internal/app/decisionprotocol
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: evaluate decision protocol gates"
```

---

### Task 8: Add verifier assessments with a hard provenance firewall

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/app/decisionprotocol/assessment.go`
- Create: `internal/app/decisionprotocol/assessment_test.go`
- Extend: `internal/adapter/store/decision_protocol_episode.go` for immutable assessment append/list.
- Extend focused store tests.

**Interfaces:**
- Caller input owns only `declared_context_class`, `declared_provider_identity`, preferences/rejections/rationale.
- `qualified_context_class` and `context_qualification` are server/provider-materialized at assessment admission time and are historical properties, not future-current permissions.
- `VerifierAssessment` never implements/converts to `EvidenceCandidate` and cannot enter verification sufficiency stores.

Qualification interface:

```go
type QualifyVerifierContextRequest struct {
    EpisodeID            decisionprotocol.EpisodeID
    ActorRef             string
    DeclaredContextClass decisionprotocol.ContextClass
    DeclaredProviderID   string
}

type VerifierContextQualifier interface {
    QualifyVerifierContext(context.Context, QualifyVerifierContextRequest) (decisionprotocol.ContextQualificationResult, error)
}

type RecordAssessmentRequest struct {
    AssessmentID             string
    EpisodeID                decisionprotocol.EpisodeID
    ActorRef                 string
    DeclaredContextClass     decisionprotocol.ContextClass
    DeclaredProviderIdentity string
    PreferredCandidates      []decisionprotocol.CandidateID
    SemanticRejections       []decisionprotocol.CandidateID
    Rationale                string
}
```

- [ ] **Step 1: Write RED tests proving caller declaration cannot become qualification**

Required cases:

```go
func TestCallerDeclaredHumanRemainsUnqualifiedWithoutTrustedQualifier(t *testing.T)
func TestCallerCannotPopulateQualifiedContextFields(t *testing.T)
func TestTrustedQualifierMaterializesExactContextQualification(t *testing.T)
func TestVerifierAssessmentCannotConvertToEvidenceCandidate(t *testing.T)
```

The last test should be compile-/type-structure oriented: no conversion method/interface should exist; do not merely test a runtime authority value.

- [ ] **Step 2: Implement immutable assessment admission with optional trusted qualification**

If qualifier is unavailable/unknown, persist a valid assessment with qualified fields absent. If qualifier determinately returns a class, persist exact provider/version/capability/cut/qualified time. Caller JSON must have no fields capable of setting server-only qualification values.

- [ ] **Step 3: Finish `VERIFIER_ASSESSMENT` requirement folding**

Exact fold:

```text
qualified supporting count >= minimum -> SATISFIED
below minimum + otherwise-supporting unresolved required qualification could change count -> INDETERMINATE
below minimum + no such unresolved item -> UNSATISFIED
```

Wrong qualified class is determinately non-counting. Zero assessments is UNSATISFIED. With no `required_context_class`, declaration may support candidate without claiming independence.

- [ ] **Step 4: Prove verifier semantic state participates in projection CAS**

Test:

```text
P1 = projection with one assessment already satisfying minimum
append second semantically distinct assessment while gate remains CLEAR
P2 gate = CLEAR
P2 projection_digest != P1 projection_digest
```

Equivalent replay of the same immutable assessment must not create a second semantic record or perturb digest.

- [ ] **Step 5: Run the task gate and commit**

```bash
go test ./internal/app/decisionprotocol ./internal/adapter/store -run 'Assessment|Verifier|ContextQualification|ProjectionDigest' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/app/decisionprotocol internal/adapter/store
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: record qualified decision verifier assessments"
```

---

### Task 9: Implement selection proposal, semantic CAS, durable idempotency, and close-unresolved

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/app/decisionprotocol/selection.go`
- Create: `internal/app/decisionprotocol/selection_test.go`
- Create: `internal/adapter/store/decision_protocol_selection.go`
- Create: `internal/adapter/store/decision_protocol_selection_test.go`
- Create: `internal/adapter/store/decision_protocol_selection_fault_test.go`
- Create: `internal/adapter/store/decision_protocol_selection_race_test.go`

**Interfaces:**
- `SelectionProposal` is append-only preference metadata with zero transition authority.
- Normal selection commit requires OPEN episode, active candidate, current source generation, exact policy digest, exact current semantic projection digest, all linked experiments settled to terminal observation bindings, and protocol gate CLEAR.
- `SelectionCommit` and `DecisionClosure` are mutually exclusive at one per-episode terminal serialization boundary.
- Durable selection persists `idempotency_key` plus `semantic_intent_fingerprint` transactionally with terminalization.
- Override path is structurally represented now but Task 10 supplies the authority authorizer; before Task 10, an override request returns `OVERRIDE_AUTHORITY_NOT_ADMISSIBLE`.

Locked requests:

```go
type CommitSelectionRequest struct {
    EpisodeID                decisionprotocol.EpisodeID
    CandidateID              decisionprotocol.CandidateID
    ActorRef                 string
    ExpectedPolicyDigest     string
    ExpectedProjectionDigest string
    OverrideRef              string
    IdempotencyKey           string
}

type CloseUnresolvedRequest struct {
    EpisodeID                decisionprotocol.EpisodeID
    ActorRef                 string
    ProjectionDigest         string
    Reason                   string
    UnresolvedDimensions     []string
}
```

Store terminal primitive:

```go
type EpisodeTerminalStore interface {
    CommitSelectionCAS(context.Context, decisionprotocol.SelectionCommitIntent, decisionprotocol.SelectionCommit) (decisionprotocol.SelectionCommit, bool, error)
    CloseEpisodeCAS(context.Context, decisionprotocol.Closure) (decisionprotocol.Closure, bool, error)
}
```

- [ ] **Step 1: Write RED tests separating proposal from commit authority**

```go
func TestSelectionProposalPersistsWhileProtocolBlocked(t *testing.T)
func TestSelectionProposalDoesNotChangeEpisodeState(t *testing.T)
func TestNormalCommitRequiresClearGateAndSettledLinkedExperiments(t *testing.T)
```

A post-link ABORTED experiment with `observation_state=SETTLING` must prevent commit even if all policy requirements otherwise look clear.

- [ ] **Step 2: Implement semantic projection CAS and source/policy preconditions**

Commit sequence before any terminal write:

```text
load OPEN episode
resolve current canonical source generation == baseline
recompute current candidate-scoped projection
require expected policy digest exact
require expected projection digest exact
require candidate ACTIVE
require every linked experiment terminal observation binding present
require protocol gate CLEAR for normal commit
```

Return stable reason codes `STALE_EPISODE_SOURCE_GENERATION`, `POLICY_CONFLICT`, `PROJECTION_CONFLICT`, `PROTOCOL_BLOCKED`, or `PROTOCOL_INDETERMINATE` without mutating episode state.

- [ ] **Step 3: Write RED durable idempotency tests across store reopen**

Required behavior:

```text
same key + same semantic fingerprint + existing durable commit -> replay prior success after Repository reopen
same key + different semantic fingerprint -> IDEMPOTENCY_CONFLICT
same candidate but normal vs override -> different semantic fingerprint and terminal conflict, never merged
```

- [ ] **Step 4: Implement terminalization + idempotency in one recovery boundary**

Persist commit and idempotency mapping under the per-episode terminal lock/CAS. `SelectionCommit` stores `idempotency_key` and `semantic_intent_fingerprint`; no in-memory-only replay table is authoritative. Fault tests must cover commit body/index split points and exact retry recovery.

- [ ] **Step 5: Write and implement commit-vs-close race semantics**

Race two goroutines:

```text
R1 commit B
R2 close_unresolved
```

Exactly one terminal fact succeeds. Loser gets `EPISODE_TERMINAL_CONFLICT`. Assert canonical store never contains both terminal kinds.

`close_unresolved` is always an available truthful OPEN-episode terminal path; it does not require protocol CLEAR or override authority, but it still requires terminal CAS against the episode not already being terminal.

- [ ] **Step 6: Run race/fault/task gates and commit**

```bash
go test ./internal/app/decisionprotocol ./internal/adapter/store -run 'Selection|CloseUnresolved|Idempotency|Terminal' -count=1
go test -race ./internal/adapter/store -run 'Selection|Terminal' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/app/decisionprotocol internal/adapter/store
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: commit decision selections atomically"
```

---

### Task 10: Implement trusted authority materialization, override intent, and commit-time requalification

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/app/decisionprotocol/authority.go`
- Create: `internal/app/decisionprotocol/authority_test.go`
- Create: `internal/adapter/store/decision_protocol_authority.go`
- Create: `internal/adapter/store/decision_protocol_authority_test.go`
- Extend: `internal/app/decisionprotocol/selection.go`
- Extend selection tests for override races/retries.

**Interfaces:**
- Caller cannot author a usable canonical `DecisionAuthorityAttestation` body.
- Authority materialization is provider-backed; commit-time requalification is a separate resolver operation over the stored attestation identity.
- Exact V1 class comparison is domain/class_id/version equality. Scope comparison is typed exact repository, optional episode semantics defined by provider contract, and exact action kind `COMMIT_SELECTION_OVERRIDE`; no wildcard strings.
- V1 has no generic core attestation-revocation record.
- `DecisionOverride` records explicit bounded intent over exact candidate/policy/projection/blocker digest. `SelectionCommit` stores the immutable authorization cut used at commit.

Override application request is locked as:

```go
type CreateOverrideRequest struct {
    EpisodeID                  decisionprotocol.EpisodeID
    CandidateID                decisionprotocol.CandidateID
    ExpectedPolicyDigest       string
    ExpectedProjectionDigest   string
    BlockingRequirementDigest  string
    AuthorityAttestationRef    string
    Reason                     string
}
```

`CreateOverrideRequest` deliberately has no caller `ActorRef`. The service loads the referenced trusted attestation, requires its scope to cover the exact episode/action, and writes `DecisionOverride.actor_ref` from the attested/provider-owned actor identity. A caller audit label cannot become override authority identity.

Provider interface:

```go
type MaterializeAuthorityRequest struct {
    ActorRef               string
    RequiredAuthorityClass decisionprotocol.AuthorityClass
    RequiredScope          decisionprotocol.AuthorityScope
}

type QualifyAuthorityRequest struct {
    AttestationID          string
    ExpectedActorRef       string
    RequiredAuthorityClass decisionprotocol.AuthorityClass
    RequiredScope          decisionprotocol.AuthorityScope
}

type AuthorityResolver interface {
    MaterializeDecisionAuthority(context.Context, MaterializeAuthorityRequest) (decisionprotocol.MaterializedAuthority, error)
    QualifyDecisionAuthority(context.Context, QualifyAuthorityRequest) (decisionprotocol.DecisionAuthorityQualification, error)
}
```

- [ ] **Step 1: Write RED trusted-producer tests**

Required cases:

```go
func TestAuthorityMaterializeRejectsCallerAuthoredAttestationBody(t *testing.T)
func TestAuthorityMaterializePersistsOnlyProviderQualifiedAttestation(t *testing.T)
func TestAuthorityExactClassAndScopeMatchingHasNoImplicitLattice(t *testing.T)
func TestUnavailableOrUnknownAuthorityFailsClosed(t *testing.T)
```

No test or implementation may accept `repository_owner` because caller string says so.

- [ ] **Step 2: Implement provider-backed materialization and immutable attestation storage**

Materialization calls the trusted resolver first. Only `QUALIFIED` creates/replays a canonical attestation. The canonical body stores actor, exact authority class, typed scope, resolver provider/version/capability, issued/expiry metadata, and provenance ref. UNKNOWN/UNAVAILABLE/etc. returns a result/reason but no usable attestation record.

- [ ] **Step 3: Implement immutable override intent creation with blocker freshness**

`decision.override.create` requires current candidate-scoped projection and persists:

```text
episode_id
candidate_id
policy_digest
projection_digest
blocking_requirement_digest
blocking_requirements
actor_ref                  # server-derived from the referenced trusted attestation/provider principal
authority_attestation_ref
reason
```

It does not assert future authority validity. Caller input does not contain an override actor identity. If the caller supplies a stale projection/blocker digest, return `OVERRIDE_SCOPE_STALE`/`PROJECTION_CONFLICT` rather than widening intent.

- [ ] **Step 4: Revalidate authority at the selection commit authorization point**

Override commit sequence extends Task 9:

```text
recompute exact current projection/blocker set
load exact override intent and attestation
require override policy allowed + exact required authority class
require override still targets exact episode/candidate/policy/projection/blocker digest
call QualifyDecisionAuthority NOW
require status QUALIFIED + exact actor/class/scope
construct authorization cut
atomically persist override SelectionCommit through Task-9 terminal/idempotency path
```

Authority failure is a commit-precondition failure, not a protocol requirement and not overrideable.

- [ ] **Step 5: Write temporal/retry tests for revocation/expiry and durable history**

Required timeline tests:

```text
qualified at T2, revoked before T4 commit -> OVERRIDE_AUTHORITY_NOT_ADMISSIBLE
qualified at commit, durable commit, revoked/expired later -> historical COMMITTED_WITH_OVERRIDE unchanged
qualification succeeds, local commit persistence fails -> retry calls resolver again
same idempotency key replays already durable override commit -> resolver not called again
resolver disappears for new commit -> AUTHORITY_REQUIREMENT_UNAVAILABLE / not admissible
```

`SelectionCommit.override_authorization` must retain exact attestation, class, actor, resolver versions, validated time, and qualification cut digest if available.

- [ ] **Step 6: Write normal-vs-override concurrent terminal race test**

For same candidate B and projection, race a normal commit with an override commit. Whichever terminal fact wins makes the loser receive `TERMINAL_SELECTION_CONFLICT`; the store must never collapse them because override status/ref participates in semantic intent fingerprint.

- [ ] **Step 7: Run task/race gates and commit**

```bash
go test ./internal/core/decisionprotocol ./internal/app/decisionprotocol ./internal/adapter/store -run 'Authority|Attestation|Override|Selection' -count=1
go test -race ./internal/adapter/store -run 'Override|Selection' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/core/decisionprotocol internal/app/decisionprotocol internal/adapter/store
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: authorize decision protocol overrides"
```

---

### Task 11: Expose the bounded Decision Protocol surface through IPC v2, bridge, MCP, and schemas

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `internal/adapter/ipc/decision_protocol_v2.go`
- Create: `internal/adapter/ipc/decision_protocol_test.go`
- Modify: `internal/adapter/ipc/protocol_v2.go`
- Modify: `internal/adapter/ipc/server_unix.go`
- Create: `internal/adapter/mcp/decision_protocol_input.go`
- Create: `internal/adapter/mcp/decision_protocol_call.go`
- Create: `internal/adapter/mcp/decision_protocol_test.go`
- Create: `internal/app/bridge/decision_protocol.go`
- Create: `internal/app/bridge/decision_protocol_test.go`
- Modify: `internal/app/bridge/handler.go`
- Modify: `api/schema/ipc-v2.json`
- Modify: `api/schema/mcp-input-v2.json`
- Modify: `api/schema/mcp-output-v2.json`
- Add/extend focused `api/schema/*decision_protocol*_test.go`.

**Interfaces:**
- Keep one MCP tool: `local_shell`.
- Add exactly the frozen semantic action families:

```text
decision.policy.snapshot
decision.policy.activate
decision.create
decision.inspect
decision.evaluate
decision.close_unresolved
decision.candidate.create
decision.candidate.revise
decision.experiment.define
decision.prediction.bind
decision.experiment.seal
decision.experiment.close
decision.experiment.abort
decision.assessment.record
decision.selection.propose
decision.override.create
decision.selection.commit
decision.authority.materialize
```

- Ordinary `start` gains optional top-level `experiment_id`; that is execution admission metadata, not a separate decision action.
- IPC/MCP input uses one bounded nested `decision` object for decision-action-specific fields rather than adding free-form maps. The JSON schema sets `additionalProperties:false` and action validation requires only fields allowed for that action.
- No API accepts caller-supplied observation results, qualified verifier fields, context qualification, or canonical authority attestation body.

Transport shape:

```go
type DecisionCandidateInputV1 struct {
    CandidateID        string `json:"candidate_id"`
    SemanticClaim      string `json:"semantic_claim"`
    CandidateKind      string `json:"candidate_kind,omitempty"`
}

type DecisionPredictionInputV1 struct {
    PredictionID string                                `json:"prediction_id"`
    CandidateID  string                                `json:"candidate_id"`
    Role         decisionprotocol.PredictionRole       `json:"role"`
    Predicate    decisionprotocol.ObservationPredicate `json:"predicate"`
}

type DecisionAssessmentInputV1 struct {
    AssessmentID             string                        `json:"assessment_id"`
    DeclaredContextClass     decisionprotocol.ContextClass `json:"declared_context_class"`
    DeclaredProviderIdentity string                        `json:"declared_provider_identity,omitempty"`
    PreferredCandidates      []string                      `json:"preferred_candidates"`
    SemanticRejections       []string                      `json:"semantic_rejections,omitempty"`
    Rationale                string                        `json:"rationale,omitempty"`
}

type DecisionAuthorityMaterializeInputV1 struct {
    RequiredAuthorityClass decisionprotocol.AuthorityClass `json:"required_authority_class"`
    RequiredScope          decisionprotocol.AuthorityScope `json:"required_scope"`
}

type DecisionPolicySnapshotInputV1 struct {
    Content decisionprotocol.PolicyContent `json:"content"`
}

type DecisionRequestV1 struct {
    EpisodeID                    string                               `json:"episode_id,omitempty"`
    EpisodeKind                  decisionprotocol.EpisodeKind         `json:"episode_kind,omitempty"`
    PredecessorEpisodeID         string                               `json:"predecessor_episode_id,omitempty"`
    CandidateID                  string                               `json:"candidate_id,omitempty"`
    ParentCandidateID            string                               `json:"parent_candidate_id,omitempty"`
    ExperimentID                 string                               `json:"experiment_id,omitempty"`
    Policy                       *DecisionPolicySnapshotInputV1       `json:"policy,omitempty"`
    ActivationID                 string                               `json:"activation_id,omitempty"`
    PolicyDigest                 string                               `json:"policy_digest,omitempty"`
    ProposalGeneration           string                               `json:"proposal_generation,omitempty"`
    ExpectedPreviousPolicyDigest string                               `json:"expected_previous_policy_digest,omitempty"`
    Candidate                    *DecisionCandidateInputV1            `json:"candidate,omitempty"`
    Prediction                   *DecisionPredictionInputV1           `json:"prediction,omitempty"`
    Assessment                   *DecisionAssessmentInputV1           `json:"assessment,omitempty"`
    AuthorityRequest             *DecisionAuthorityMaterializeInputV1 `json:"authority_request,omitempty"`
    AuthorityAttestationRef      string                               `json:"authority_attestation_ref,omitempty"`
    ActorRef                     string                               `json:"actor_ref,omitempty"`
    ExpectedPolicyDigest         string                               `json:"expected_policy_digest,omitempty"`
    ExpectedActivationRef        string                               `json:"expected_activation_ref,omitempty"`
    ExpectedProjectionDigest     string                               `json:"expected_projection_digest,omitempty"`
    BlockingRequirementDigest    string                               `json:"blocking_requirement_digest,omitempty"`
    IdempotencyKey               string                               `json:"idempotency_key,omitempty"`
    OverrideRef                  string                               `json:"override_ref,omitempty"`
    AbortPhase                   decisionprotocol.AbortPhase          `json:"abort_phase,omitempty"`
    UnresolvedDimensions         *[]string                            `json:"unresolved_dimensions,omitempty"`
    Reason                       string                               `json:"reason,omitempty"`
}
```

These five nested `*InputV1` structs plus `DecisionRequestV1` are **adapter DTOs owned by `internal/adapter/ipc`**, not new core canonical types. IPC/MCP map them losslessly to the application contracts defined by Tasks 2/3/4/8/9/10. `RepositoryID`, `WorkspaceID`, current source generation, effective policy activation, activation generation/time, canonical record sequence, qualified verifier context, observation results, trusted authority actor identity, and canonical policy snapshot identity (`repository_id`, `policy_digest`) are server-derived and therefore intentionally absent as caller-writable fields. `DecisionPolicySnapshotInputV1` carries semantic `PolicyContent` only. `proposal_generation` is a required string identity for `decision.policy.activate`, not a scalar counter, and must match `^gen_[0-9a-f]{64}$`.

The **machine-readable authority** for action-to-request mapping is `action_request_matrix` in `docs/superpowers/plans/2026-08-19-decision-protocol-v1-traceability.json`; Task 11 implementation/schema tests must match it exactly. Human-readable equivalent:

| Action | Required caller fields | Optional caller fields | Server-derived / never caller-authoritative |
|---|---|---|---|
| `decision.policy.snapshot` | `policy` | — | repository ID + canonical `policy_digest`; current repository compatibility check; canonical seq/index materialization |
| `decision.policy.activate` | `activation_id`, `policy_digest`, `proposal_generation`, `expected_previous_policy_digest`, `actor_ref` | — | repository ID, `activation_generation`, `activated_at`, authority=`explicit_caller` |
| `decision.create` | `episode_id`, `episode_kind`, `actor_ref` | `predecessor_episode_id`, `expected_policy_digest`, `expected_activation_ref` | repository/workspace/source generation and current effective applicable activation |
| `decision.inspect` | `episode_id` | `candidate_id` | projection/canonical cut |
| `decision.evaluate` | `episode_id`, `candidate_id` | — | projection/canonical cut |
| `decision.close_unresolved` | `episode_id`, `actor_ref`, `expected_projection_digest`, `reason`, `unresolved_dimensions` | — | terminal sequence/time |
| `decision.candidate.create` | `episode_id`, `candidate`, `actor_ref` | — | `declared_at` |
| `decision.candidate.revise` | `episode_id`, `parent_candidate_id`, `candidate`, `actor_ref` | — | parent activeness/lineage and `declared_at` |
| `decision.experiment.define` | `episode_id`, `experiment_id`, `actor_ref` | — | `declared_at` |
| `decision.prediction.bind` | `episode_id`, `experiment_id`, `prediction` | — | source generation and `committed_at` |
| `decision.experiment.seal` | `experiment_id`, `actor_ref` | — | sealed digest/base cut/pairs/time |
| `decision.experiment.close` | `experiment_id`, `actor_ref` | — | server-derived observation binding/closure time |
| `decision.experiment.abort` | `experiment_id`, `abort_phase`, `actor_ref`, `reason` | — | execution-link relation/time |
| `decision.assessment.record` | `episode_id`, `assessment`, `actor_ref` | — | qualified context/provider cut if available |
| `decision.selection.propose` | `episode_id`, `candidate_id`, `actor_ref` | `reason` | `proposal_id`, proposal time; `reason` maps to canonical rationale |
| `decision.override.create` | `episode_id`, `candidate_id`, `expected_policy_digest`, `expected_projection_digest`, `blocking_requirement_digest`, `authority_attestation_ref`, `reason` | — | `override_id`, trusted actor identity from attestation/provider principal; exact current blocker set validation/time |
| `decision.selection.commit` | `episode_id`, `candidate_id`, `actor_ref`, `expected_policy_digest`, `expected_projection_digest`, `idempotency_key` | `override_ref` | `commit_id`, semantic intent fingerprint/terminal seq/time and override authorization cut |
| `decision.authority.materialize` | `authority_request` | — | **trusted actor identity/principal**, resolver qualification, attestation body/id/time |

Per-action validation rejects every field not present in that row. In particular, `decision.authority.materialize` and `decision.override.create` reject caller `actor_ref`; `MaterializeAuthorityRequest.ActorRef` and canonical `DecisionOverride.actor_ref` are populated only from the trusted transport/provider/attestation identity in Tasks 10/12. `decision.policy.snapshot.policy` is `DecisionPolicySnapshotInputV1`, containing `content` only; nested caller `repository_id`/`policy_digest` fields are outside the transport schema. `decision.policy.activate.proposal_generation` is required and must match `^gen_[0-9a-f]{64}$`. `decision.policy.activate.expected_previous_policy_digest` is required and must be exactly `"absent"` or match `^pol_[0-9a-f]{64}$`; omission/empty string is invalid, including first activation. `decision.close_unresolved.unresolved_dimensions` is required as an explicit array (empty is valid) so omission cannot be confused with transport loss. The shared DTO therefore uses `*[]string`: nil means absent, while a non-nil pointer to an empty slice round-trips as `[]` through IPC/bridge/MCP.

Adapter mapping aliases are also frozen:

```text
DecisionRequestV1.expected_projection_digest
  → CloseUnresolvedRequest.ProjectionDigest for decision.close_unresolved
  → CommitSelectionRequest.ExpectedProjectionDigest for decision.selection.commit
  → CreateOverrideRequest.ExpectedProjectionDigest for decision.override.create

DecisionRequestV1.reason
  → SelectionProposal.Rationale for decision.selection.propose
  → CloseUnresolvedRequest.Reason / CreateOverrideRequest.Reason / AbortExperiment reason for their exact actions

DecisionRequestV1.abort_phase
  → AbortExperiment AbortPhase

DecisionRequestV1.authority_attestation_ref + blocking_requirement_digest
  → CreateOverrideRequest exact identity fields
```

No adapter may substitute a server-derived value for a missing required caller field merely because the application layer could theoretically load it.


- [ ] **Step 1: Write RED strict-decode/action-field tests**

Build the test table from the exact 18-row request matrix above: for every action, test one valid minimum payload, every required-field omission, every optional-field acceptance, and representative cross-action field rejection. Add explicit mapping tests for `PutPolicySnapshotRequest`, `ActivatePolicyRequest`, `CreateEpisodeRequest`, `AbortExperiment`, `CloseUnresolvedRequest`, `CreateOverride`, and `CommitSelection` so no transport field is silently server-invented or dropped. Minimum security tests:

```go
func TestDecisionAssessmentInputRejectsQualifiedContextFields(t *testing.T)
func TestDecisionAuthorityInputRejectsCallerAttestationBody(t *testing.T)
func TestDecisionAuthorityMaterializeRejectsCallerActorRef(t *testing.T)
func TestDecisionObservationResultsHaveNoCallerInputField(t *testing.T)
func TestDecisionPolicySnapshotInputContainsContentOnly(t *testing.T)
func TestDecisionPolicyActivateRequiresGenerationDigestAndPreviousSentinelOrDigest(t *testing.T)
func TestStartAcceptsOptionalExperimentIDAndPollRejectsIt(t *testing.T)
```

- [ ] **Step 2: Implement IPC v2 mapping and daemon action interface**

Add focused decision methods to `ipc.Actions` through a composed sub-interface so the existing interface stays under the eight-method hard cap; follow the verification/mutation-scope split-file pattern. Map `decision.policy.snapshot` exactly to Task-2 `PutPolicySnapshotRequest{RepositoryID: serverResolvedRepositoryID, Content: req.Policy.Content}`; transport never accepts a canonical `PolicySnapshot` body. Map `decision.policy.activate` exactly to `ActivatePolicyRequest`, preserving the required `gen_<64hex>` proposal-generation identity and required explicit previous-policy sentinel/digest. Resolve repository/workspace/trusted transport context server-side only where the action matrix marks it server-derived. `inspect/evaluate` handlers call projection only and never `Start`.

- [ ] **Step 3: Implement bridge forwarding and preserve structured typed responses**

Add `isDecisionProtocolAction(action string)` with the exact frozen set. Bridge request/response carries the bounded decision payload/projection/result; no prose parsing or loss of reason codes. Add bridge tests proving no decision action is misclassified as execution.

- [ ] **Step 4: Implement MCP decode/call path under `local_shell`**

The MCP adapter validates the same action-field matrix before forwarding. Add no-spawn tests with a fake bridge client that counts `start` calls; all decision actions except the caller's separate ordinary `start` must keep the count at zero.

- [ ] **Step 5: Update JSON schemas and schema fixtures**

Apply Decision Protocol schema additions on top of the reviewed structured-result/pytest schema at `39de4426a95cfb58cbb99a75165b9feb5cc7169c`. Preserve `structured_schema_versions`, `structured_input_kinds`, artifact-blob/capture definitions, pytest adapter advertisement, and structured artifact limits. Add closed definitions for decision action enums, nested decision request declarations, bounded projection/results, reason codes, and optional start `experiment_id`. `additionalProperties:false` must reject server-owned fields on input.

- [ ] **Step 6: Run transport/schema gates and commit**

```bash
go test ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge ./api/schema -run 'Decision|ExperimentID' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add internal/adapter/ipc internal/adapter/mcp internal/app/bridge api/schema
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: expose decision protocol transport"
```

---

### Task 12: Compose the daemon runtime, capability advertisement, compatibility firewall, and built-in authority provider

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `cmd/shellbeam/decision_protocol.go`
- Create: `cmd/shellbeam/decision_protocol_test.go`
- Modify: `cmd/shellbeam/command_daemon.go`
- Create: `internal/core/capability/decision_protocol.go`
- Create: `internal/core/capability/decision_protocol_test.go`
- Modify: `internal/core/capability/catalog.go`
- Modify: `internal/core/capability/catalog_test.go`
- Create: `internal/app/decisionprotocol/service.go`
- Extend: `internal/app/decisionprotocol/authority.go` for resolver registry/default provider.
- Add focused daemon/composition tests.

**Interfaces:**
- Compose one Decision Protocol service from the existing store, canonical source-generation provider, receipt/structured/verification readers, and authority resolver registry.
- Advertise capability only when canonical store initialization and required read-side dependencies are available.
- V1 built-in authority provider may qualify only exact class `{domain:"shellbeam", class_id:"explicit_caller", version:1}` under the current authenticated OS-user/explicit-call boundary. The attested `actor_ref` is provider/transport-derived from that authenticated boundary (for the built-in Unix IPC provider, an exact provider-owned binding to the accepted peer UID); an arbitrary caller-supplied audit label must never be copied into a qualified attestation as actor identity. It must not claim repository ownership/maintainership. Future provider classes plug into the registry without changing Decision Protocol schema.
- Repositories with no explicitly activated applicable Decision Policy cannot create a governed episode; ordinary ShellBeam remains unchanged and does not acquire implicit hard gates.

Capability shape:

```go
type DecisionProtocolSupport struct {
    SchemaVersion      int      `json:"schema_version"`
    ProtocolVersion    int      `json:"protocol_version"`
    PredicateKinds     []string `json:"predicate_kinds"`
    AuthorityProviders []string `json:"authority_providers,omitempty"`
    OneExecutionPerExperiment bool `json:"one_execution_per_experiment"`
}
```

- [ ] **Step 1: Write RED composition/capability tests**

Required tests:

```go
func TestDecisionProtocolCapabilityAdvertisesClosedV1Contract(t *testing.T)
func TestDecisionProtocolRuntimeUsesExistingStructuredAndVerificationReaders(t *testing.T)
func TestBuiltInAuthorityProviderOnlyQualifiesShellBeamExplicitCaller(t *testing.T)
func TestRepositoryWithoutActivatedDecisionPolicyKeepsOrdinaryStartSemantics(t *testing.T)
```

- [ ] **Step 2: Implement the service facade without a planner/scheduler dependency**

`Service` composes policy/episode/candidate/experiment/evaluation/selection/authority components. It must have no process-owner/start scheduler field. The only coupling to execution is read-side admission metadata and the daemon's explicit experiment-aware start admission path from Task 5.

- [ ] **Step 3: Implement the exact built-in explicit-caller resolver and registry**

Registry key is exact authority class domain/id/version. Thread a trusted caller principal from the authenticated IPC/bridge boundary into authority materialization without exposing a JSON field that can forge it. For Unix IPC the principal binds to the peer UID already accepted by `authListener`; MCP/bridge forwarding preserves that server-owned principal and cannot replace it with `decision.actor_ref`. The built-in provider's exact V1 actor binding is `shellbeam:explicit_caller:uid:<decimal-peer-uid>` and is constructed only from the accepted peer UID. The built-in provider derives its canonical attestation `actor_ref` from this provider-owned principal and returns QUALIFIED only for the current trusted ShellBeam explicit caller scope/action contract. Requests for `repository_owner`, `maintainer`, arbitrary domains, wildcard-like strings, or caller attempts to substitute the actor identity return UNKNOWN/UNAVAILABLE or strict-input rejection, never a promoted class.

- [ ] **Step 4: Add compatibility regression tests for ordinary execution/replay**

Run existing fingerprint corpus and reservation/replay tests with no experiment binding and assert no changed expected fingerprints or behavior. Add tests that disabled/unavailable Decision Protocol capability does not alter `start`, `poll`, `write`, `kill`, structured results, evidence, or verification semantics.

- [ ] **Step 5: Prove candidate commit remains upstream of verification completion**

Composition test commits a protocol-clear candidate and then inspects downstream verification state. Assert no automatic mutation, test, evidence, or verification-clear record is created. Decision projection may say COMMITTED; it must never synthesize task `done` or verification `clear`.

- [ ] **Step 6: Run composition/regression gates and commit**

```bash
go test ./internal/core/capability ./internal/app/decisionprotocol ./cmd/shellbeam -run 'DecisionProtocol|ExplicitCaller|OrdinaryStart|CandidateCommit' -count=1
go test ./internal/core/operation ./internal/app/daemon ./internal/adapter/store -run 'Fingerprint|Replay|Reservation' -count=1
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git add cmd/shellbeam internal/core/capability internal/app/decisionprotocol
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "feat: compose decision protocol runtime"
```

---

### Task 13: Prove end-to-end protocol, restart recovery, concurrency/security invariants, and final acceptance

**Mandatory pre-task base gate — run before editing any file in this task:**

```bash
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
```

If this fails, stop before making task-local edits and return to the Task-0 drift/integration gate. Do not continue on a stale implementation base.


**Files:**
- Create: `cmd/shellbeam/decision_protocol_acceptance_test.go`
- Create: `cmd/shellbeam/decision_protocol_security_test.go`
- Create: `cmd/shellbeam/decision_protocol_restart_test.go`
- Extend focused race/fault tests only where an invariant lacks coverage.
- Update: `docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md` with final acceptance section.

**Interfaces:**
- This task adds no new semantic capability. It proves the frozen contract across real daemon/store/IPC/MCP composition.
- Benchmark/oracle metrics remain unavailable unless an exhaustive qualifying oracle harness is explicitly used; production acceptance must not invent `Pass@N`, recall, or regret.

- [ ] **Step 1: Add the normal end-to-end acceptance sequence**

Use the real composed runtime and `local_shell`-equivalent IPC/MCP calls:

```text
policy.snapshot
policy.activate (explicit_caller)
decision.create
candidate.create A
candidate.create B
experiment.define E1
prediction.bind A/B on same dimension with different outcomes
experiment.seal E1
ordinary start(operation_id=op1, experiment_id=E1)
wait terminal
experiment.close E1
decision.evaluate(candidate B)
selection.propose(B)
selection.commit(B, expected projection digest, idempotency key)
inspect -> COMMITTED
```

Assert no decision action started `op1`; only the explicit ordinary start did. Assert `ExperimentExecutionLink` existed durably before spawn evidence.

- [ ] **Step 2: Add end-to-end blocked/uncertainty escape tests**

Cover:

```text
single lineage under challenge policy -> BLOCKED and normal commit rejected
unresolved verifier qualification -> INDETERMINATE and normal commit rejected
same episodes can close_unresolved -> CLOSED_UNRESOLVED
budget exhausted while blocked -> no further protocol-governed experiment admission; close_unresolved remains available
```

- [ ] **Step 3: Add end-to-end override authorization tests**

With policy requiring built-in `shellbeam/explicit_caller/v1`:

```text
create override for exact blocker digest
materialize trusted explicit-caller attestation
commit override -> COMMITTED_WITH_OVERRIDE
```

Then cover stale blocker digest, wrong class/scope, unavailable provider, revocation-before-commit via fake resolver, and post-durable expiry/revocation preserving historical commit.

- [ ] **Step 4: Add restart/recovery acceptance**

Restart store/daemon between each sensitive durable boundary in separate focused subtests:

```text
policy activation
candidate revision
experiment seal
linked admission before spawn replay
observation binding
selection commit/idempotency replay
override authorization durable commit
```

After reopen, projections/digests/terminal state must reproduce from canonical records; event journal loss/rebuild must not alter canonical replay truth.

- [ ] **Step 5: Add concurrency/security race suite**

Run with `-race` and assert all frozen atomicity boundaries:

```text
concurrent candidate revision -> one replacement
seal vs prediction bind -> either prediction is in sealed digest or bind rejected; never post-seal inclusion ambiguity
concurrent execution admission -> one ExperimentExecutionLink
close vs abort -> one terminal experiment record
concurrent observation materializers -> one binding
commit vs close_unresolved -> one episode terminal record
normal vs override commit -> one epistemically exact terminal record
stale projection commit vs new assessment -> PROJECTION_CONFLICT
```

Security assertions include caller attempts to inject qualified verifier fields, attestation body, wildcard authority class, historical activation, unrelated verification evidence, and caller-supplied observation results; all fail closed before durable semantic promotion.

- [ ] **Step 6: Prove production oracle analytics do not invent unavailable metrics**

Create a production episode where losing candidate is not exhaustively evaluated. Projection/API must either omit oracle metrics or return the frozen unavailable form:

```text
oracle_metrics = unavailable
reason = candidates_not_exhaustively_evaluated
```

No inferred candidate recall/selection regret appears.

- [ ] **Step 7: Run final fresh verification**

Run all of these from a cleanly built worktree state after the Task-13 code is staged/unstaged as appropriate:

```bash
go test ./internal/core/decisionprotocol ./internal/app/decisionprotocol ./internal/adapter/store ./internal/app/daemon ./internal/adapter/ipc ./internal/adapter/mcp ./internal/app/bridge ./api/schema ./cmd/shellbeam -count=1
go test -race ./internal/app/decisionprotocol ./internal/adapter/store ./internal/app/daemon ./internal/adapter/ipc ./cmd/shellbeam -run 'DecisionProtocol|Experiment|Selection|Override' -count=1
go test ./...
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
go run ./tools/devctl test --dirty --base "$SHELLBEAM_BASE_REF" --json
git diff --check
python3 scripts/check-decision-protocol-v1-plan-traceability.py
```

Expected: all exit 0 and traceability prints `PASS invariants=48/48 sections=57/57 tasks=14/14`.

- [ ] **Step 8: Update final evidence and commit**

Append exact final command results, current HEAD-before-final-commit, race/full-suite status, and any measured compatibility fingerprints to the baseline evidence file. Then:

```bash
git add cmd/shellbeam/decision_protocol_* internal docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md
git diff --cached --check
git -c core.hooksPath=.githooks commit -m "test: prove decision protocol v1 acceptance"
git status --short --branch
```

Expected: clean worktree after commit.

---

## Plan Completion Gate

Before declaring implementation complete, verify every frozen invariant is mapped by the traceability file and every task commit exists in order. Then run:

```bash
python3 scripts/check-decision-protocol-v1-plan-traceability.py
export SHELLBEAM_BASE_REF="$(scripts/decision-protocol-v1-implementation-base.sh)"
git log --oneline "$SHELLBEAM_BASE_REF"..HEAD
git diff --check "$SHELLBEAM_BASE_REF"..HEAD
go test ./...
```

The implementation is **not** done merely because Decision Protocol selects a candidate. Completion requires the Decision Protocol acceptance suite plus the repository's normal verification gates to pass. No push/PR/merge is part of this plan unless separately authorized.
