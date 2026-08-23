# Context Exec Helper-Claim Shell Continuity Amendment

**Status:** architectural amendment approved in chat for authoring on 2026-08-22; implementation remains blocked until the exact committed amendment is reviewed and approved by the human reviewer.

**Base design:** `docs/superpowers/specs/2026-08-18-delegated-context-exec-evidence-design.md`

**Base design SHA-256:** `b3b10c5481fee65118c5fc062255e6f43b7f357d15a17e1069b1f15abf52a32c`

The base design remains byte-for-byte unchanged so its existing human approval artifact keeps its original provenance. This document amends only the helper-authentication continuity rule described below.

```text
PUBLIC_ACTION=context.exec
PUBLIC_SCHEMA_CHANGE=false
HELPER_WIRE_PROTOCOL_CHANGE=false
BASE_APPROVAL_REPLACED=false
AMENDMENT_IMPLEMENTATION_ALLOWED=false
```

## 1. Problem discovered by Task-8 native verification

The base design correctly requires the daemon to prove a qualified current shell immediately before helper-launch delivery and to reject shell replacement or authority drift before child spawn.

Native verification exposed a lifecycle fact that makes a second shell-family probe during helper authentication unsound:

```text
pre-launch prompt boundary:
    pane_current_command = zsh
    shell identity        = exact/qualified

helper authentication window:
    pane_current_command = shellbeam
    direct parent         = the same zsh pane shell
    pane shell PID        = unchanged
    pane shell process identity = unchanged
    helper executable     = exact approved ShellBeam binary
    helper is foreground on the same pane TTY
```

`pane_current_command` reports the current foreground command, not an immutable identity for the pane shell. Therefore, after the approved helper starts, re-running shell-family detection from `pane_current_command` can turn a still-valid zsh/fish/bash boundary into `ShellUnknown` solely because the helper itself is foreground.

Treating that expected transition as shell replacement is a false negative. Whitelisting `shellbeam` as a shell family would be a false positive and violates the existing no-guess policy.

## 2. Selected amendment

Shell qualification and shell continuity are two different proofs at two different lifecycle points.

### 2.1 Pre-launch shell qualification remains mandatory

Immediately before helper-launch delivery, the daemon MUST still prove all base-design admission facts, including:

```text
delegated session lifecycle = live
provider generation = current
provider owner = agent
desired owner = agent
agent ingress = writable
authority epoch = exact
privacy capture = public
privacy release = not pending
current shell family = exact supported fish/zsh/bash
shell runtime identity = qualified and unchanged
safe prompt boundary = current and qualified
pane shell PID = current
pane TTY = current
cwd = current
```

Unknown, nested, replaced, or otherwise unqualified shell state at this point still fails closed. This amendment does not permit launch from an unknown shell and does not weaken the pre-launch boundary.

### 2.2 Auth-time continuity uses authenticated process ancestry, not foreground command naming

Once the fixed helper invocation has been delivered and the helper becomes foreground, the daemon MUST NOT re-derive shell family from `pane_current_command` as a condition of helper claim.

Instead, the daemon MUST prove continuity from the already-qualified pre-launch shell to the connecting helper using server-derived local process evidence:

```text
same exact delegated session
same exact authority epoch
same exact provider generation
same expected pane-shell PID
same stable pane-shell process identity
same pane TTY
same-user local peer credentials
exact approved helper executable identity
helper direct-parent PID = expected pane-shell PID
observed parent process identity = expected pane-shell process identity
helper is foreground on expected pane TTY
```

The helper does not supply or attest these continuity facts. They are derived by the daemon from the private local connection, OS process observation, and the pre-launch expectation.

A valid proof establishes that the foreground `shellbeam` process is the exact short-lived helper launched by the already-qualified shell; it does not redefine `shellbeam` as a shell family.

### 2.3 Fresh non-shell authority revalidation remains mandatory at claim time

Immediately before the durable helper-generation bind, the daemon MUST freshly revalidate:

```text
delegated binding still live
provider ref and provider generation unchanged
authority epoch unchanged
desired owner = agent
observed owner = agent
agent ingress writable
no ownership transfer active
privacy inactive
privacy release not pending
cwd still denotes the same directory object
```

The stored pre-launch shell runtime identity remains the shell identity bound into the context expectation. Its continuity is established by the authenticated parent-shell process proof from §2.2 rather than by a volatile foreground-command reprobe.

If provider generation, epoch, ownership, privacy, parent shell process identity, pane TTY, helper executable identity, direct ancestry, foreground proof, or cwd identity cannot be proved, claim fails and no child may spawn.

## 3. Internal shell-continuity proof contract

The verifier-to-claim-binder boundary must carry a typed, server-derived proof rather than a bare success/error bit.

Required internal semantic shape (Go names may vary only if the same fields and meanings remain explicit):

```go
type ShellContinuityProof struct {
    SessionID                  string
    AuthorityEpoch             delegatedsession.AuthorityEpoch
    ProviderGeneration         string
    ShellRuntimeIdentity       string
    PaneShellPID               int
    PaneShellProcessIdentity   string
    PaneTTY                    string
    HelperPID                  int
    HelperExecutableIdentity   string
    ForegroundProven           bool
    ObservedAt                 time.Time
}
```

Required semantics:

- `ShellRuntimeIdentity` is copied from the qualified pre-launch expectation, not inferred from the helper process name.
- `PaneShellProcessIdentity` is the stable OS process identity captured for the expected pane shell before helper delivery and re-observed during peer verification.
- `HelperPID`, local UID proof, executable identity, direct parent relation, and foreground status come from the accepted private local connection and host process observation.
- `ObservedAt` is bounded claim-time evidence and must be non-zero.
- The proof is internal authority material, not public API data and not a bearer capability.
- The helper cannot serialize, forge, replay, or provide the proof.
- Public state must not expose raw process identity, TTY, helper PID, or other private continuity metadata.

The existing one-generation private claim capability remains mandatory after peer continuity succeeds. This amendment does not replace challenge/response authentication.

## 4. Claim binding contract

The claim binder MUST bind all of the following atomically or fail closed:

```text
reserved request fingerprint
reserved helper generation
qualified pre-launch context expectation
authenticated shell-continuity proof
fresh non-shell authority snapshot
claim verifier digest
boundary observation time
```

The binder MUST verify that the continuity proof matches the reservation and the pre-launch expectation exactly:

```text
session_id
request authority_epoch
provider_generation
shell runtime identity
pane shell PID / stable process identity
pane TTY
expected helper executable identity
```

A mismatch is not recoverable by guessing a shell family from the helper process name.

The durable state may keep the existing non-secret helper binding and verifier digest. Raw peer credentials or process identities do not need to become reusable durable bearer material merely to satisfy this amendment.

## 5. CWD identity clarification

Task-8 native verification also exposed a macOS path-alias case:

```text
shell logical PWD: /var/folders/...
physical getcwd:    /private/var/folders/...
```

These spellings can denote the same directory object. Therefore, wherever the base design requires the cwd to be "exact" or "unchanged", equality means directory identity, not string identity.

Rules:

1. Lexically equal cleaned absolute paths are equal.
2. If spellings differ, both paths must resolve via local filesystem metadata and denote directories.
3. Equality then requires the operating system to prove they are the same filesystem object.
4. Stat/identity failure or different objects fails closed as `context_exec_boundary_unproven`.
5. The authoritative stored `CWDObserved` spelling may remain the pre-launch observation; accepting an alias does not authorize a cwd change.
6. No cwd path is added to the public projection by this clarification.

This permits aliases such as `/var/...` versus `/private/var/...` only when object identity is mechanically proved.

## 6. Failure semantics

At minimum:

- unqualified/unknown shell before helper delivery -> `context_exec_boundary_unproven`;
- provider generation or authority epoch drift -> `context_exec_stale_generation`;
- ownership/agent ingress drift -> `context_exec_not_agent_owned`;
- active/private/pending privacy -> `context_exec_privacy_blocked`;
- local peer credential, expected helper executable, direct-parent, parent-process-identity, or foreground-TTY verification failure before a continuity proof exists -> `context_helper_auth_failed`;
- an authenticated continuity proof whose provider generation or authority epoch does not match the reservation -> `context_exec_stale_generation`;
- an authenticated continuity proof whose shell runtime identity, pane-shell PID/process identity, or pane TTY does not match the stored pre-launch expectation -> `context_exec_boundary_unproven`;
- cwd object identity mismatch/unproven -> `context_exec_boundary_unproven`;
- durable claim mutation ambiguity -> `context_exec_ambiguous`.

No error/log/public diagnostic may contain environment values, pane transcript, private output, claim capability bytes, or secret-derived material.

## 7. Counterexample review

### Foreground helper changes `pane_current_command` to `shellbeam`

**Result:** allowed only after a qualified pre-launch shell boundary and only when local peer proof establishes exact helper executable, direct parent = expected pane shell, stable parent process identity, and foreground TTY. No shell-family reprobe occurs from the helper command name.

### A random same-user `shellbeam` process knows the opaque launch id

**Result:** rejected. It cannot satisfy exact private connection peer identity, expected direct parent, stable pane-shell parent identity, foreground TTY, and one-generation claim proof simultaneously.

### Nested shell appears before helper launch

**Result:** rejected by pre-launch shell qualification/identity continuity. The amendment does not bless nested or changed shells.

### Pane shell is replaced after pre-launch reservation

**Result:** rejected when parent PID/process identity or provider generation no longer matches the pre-launch expectation.

### Helper is launched from the right executable but by the wrong parent

**Result:** rejected by direct-parent proof.

### Helper is backgrounded or detached from the pane TTY

**Result:** rejected by foreground-TTY proof.

### Helper is valid but the authority epoch changes during authentication

**Result:** rejected by fresh claim-time authority revalidation.

### CWD spelling changes through an alias to the same directory object

**Result:** accepted only when local filesystem identity proves the same object.

### CWD changes to a different directory with a similar path

**Result:** rejected. Lexical similarity is irrelevant when object identity differs.

## 8. Public and protocol compatibility

This amendment requires no new public `local_shell` fields and no MCP/IPC schema change.

The helper command remains conceptually:

```text
shellbeam __context_exec_helper <opaque_launch_id>
```

The private helper challenge/response protocol remains one-generation and non-exportable. The amendment changes the internal server verifier/callback contract so successful peer verification yields typed continuity evidence for claim binding.

Any ShellBeam-internal runtime-routing metadata used to locate the private daemon endpoint remains non-bearer control metadata, belongs to the closed internal control namespace, and must be stripped before workload child exec. It is not claim authority and does not replace peer/challenge proof.

## 9. Required TDD and acceptance coverage

Implementation is not complete until tests prove all of these independently:

1. pre-launch unknown/changed shell still rejects before helper delivery;
2. a valid helper becoming foreground does not invalidate the already-qualified shell solely because `pane_current_command` becomes `shellbeam`;
3. shell-continuity proof requires exact helper executable identity;
4. shell-continuity proof requires direct parent = expected pane shell PID;
5. shell-continuity proof requires stable parent process identity;
6. shell-continuity proof requires foreground status on expected pane TTY;
7. claim binding rejects missing/mismatched/stale continuity proof before durable helper auth;
8. fresh provider generation/epoch/owner/privacy drift still rejects at claim time;
9. same-directory path aliases pass only with filesystem object identity;
10. different cwd objects fail closed;
11. native Darwin zsh post-secret workflow reaches helper authentication/child execution without shell-family false negatives;
12. anti-leak tests prove the new proof does not appear in public response/state/logs;
13. same request replay remains exactly once and changed request remains conflict;
14. existing helper executable/path, timeout/output, crash-window, resource/hermetic truthfulness gates remain unchanged.

## 10. Non-goals

This amendment does not:

- make `shellbeam` a supported shell family;
- trust `pane_current_command=shellbeam` as proof of anything;
- weaken pre-launch shell qualification;
- allow arbitrary nested/replaced shell state;
- make process ancestry a public or reusable bearer credential;
- make cwd lexical spelling authoritative over filesystem object identity;
- change public `context.exec` request/response schemas;
- change evidence authority beyond the already-approved `context_exec_child_owned_v1` contract;
- add TTY child mode, stdin streaming, arbitrary env overrides, or resident helpers.

## 11. Implementation sequencing after human review

After the exact committed amendment is approved, the implementation plan should minimally sequence:

1. introduce the typed internal shell-continuity proof and make the host peer verifier produce it;
2. thread the proof through server authentication into claim binding;
3. split claim-time fresh authority revalidation from volatile shell-family probing while preserving all non-shell checks;
4. bind proof fields against the stored pre-launch expectation and fail closed on mismatch;
5. retain/finish directory-object CWD equality at adapter and app revalidation boundaries;
6. rerun the native post-secret acceptance until stable without diagnostic-only instrumentation;
7. remove temporary diagnostics;
8. resume the remaining Task-8 integration matrix, evidence document, stable gate, race gate, `devctl check`, commit gate, and final commit.

No implementation step in this sequence is authorized by this authored document alone. Human review of the exact committed amendment is required first.

## 12. Design self-review criteria

Before requesting human review, the authored amendment must contain no unresolved implementation placeholders, must not contradict the base design's reserve-before-launch or one-generation claim rules, must preserve anti-leak requirements, and must keep all public interfaces unchanged unless a separate explicit amendment approves otherwise.
