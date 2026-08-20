# Context Exec Design Technical Review

**Scope:** adversarial technical review of `docs/superpowers/specs/2026-08-18-delegated-context-exec-evidence-design.md` before any implementation work.

```text
H5_DESIGN_ALLOWED=true
CONTEXT_EXEC_AVAILABLE=false
CONTEXT_EXEC_IMPLEMENTATION_ALLOWED=false
HUMAN_APPROVAL_ARTIFACT_PRESENT=false
```

## 1. Inputs reviewed

- H4 completion evidence: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h4-secret-privacy.md`
- Master design §§29, 40, 57, 62
- Current delegated receipt rule: `evidence_authority=session_lifecycle_only`
- Current evidence consumers reject `session_lifecycle_only` as mechanical verification authority
- H5 Task-1 mandatory review questions and counterexample matrix

H4 explicitly records `H5_DESIGN_ALLOWED=true` while `CONTEXT_EXEC_AVAILABLE=false`; this review does not reinterpret the design gate as implementation permission.

## 2. Selected architecture

**Selected:** short-lived authenticated helper launched from a proved delegated-shell boundary, with helper-owned child process, stdout/stderr pipes, timeout/signal/reap, and descriptor-bound executable identity.

**Rejected:** prompt/transcript wrapping as mechanical evidence because mixed pane bytes cannot establish causal attribution.

**Rejected for V1:** resident in-shell context agent because it increases credential/control lifetime, replay surface, resource creep, and epoch complexity.

## 3. Six first-class review questions

### 3.1 Output attribution — PASS candidate

Canonical output is only the child stdout/stderr pipes owned by the authenticated helper. Delegated pane bytes are excluded. Optional pane mirroring would be presentation-only.

### 3.2 Child/job-control ownership — PASS candidate

V1 is non-TTY, stdin closed, one child process group, helper-owned signals/timeouts/reap. The shell does not own child evidence streams.

### 3.3 Exactly-once + epoch semantics — PASS candidate

Reservation precedes helper launch; one reservation binds one helper generation; changed replay conflicts; ambiguous launch/spawn never causes blind duplicate execution. Active child blocks authority transfer completion until proved terminal or cancelled/reaped under the original epoch.

### 3.4 Helper authentication identity — PASS candidate

`context_exec_id` and opaque pane-visible launch id are correlation/routing identities only. Helper claim authority is a separate high-entropy one-generation private-channel capability combined with peer ancestry/executable proof.

### 3.5 Control-environment stripping — PASS candidate

Only a closed ShellBeam internal-control namespace and helper/control file descriptors are stripped. User exported context is preserved. The design explicitly does not claim workload secrecy if the workload itself prints inherited secrets.

### 3.6 Actual executable identity — PASS candidate with platform qualification requirement

Executable resolution happens in the helper under inherited PATH. Strong evidence additionally requires descriptor-bound execution of the same executable object whose identity was observed. Path resolve followed by later `execve(path)` is rejected as TOCTOU-vulnerable.

## 4. Counterexample attack results

### Stale epoch helper after new handoff

**Result:** fail closed. Exact session/provider/shell/authority generation is revalidated; stale helper cannot authenticate/spawn.

### Lost response causes duplicate helper/child

**Result:** fail closed. Reserve-before-launch plus one helper generation and ambiguity state prevent duplicate-by-default recovery.

### Cwd changes after reservation

**Result:** fail closed. Helper claim binds current shell/cwd boundary; mismatch prevents spawn.

### Secret env accidentally logged

**Result:** design prohibits env values/hash/length in daemon/helper log/error/public/durable metadata. Environment is inherited, not captured.

### Private capture remains active

**Result:** request rejected before helper launch.

### Helper auth succeeds but child does not spawn

**Result:** explicit spawn failure. No terminal/exit evidence is invented.

### Child exits while daemon disconnected

**Result:** same-generation bounded recovery may report terminal state; otherwise conservative termination/ambiguity truth applies. No second child.

### Helper dies after child spawn

**Result:** exact process-group cleanup where provable; authority degrades to helper-lost/ambiguous. No invented exit status.

### Human handoff requested during active child

**Result:** transfer cannot complete until terminal or cancellation+reap. No epoch silently inherits the in-flight child.

### Nested/replaced shell after request

**Result:** generation/boundary mismatch; no guessed shell syntax.

### Executable absent

**Result:** stable spawn failure. No shell-string fallback.

### Background prompt/job writes to pane

**Result:** irrelevant to canonical child output because child output uses separately owned pipes.

### Public context_exec_id used by another same-user process

**Result:** insufficient to claim reservation; private capability and peer identity proof are mandatory.

### Helper-only token/fd would reach child

**Result:** control namespace stripping + FD closure is a spawn prerequisite. Failure to prove that boundary blocks strong authority.

### PATH resolves an unexpected executable

**Result:** inherited delegated PATH is the execution authority; result records requested versus actual identity instead of pretending daemon expectation won.

### Executable path swaps between observation and exec

**Result:** descriptor-bound execution is mandatory for strong authority; path-only execution is not approvable.

## 5. Evidence-authority review

The proposed `context_exec_child_owned_v1` authority is materially stronger than shell transcript evidence because the system owns and authenticates:

```text
request reservation
helper generation
child spawn
actual executable identity
stdout/stderr pipes
child terminal reap
```

It remains weaker/different from a generic direct daemon command in context-hermeticity. The inherited environment/cwd are provenance, not verified deterministic inputs. Context-exec alone does not imply artifact verification, filesystem/network isolation, resource enforcement, or hermetic execution.

Therefore:

- delegated session receipt remains `session_lifecycle_only`;
- pane transcript is never promoted;
- mechanical evidence consumers may accept `context_exec_child_owned_v1` only for supported child identity/output/exit facts and only after validating the complete context-exec authority contract;
- stronger evidence consumers keep their independent requirements.

## 6. Design self-review

### Placeholder scan

No implementation-semantic `TBD`/`TODO` remains. The only placeholder is the intentionally human-supplied `<exact 64-hex digest>` in the approval artifact example.

### Internal consistency

The design consistently requires reserve-before-launch, one helper generation, separate output pipes, public privacy, agent ownership, non-TTY/closed-stdin V1, and no transcript authority.

### Scope

V1 excludes resident helpers, TTY child mode, stdin streaming, shell functions/aliases, arbitrary env overrides, pane output evidence, and broad-provider implementation. These exclusions keep the evidence contract implementable as one high-assurance slice.

### Ambiguity review

The design explicitly chooses conservative semantics for transfer-vs-active-child, daemon disconnect, ambiguous helper launch/spawn, same-user claim attempts, control-environment stripping, and executable TOCTOU.

## 7. Reviewer decision requested

Technical authoring review outcome:

```text
DESIGN_TECHNICAL_REVIEW=READY_FOR_HUMAN_REVIEW
IMPLEMENTATION_AGENT_APPROVAL=NOT_AUTHORITY
```

A human reviewer must independently inspect the exact committed spec and supply the closed approval JSON bound to its SHA-256. Until then:

```text
CONTEXT_EXEC_AVAILABLE=false
CONTEXT_EXEC_IMPLEMENTATION_ALLOWED=false
```

## 8. Exact authored spec digest

```text
spec_sha256=b3b10c5481fee65118c5fc062255e6f43b7f357d15a17e1069b1f15abf52a32c
```

This digest is pre-commit authored-byte evidence only. The human approval artifact must bind the exact committed spec bytes; verify the digest again after commit before accepting approval.
