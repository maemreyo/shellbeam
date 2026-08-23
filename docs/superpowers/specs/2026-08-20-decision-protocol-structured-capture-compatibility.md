# Decision Protocol V1 / Structured Capture Compatibility Amendment

**Status:** Approved implementation amendment for Decision Protocol V1 after upstream `main` advanced to `39de4426a95cfb58cbb99a75165b9feb5cc7169c`.

**Parent spec:** `docs/superpowers/specs/2026-08-19-decision-protocol-design.md`

**Parent plan:** `docs/superpowers/plans/2026-08-19-decision-protocol-v1.md`

## 1. Scope and reviewed upstream authority

The original Decision Protocol implementation base was `27207d94b097040b571081c8c49d9c09487460c5`. Upstream `main` now includes the structured artifact / pytest result work through `39de4426a95cfb58cbb99a75165b9feb5cc7169c`.

The reviewed owner-set patch is frozen by these identities:

- reviewed owner overlap base: `39de4426a95cfb58cbb99a75165b9feb5cc7169c`
- owner patch SHA-256: `d9d1429a2a6e0ce413b978fc89cdfc1505a1b1a24c0527d7ae1c54a51c91c28d`
- owner path-list SHA-256: `2f5ae958bce1c3d33ab3ea391e85c8d24cf48afdc140512cdbe41ae4635cd05f`

The eight files changed by both implementation lines are:

```text
internal/adapter/store/repository.go
internal/adapter/store/reservation.go
internal/app/daemon/admission.go
internal/app/daemon/project_command.go
internal/app/daemon/service.go
internal/app/daemon/types.go
internal/core/operation/intent.go
internal/core/operation/persistence.go
```

This amendment authorizes integration of exactly that reviewed owner patch. It does not authorize later owner drift. Any owner-path change after the reviewed overlap base requires another explicit compatibility review.

## 2. Preserved invariants

All frozen Decision Protocol V1 invariants remain in force. In particular:

1. `experiment_id` is immutable first-admission observation/replay identity and does not change request or execution meaning.
2. An omitted `experiment_id` preserves the current upstream ordinary-operation fingerprint corpus exactly.
3. A linked Decision Protocol experiment still creates at most one observation-producing operation link, recovery-indivisible with successful admission before spawn.
4. Structured capture authority remains independently qualified and immutable; Decision Protocol does not weaken, bypass, or synthesize it.
5. No new canonical Decision Protocol record kind is introduced by this amendment.
6. Ordinary starts, structured results, evidence, verification, and replay remain unchanged when no Decision Protocol experiment is supplied.

## 3. Observation-binding composition

Upstream now binds `StructuredCaptureDigest` into `ObservationBindingFingerprint`. Decision Protocol must compose with that authority rather than replace it.

The frozen composition order is:

```text
legacy observation binding
  -> decision experiment binding, when experiment_id is present
  -> verification-attempt binding, when present
  -> structured-capture binding, when StructuredCaptureDigest is present
```

Consequences:

- With `experiment_id == ""`, the fingerprint is byte-for-byte identical to upstream `main` behavior.
- With an experiment present, changing or omitting the experiment changes only observation identity.
- Verification attempt and structured capture retain their upstream semantics and continue to participate in the same final immutable observation fingerprint.
- Every location that recomputes observation identity (raw start replay, project-command replay, reservation validation, structured-capture preparation) must use the same composed helper/path. No call site may silently omit `ExperimentID` or `StructuredCaptureDigest`.

## 4. Pre-admission session identity recovery

Structured pytest capture persists an authority whose digest includes `SessionID` before the final operation reservation is committed. Decision Protocol experiment admission also freezes `SessionID` in its private recovery claim. Crash recovery must therefore choose one stable session identity before structured-capture preparation.

For an experiment-bound start with no already-durable operation reservation, resolve the pre-admission session identity in this order:

1. Existing Decision Protocol experiment admission claim for the requested `experiment_id` and `operation_id`.
2. Existing structured capture authority for the same `operation_id`, if no Decision Protocol claim exists.
3. Otherwise allocate the normal fresh session ID.

If both a Decision Protocol claim and structured capture authority exist, their session IDs must agree; disagreement fails closed before new durable admission or spawn.

The structured-capture fallback is recovery identity only. It does not make structured capture authoritative for `experiment_id`; final experiment admission still validates the requested experiment, workspace/source generation, request fingerprint, execution fingerprint, final composed observation fingerprint, and single-execution constraint.

This rule closes both crash windows:

- Decision claim durable, reservation/link absent.
- Structured capture authority durable, Decision claim/reservation absent.

No new durable preclaim type is added.

## 5. Store and daemon integration

The repository may expose one focused read-only admission-identity method to the daemon application layer. It may inspect the existing private Decision Protocol claim and existing structured capture authority, but must not create either while resolving identity.

The existing final `ReserveExperimentOperation` remains the only Decision Protocol operation-reservation commit path and preserves lock order:

```text
per-operation lock -> admission lock -> decisionProtocolMu
```

Structured-capture preparation remains outside those locks. Its resulting adapter/digest is incorporated into the final observation fingerprint before `ReserveExperimentOperation` is invoked.

On final reservation failure, existing structured-capture compensation/abandon behavior remains in force.

## 6. Schema and capability compatibility

Task 11 schema work must be applied on top of upstream structured-result schema changes, not by replacing them. Preserve:

- structured schema/input kinds,
- artifact blob/capture definitions,
- pytest adapter advertisement,
- structured artifact limits,
- existing legacy catalog stripping behavior.

Decision Protocol adds only its bounded transport fields/actions and optional start `experiment_id` as specified by the parent plan.

## 7. Required compatibility tests

Before Task 11 resumes, add/retain focused tests proving:

```text
ordinary omitted experiment -> upstream observation fingerprint unchanged
experiment + verification -> both identities participate
experiment + structured capture -> both identities participate
experiment + verification + structured capture -> deterministic composed fingerprint
raw start replay recomputes the same composed identity
project-command replay recomputes the same composed identity
claim-only restart reuses frozen session before structured capture preparation
capture-authority-only restart reuses frozen session before Decision claim creation
claim + capture authority session disagreement fails closed
experiment + pytest admission creates one reservation and one canonical execution link before spawn
```

Run both Decision Protocol admission tests and upstream structured capture/pytest admission tests after integration.

## 8. Task-0 ratchet

Task 0 may accept `39de4426a95cfb58cbb99a75165b9feb5cc7169c` only when the reviewed owner patch and path-list SHA-256 values above match exactly. After that SHA becomes the durable implementation base, future integration audits compare owner drift from this reviewed overlap base. Non-owner drift may still be integrated under the existing topology rules; any new owner drift stops for another amendment/re-review.
