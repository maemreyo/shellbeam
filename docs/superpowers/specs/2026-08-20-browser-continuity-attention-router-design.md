# Browser Continuity Attention Router Design

**Status:** cross-system architecture freeze proposed; awaiting review. No implementation approved. P0 implementation planning may proceed only after this document is approved.

**Execution/design base:** `55d6b4fc8ce897e43e05de199f7d1f748a8d21a8` (`main`, 2026-08-20)

**Scope:** a Firefox extension that observes multiple concurrent ChatGPT Web conversations, classifies why each one stopped, correlates each one against attributed ShellBeam machine facts through a least-privilege read-only native bridge, records what needs human attention, and actuates conversation mutations **only** on an explicit human gesture. Autonomous continuation is explicitly out of P0 and gated behind a measured graduation criterion.

**Companion designs:**

- [ShellBeam V1 Design](./2026-08-13-shellbeam-v1-design.md)
- [Machine Truth Harness Architecture](./2026-08-18-machine-truth-harness-architecture-design.md)
- [Affected Surface / Verification / Evidence Sufficiency](./2026-08-18-affected-surface-verification-evidence-sufficiency-design.md)
- [Multilanguage Structured Results](./2026-08-19-multilanguage-structured-results-design.md)
- [Persistent Runtime and Named Sessions](./2026-08-16-persistent-runtime-named-sessions-design.md)
- Human–Agent Interactive Session Handoff (not on `main`; integration deferred to P2)

---

## 1. Purpose

A ChatGPT Web conversation driving ShellBeam can stop for many reasons: the model finished, the response hit a length cutoff, the stream broke, the account hit a rate limit, the model refused, or a local command is still running and the model is waiting. From the browser alone these are largely indistinguishable, and a human running eight such conversations in parallel has no way to know which of them needs attention without visiting each tab.

This subsystem answers one question per conversation:

```text
does this conversation need me, and why?
```

It does not answer:

```text
is the user's task complete?
```

ShellBeam already refuses to answer the second question. `tests/contract/verification_truth_boundary_test.go` and the schema/MCP/CLI equivalents prove no production surface exposes `task_complete`, `work_complete`, or `safe_to_finish`. This design extends that refusal to the browser: no component of this subsystem may assert user-task completion, and no machine fact may be used to overrule a human's completion judgment.

## 2. Decision summary

A Firefox extension SHALL own an **Attention Router**: per-conversation watch state, single-writer controller authority, observation freshness fencing, a qualified DOM adapter, a stop-cause classifier, a durable attention ledger with human adjudication, and an actuator that runs **only** on human gesture.

ShellBeam SHALL expose a new **Browser Bridge Protocol v1** through a connectionless native-messaging host with a closed set of read-only verbs. The host is a privilege membrane, not an SDK.

Autonomous continuation SHALL NOT exist in P0. It becomes designable only after the P0 ledger produces adjudicated evidence meeting a graduation gate defined in §28.

ShellBeam core SHALL NOT change for P0. No new projection, no new aggregate root, no new durable identity. The additions are one command surface, one protocol, one installer step, and conformance fixtures.

## 3. The three-authority model

This is the organizing principle of the whole subsystem. Every design decision below derives from it.

```text
                       HUMAN AUTHORITY
        semantic task meaning; in P0, the sole source of
                 conversation mutation authority
                              │
              ┌───────────────┴───────────────┐
              ▼                               ▼
     BROWSER AUTHORITY                  ADJUDICATION
  observable agent/UI state,        ground truth about what
  watch identity, controller        actually happened, fed
  ownership, actuation                back as labeled data
              │
              │ read-only, attributed
              ▼
    SHELLBEAM AUTHORITY
  attributed machine facts: operations, sessions,
  events, verification counts, structured results
```

Normative consequences:

1. The browser MAY say *"generation stopped"*. It MAY NOT say *"work is finished"*.
2. ShellBeam MAY say *"one obligation is blocking"*. It MAY NOT say *"the task is incomplete"*, and it MAY NOT be used to overrule a human completion claim.
3. The stop-cause classifier MAY say *"this looks like a rate limit"*. It MAY NOT authorize a mutation.
4. In P0, only a human gesture authorizes a conversation mutation.

A previous iteration of this design proposed that a completion marker plus a verification contradiction should suppress a completion notification. That is rejected: it is the `task_complete` invariant with the sign flipped, and it makes machine facts adjudicate a semantic claim.

## 4. Problem scenarios

**S1 — Length cutoff.** The model is mid-implementation and the response ends at a token limit. A new turn is needed. Today the human must notice and click.

**S2 — Waiting on local work.** The model started a 20-minute test run and stopped its turn. Sending a new turn now is wasteful or harmful; the correct action is to wait for the operation to reach a terminal state.

**S3 — Rate limit.** The account hit a quota wall. Sending another turn burns quota and produces error turns. The correct action is to tell the human and back off.

**S4 — Silent stall.** The model reruns the same failing test suite every turn, producing new receipts each time while making no observable advancement on that surface.

**S5 — Duplicate tabs.** The same conversation is open in two tabs. Both observe "stopped". Uncoordinated, both would act.

**S6 — Cross-conversation contamination.** Conversation A's test failure is attributed to conversation B, and B is told to fix a failure it never caused.

**S7 — Stale takeover.** The controller tab dies. Another tab holding a minutes-old view of the same conversation takes over, sees "stopped and incomplete", and acts on a stale observation with perfectly valid authority.

**S8 — Adapter drift.** ChatGPT ships a UI change. Selectors silently match the wrong element and the system invents facts.

**S9 — Human interleaving.** The human types a follow-up while a watch is armed. The system must not adopt that turn into an automated chain.

**S10 — False belief.** Everything quietly stopped overnight and the human believed automation was running. This is strictly worse than having no automation.

## 5. Goals

1. Per-conversation isolation, with no global state authorizing per-conversation action.
2. Exactly one writer per conversation at any time, with fencing that survives background termination.
3. No action on a stale observation, even under valid authority.
4. Honest degradation: unknown is a first-class outcome everywhere, and never rendered as a negative fact.
5. Attribution-safe enrichment: ShellBeam facts reach a conversation only when attributable to it.
6. A durable, enumerable record of everything that needed attention, surviving browser restart.
7. Adjudicated ground truth sufficient to decide whether autonomous continuation is justified.
8. Zero ShellBeam core change; zero new ShellBeam authority.

## 6. Non-goals

This design does NOT authorize:

- autonomous conversation mutation in P0;
- ShellBeam driving, scripting, or scraping any browser;
- ShellBeam storing conversation content, transcripts, or conversation identity;
- a generic browser-automation DSL, selector/action rule engine, or workflow language;
- a generic ShellBeam action passthrough reachable from the browser;
- any new durable ShellBeam aggregate root (`Task`, `Change`, `envelope_id`);
- any user-task completion claim from any component;
- resuming automation after a browser restart;
- treating any ShellBeam fact as authority over semantic completion;
- adapters extending the core vocabulary.

## 7. Terminology

```text
WatchTask            browser-owned unit of watching; authoritative identity of
                     "what this tab is being watched for"
watch_task_id        created at arm time; never derived from conversation or activity
conversation_key     best-effort remote conversation identity; LATE-BOUND
controller           the single tab permitted to mutate a conversation
controller_generation monotonic fencing counter for controller authority
observation_epoch    fencing counter for freshness of the browser's view
adapter              maps one web product's DOM into the closed core vocabulary
qualified_feature    an adapter capability positively verified at bootstrap
stop_cause           why generation ended (may be unknown)
cause_quality        how the cause was known (explicit / inferred / unavailable)
attention record     durable, authority-free observation that a human may need to act
adjudication         a human verdict attached to an attention record
actuator             the code path that mutates a conversation
Browser Bridge       the ShellBeam-owned read-only native-messaging protocol
browser host         short-lived native process implementing the bridge
```

## 8. Alternatives considered

**A. Pure DOM userscript, no ShellBeam.** Cannot distinguish S2 from S1, cannot see S4 at all, and cannot attribute anything. Rejected as insufficient, though it remains the honest fallback whenever ShellBeam enrichment is unavailable.

**B. ShellBeam drives Firefox (Playwright / CDP / AppleScript).** Inverts the dependency so that machine-execution infrastructure depends on a private, unowned DOM. Also contradicts [Machine Truth Harness §15](./2026-08-18-machine-truth-harness-architecture-design.md), which lists a custom browser engine as an explicit non-goal. Rejected.

**C. Localhost HTTP endpoint for a userscript.** Contradicts [docs/security.md](../../security.md): the daemon is reachable only through an owner-only Unix socket with peer-UID checking and there is deliberately no loopback TCP listener. Rejected.

**D. Persistent native port (`runtime.connectNative`).** Requires request multiplexing, a framed stdout writer, reconnection logic, and per-port lifetime coupled to background lifetime. Rejected for P0 in favour of connectionless `runtime.sendNativeMessage`; it may be revisited only if measurement proves process spawn cost dominates.

**E. UID-wide liveness as a continuation gate.** Rejected: with several conversations running, `live_sessions > 0` from another conversation would block every other conversation. This is cross-conversation head-of-line blocking, and it is the reason for invariant I10 in §30.

**F. Autonomous continuation in P0.** Rejected: the two components that would authorize it — stop-cause classification and attribution — have never been measured on real workload. P0 exists to measure them.

## 9. High-level architecture

```text
   ChatGPT tab            ChatGPT tab            ChatGPT tab
       │                      │                      │
  content script         content script         content script
  (untrusted obs.)       (untrusted obs.)       (untrusted obs.)
       └──────────────────────┼──────────────────────┘
                              ▼
              ┌───────────────────────────────┐
              │   Extension background        │
              │                               │
              │  WatchTask authority          │
              │  controller lease + fencing   │
              │  observation epochs           │
              │  stop-cause classifier        │
              │  correlation classifier       │
              │  admission / rate limiting    │
              │  Attention Ledger             │
              │  actuator (human-gated)       │
              └───────────────┬───────────────┘
                              │ sendNativeMessage
                              │ fixed verbs, bounded responses
                              ▼
              ┌───────────────────────────────┐
              │  shellbeam browser host       │
              │  short-lived, stateless       │
              │  hardcoded verb → IPC mapping │
              └───────────────┬───────────────┘
                              │ owner-only unix socket, peer UID
                              ▼
                      ShellBeam daemon
```

Content scripts produce observations and never hold authority. The background holds all authority and all state. The host holds no state at all.

## 10. Identity model

Four layers, with causality respected. An earlier draft placed `conversation_key` above `watch_task_id`; that was wrong, because a new chat has no conversation identity until after its first turn.

```text
watch_task_id            created at ARM; authoritative; never derived
    │
    ├── controller binding      (tab, controller_generation)
    ├── observation_epoch
    ├── conversation_key?       LATE-BOUND, best effort
    └── shellbeam correlation?  OPTIONAL, best effort, may be absent/wrong
```

`watch_task_id` SHALL NOT equal, derive from, or be derived by `activity_id`. Per [Machine Truth Harness §6](./2026-08-18-machine-truth-harness-architecture-design.md), `activity_id` is deliberately semantically weak and explicitly does not mean one user task or one unit of completion. Treating it as a task handle would import a meaning ShellBeam refuses to provide.

**Late binding and collision.** When `conversation_key` resolves, the background SHALL reconcile it against all active WatchTasks:

```text
conversation_key resolves for W
    ↓
scan active WatchTasks under a GLOBAL transition lock
    ↓
no existing controller for that key      → W binds and becomes controller
existing live controller for that key    → W is demoted to observer
existing controller fails liveness check → takeover protocol (§11), never overwrite
```

The rule is *existing valid controller wins*, never last-writer-wins.

**Liveness is a ping, not a tab record.** `tabs.get()` proves only that the browser still has a tab record. Firefox can discard a tab, leaving the record present and the content script dead. Controller liveness SHALL be established by a ping to the content script with a timeout, returning `watch_task_id`, `document_epoch`, `conversation_key?`, and `observation_epoch`.

## 11. Authority and freshness are orthogonal

Two fencing dimensions, both required for any mutation:

```text
controller_generation   proves the actor currently holds authority
observation_epoch       proves the information the actor is acting on is fresh
```

`controller_generation` alone is insufficient. Scenario S7 is a valid generation acting on a stale view. Therefore takeover SHALL be:

```text
controller ping fails
    ↓
lease candidate
    ↓
controller_generation++            (authority acquired)
    ↓
RESYNC_REQUIRED                    (mutation authority NOT yet granted)
    ↓
forced reload of the conversation
    ↓
adapter bootstrap + qualification (§12)
    ↓
conversation identity confirmation
    ↓
observation_epoch established
    ↓
ARMED
```

There SHALL be no edge from lease acquisition directly to actuation.

**Native-continue eligibility is ephemeral and SHALL NOT be reconstructed from persisted state.** The server persists the assistant message; the "Continue generating" affordance is client state and does not survive a reload. Therefore a controller that took over after a crash can never use it. This forces two distinct actuation paths with different preconditions (§17), not one generic mutation node.

## 12. Browser adapter qualification

The adapter is the only component qualified against a moving, unowned target. It is therefore the component most likely to be silently wrong, and it SHALL be treated the way ShellBeam treats providers: absence degrades honestly rather than causing invented facts ([Machine Truth Harness §16.15](./2026-08-18-machine-truth-harness-architecture-design.md)).

At every bootstrap the adapter SHALL produce a capability matrix, not a boolean:

```text
adapter_id              chatgpt-web
adapter_version         1
fingerprint             <hash of qualified selector shapes>

message_container       qualified
assistant_last_message  qualified
composer                qualified
send_action             qualified
generation_indicator    qualified
native_continue         unavailable
rate_limit_signal       drifted
network_error_signal    qualified
```

Qualification is **per feature** and re-verified at each bootstrap. Global fingerprint invalidation is rejected as too brittle: a cosmetic change to one banner must not disarm every watch.

Every observation SHALL carry provenance. An unqualified feature yields `unknown` with a reason, never a negative fact:

```text
rate_limit:
    value  = unknown
    reason = feature_unqualified
```

`rate_limit = false` from an unqualified feature is a specification violation.

Feature drift SHALL affect only consumers of that feature. A watch enters a stop state only when the operation it is currently attempting depends on a drifted or unavailable feature. Example: `rate_limit_signal` drifts while `composer` and `send_action` remain qualified — observation continues, human-triggered continuation remains available, and rate-limit classification becomes `unknown`.

## 13. The core vocabulary is closed

```text
states        observing, generating, stopped, resyncing, armed,
              attention_required, disarmed
causes        normal_completion, cutoff, stream_error, rate_limit,
              refusal, content_filter, unknown
qualities     explicit, inferred, unavailable
actions       native_continue, send_continuation
events        manual_override, adapter_drift, controller_lost,
              binding_ambiguous, budget_exhausted, resync_failed
```

Adapters map external observations **into** this vocabulary. An adapter SHALL NOT introduce states, causes, actions, or authority semantics. A new term requires a core vocabulary change, invariant review, and a protocol version bump — never an adapter-local extension.

This closes the extension point through which plugin architectures usually erode into rule engines.

## 14. Stop cause and cause quality are separate axes

`indeterminate` is not a cause. Collapsing it into the cause enum hides an absence inside a claim.

```text
stop_cause     normal_completion | cutoff | stream_error |
               rate_limit | refusal | content_filter | unknown
cause_quality  explicit | inferred | unavailable
```

`stop_cause = unknown` with `cause_quality = unavailable` is an ordinary, expected outcome, and is expected to be the plurality outcome in practice. Distinguishing "the model finished" from "the model was cut off" has no reliable DOM signal in the general case.

The product therefore SHALL NOT depend on classifier sharpness. Time since stop, attribution quality, browser state, and machine facts are the substrate; the classifier is enrichment. A card reading

```text
Chat 5 stopped 18 min ago
Cause: unknown (unavailable)

Machine context:
  pytest: 2 failing
  comparable failure signature repeated: 3 runs
  last attributed ShellBeam event: 17 min ago
```

is expected to be among the most useful outputs of the system, and its usefulness comes entirely from the substrate.

## 15. Extension runtime state is MV3-safe by construction

Firefox Manifest V3 supports only non-persistent background scripts, and background scripts unload after a few seconds of inactivity. Globals do not survive; `setTimeout` is not a durable timer. A watchdog spends nearly all of its life waiting, which is precisely the condition that triggers unload.

Therefore:

```text
storage.session      watch runtime authority
                       controller, controller_generation
                       observation_epoch, state
                       conversation binding
                       shellbeam correlation
                       deadlines, cursors, retry state
                       per-watch last_native_read_at

storage.local        ZERO authority
                       Attention Ledger (durable, survives restart)
                       prompt templates, grace defaults, preferences
```

`storage.session` is correct for authority: it survives background unload and is cleared when the browser closes. That is exactly the desired lifetime, because a controller lease must not outlive the browser process that held it. Resetting fencing counters after a browser restart is safe, since every possible stale callback died with the process. `storage.session` is also not exposed to content scripts by default, which matches the trust boundary.

**Timers.** Long waits SHALL use `alarms`, never `setTimeout`. The background writes an authoritative deadline into `storage.session`, sets an alarm, and may unload. On wake it re-reads the record and compares against its own recorded deadline; the alarm is a wake signal, never the source of truth. Firefox has no documented minimum clamp on `delayInMinutes` (Chrome fires no more than every 30 seconds and warns below 0.5 minutes), so short grace periods are plausible on Firefox and SHALL be verified empirically. Alarm delay reduces throughput; it cannot affect correctness, because every decision revalidates against stored deadlines.

**One record per watch.** `storage.session` writes are not transactional across keys. State SHALL be a single record per watch, written whole:

```text
watch:<watch_task_id> = {
    schema_version, revision,
    controller, controller_generation,
    observation_epoch, state,
    conversation_binding, shellbeam_binding,
    deadlines, retry_state, event_cursor,
    last_native_read_at
}
```

A transition is: read record → validate → construct the entire next record → one `set`. Multi-key writes are forbidden, because a crash between two `set` calls leaves torn state that no serializer can repair.

No secondary index (`conversation_key → watch_task_id`) SHALL be maintained in P0. A bounded scan over active watches at late-bind time is trivially cheap at the intended scale and avoids a torn index.

## 16. Serialization model

The background is the only writer of authority state. Content scripts, the host, and any popup/options page SHALL NOT write it. This gives a single logical writer, so compare-and-swap **semantics** are required while a CAS storage **primitive** is not.

Two lock scopes are required, and conflating them is a bug in either direction:

```text
withWatchLock(watch_task_id)      per-watch transitions
                                  state changes, deadlines, cursors,
                                  retry accounting

withGlobalLock()                  cross-record transitions ONLY
                                  conversation late-binding
                                  controller collision reconciliation
                                  controller takeover
                                  native read admission
```

A per-watch lock does not protect conversation late-binding: that transition reads every record and claims a shared resource. Two tabs late-binding to the same conversation concurrently would each scan clean and each claim. Conversely, taking the global lock for ordinary per-watch transitions would reintroduce head-of-line blocking.

If the background dies mid-transition, no `set` has occurred and the old record stands; if it dies after the `set`, the new record stands. Every wake re-reads. There is never a second concurrent background incarnation competing to write.

## 17. Human-controlled actuator

P0 has exactly one mutation authority: a human gesture. The safety property is **zero autonomous mutation**, not zero mutation code.

Building the actuator in P0 under human authority is deliberate. The actuator is the easy part; the decision is the hard part. By the time P1 is considered, the composer targeting, send path, conversation identity confirmation, stale-tab resync, controller fencing, native-continue path, drift handling, and multi-tab collision handling will have been exercised hundreds of times against real conversations. P1 then changes only *who authorizes*, not *what executes*.

A human gesture bypasses **decision policy**. It SHALL NOT bypass **mechanical safety**:

```text
human clicks Continue
    ↓
acquire controller under global lock
    ↓
reload the watch record
    ↓
conversation identity still matches?
    ↓
observation fresh (observation_epoch current)?
    ↓
required adapter features qualified?
    ↓
actuate
```

Any failed precondition yields a refusal with a remediation, not a best-effort attempt:

```text
Cannot safely act on this tab: observation is stale.
Reload / re-arm required.
```

Two distinct actuation paths, with different preconditions:

```text
NATIVE_CONTINUE      requires: same controller, same document epoch,
                     native_continue qualified and directly observed
SEND_CONTINUATION    requires: composer + send_action qualified,
                     conversation identity confirmed, fresh observation
```

**Manual override.** Automation turn provenance is P0 and requires no model cooperation:

```text
background prepares outbound     → outbound_turn_id = T17, state = prepared
content script confirms the outbound user message appeared → T17 committed
assistant response is bound to that lineage
```

A user message appearing with no corresponding prepared outbound is `manual_override`. The watch SHALL NOT adopt that turn into an automated lineage; it records attention and disarms. The browser has no authority to decide whether a human follow-up is the same logical task or a new one, so it does not guess.

Because `manual_override` is a frequent and entirely normal human behaviour, re-arming SHALL be a single gesture, and the attention record SHALL state plainly that the watch stopped because the human typed. A correct-but-annoying rule that users disable is worse than the rule itself.

**Model-visible completion nonce is deferred to P1.** In P0 a completion marker is a stop hint only, and a stop hint removes authority rather than granting it. Accepting a marker requires only that it is the last non-whitespace token of the latest assistant message and that it is not inside a code or preformatted element — a DOM check, not a text regex, which is what defeats the model quoting the instruction back. A false positive costs one unnecessary attention record and no external effect. Absence of a marker is not evidence of anything.

## 18. Attention Ledger

The ledger is the product output. Every path in the system terminates in an attention record, so if the ledger is weak, the system is weak regardless of how sound its authority model is.

Failure mode S10 is the one being closed: transient notifications across many conversations overnight produce a human who believes automation is running while everything has stopped. A durable, enumerable ledger with a badge count is the only defence.

```text
attention_id
watch_task_id
conversation_key?
created_at
severity
reason            stop_detected | rate_limited | binding_ambiguous |
                  adapter_feature_drift | repeated_failure_surface |
                  manual_override | retry_budget_exhausted |
                  resync_failed | attribution_lost

observation:
    stop_cause, cause_quality
    adapter facts + per-feature qualification
    shellbeam facts (literal, bounded)
    attribution quality
    coverage

acknowledged_at?
resolved_at?

adjudication?:
    verdict        correct | wrong | unclear
    actual_cause?  normal_completion | cutoff | stream_error |
                   rate_limit | refusal | content_filter | other | unknown
    adjudicated_at
```

The ledger lives in `storage.local` and carries **zero execution authority**. It survives a browser restart precisely so the human can read what happened overnight; it can never resume automation by itself.

**Observation and adjudication SHALL remain separate fields.** Observation is what the system believed. Adjudication is what the human later said. Overwriting the former with the latter destroys the only dataset that can falsify the classifier.

Reported metrics SHALL always show the denominator:

```text
classified_records     428
adjudicated_records    117
adjudication_coverage  27.3%
accuracy (adjudicated) ...
```

Reporting accuracy without coverage is forbidden. Reported metrics SHALL also carry an explicit selection-bias caveat: humans preferentially adjudicate surprising cases, so the adjudicated subset is not assumed representative.

## 19. Browser Bridge Protocol v1

Connectionless. One request, one bounded response, one short-lived process.

```text
hello
activity_facts            { correlation_id }
activity_events           { correlation_id, cursor? }
verification_facts        { correlation_id }
structured_failure_facts  { correlation_id }
```

There is no `action` field, no `argv`, no `command`, no `cwd`, no path, no caller-chosen session id, and no generic passthrough anywhere in the protocol. Verbs are a closed enum; the host maps each to a hardcoded ShellBeam request. `internal/app/bridge.Handler` SHALL NOT be reused, because it forwards a caller-supplied action and would hand the browser the entire ShellBeam surface including execution.

**Responses are literal facts, never judgments.** The host returns counts and provenance; the extension classifies:

```text
{
  "activity_found": true,
  "latest_operation_at": "...",
  "operation_count_retained": 64,
  "compacted_operations": 12,
  "event_cursor_continuity": "continuous",
  "sessions": { "running": 1, "finalizing": 0 },
  "verification": { "satisfied": 7, "waived": 0,
                    "blocking": 1, "indeterminate": 2 },
  "coverage": { "historical": "partial", "session_scope": "activity" }
}
```

A field named `mechanical_blockers` — or any array whose name encodes a policy judgment — is forbidden. `blocking: 1` is a literal count under the effective policy; calling it a blocker is the extension's policy, not ShellBeam's fact. This follows [Machine Truth Harness §16](./2026-08-18-machine-truth-harness-architecture-design.md) invariants 4 and 13: policy gaps are advisory, and projections preserve authority, freshness, and coverage rather than flattening uncertainty.

**Bounds.** Native messaging caps a message from the application to the extension at 1 MB. That is a transport failure threshold, not an API budget. The host SHALL target ≤ 64 KiB and enforce a hard bound of ≤ 256 KiB, truncating with explicit truncation and coverage markers, never silently. `inspect.events` already supports `max_events` and a cursor, so pagination is natural.

**Negotiation and three distinct degradations.** `hello` SHALL distinguish outcomes whose remediations differ:

```text
host absent / manifest missing      → install the browser host
host present, daemon unreachable    → start or repair the daemon
protocol version incompatible       → update host or extension
```

All three degrade to *ShellBeam enrichment unavailable*. None disables the Attention Router: the browser-only substrate remains useful, and this is the honest fallback identified in alternative A.

## 20. ShellBeam host responsibilities

The host is a security membrane, not a convenience library. It:

1. reads exactly one JSON message from stdin, validates it against the closed verb enum, and rejects anything else;
2. maps the verb to a hardcoded IPC request;
3. bounds and truncates the response;
4. writes one JSON message and exits.

It holds no cursors, no watch state, no conversation identity, and no session. It never receives or stores conversation content. Statelessness is not merely hygiene here: because a connectionless host cannot hold state, cursors necessarily live in the extension, which is where they belong.

[`internal/adapter/ipc`](../../../internal/adapter/ipc/client_unix.go) speaks HTTP over the Unix socket with a pooled `http.Client`, so concurrent use is safe and no serialization is needed in the host.

**Rate limiting has exactly one possible home, and it is the extension.** The daemon has no read-path budget: [`internal/app/daemon/budget.go`](../../../internal/app/daemon/budget.go) gates `AcquireStart` and output bytes only, and the sole concurrency limiter is `mediaSlots` for media reads. A stateless host cannot rate-limit either. Therefore the background SHALL enforce admission:

```text
global_native_reads_inflight   <= N        (starting default 2)
per_watch_native_min_interval  >= X ms     (starting default 1000–2000)
```

Final values are set by benchmark qualification, not frozen here. Per-watch `last_native_read_at` and pending deadlines SHALL persist in `storage.session` so a wake does not burst every watch at once. Deterministic jitter (`deadline + hash(watch_task_id) % window`) SHALL spread post-sleep wakes.

**Admission ordering is normative:**

```text
acquire global native slot
    ↓
reload the watch record
    ↓
revalidate controller_generation, observation_epoch, deadline, binding
    ↓
issue the native read immediately
```

Validating before queueing is a defect: with a small slot count and many watches, a request can wait long enough that its assumptions expire while queued. Revalidating after acquisition lets an expired request be rejected or replanned instead of executed on stale premises.

## 21. Native messaging install boundary

The native manifest grants a browser extension a channel to a same-UID process. That is a real privilege delegation and SHALL be explicit, separate, and revocable.

1. `shellbeam install` SHALL continue to manage only the daemon. It SHALL NOT write a native messaging manifest. Installing ShellBeam must never silently grant a browser extension access to machine facts.
2. A separate, explicitly invoked step installs the manifest, and a matching step removes it.
3. The manifest SHALL pin `allowed_extensions` to one extension ID. The extension therefore SHALL declare a fixed `browser_specific_settings.gecko.id`, and the installer writes exactly that ID.
4. `doctor` SHALL report manifest presence, the pinned extension ID, and the protocol version, so the three degradations in §19 are diagnosable locally.

The extension is not same-UID-privileged; the host is. The host is the only place least privilege can be enforced, which is why its verb set is closed and why it has no passthrough.

## 22. Correlation semantics

`activity_id` correlation is **optional, best-effort, and may be absent, stale, or wrong**. It is a model-supplied field ([`internal/adapter/mcp/input.go:39`](../../../internal/adapter/mcp/input.go:39)), so a model may simply omit it, and `inspect.activity` then fails with `ActivityNotFound` ([`internal/app/activity/service.go:38`](../../../internal/app/activity/service.go:38)).

`ActivityNotFound` SHALL map to `facts_unavailable`. It SHALL NOT map to idle, settled, or complete.

The extension classifies correlation quality from literal host facts:

```text
unbound      no binding declared
declared     binding declared in the transcript
current      activity exists AND was updated within the current turn window
stale        activity exists but its latest attributed operation predates the window
ambiguous    conflicting bindings observed for the same watch
unavailable  host, daemon, or protocol unavailable
```

Existence alone is insufficient for `current`: an activity persists forever after a single operation, so recency relative to the turn boundary is required.

**Coverage loss SHALL be surfaced.** `Activity` retains at most `MaxOperationHistory = 64` operation refs and counts the rest in `CompactedOperations` ([`internal/core/activity/activity.go`](../../../internal/core/activity/activity.go)). `compacted_operations > 0` SHALL render as `historical_coverage = partial`. Sixty-four retained operations SHALL NOT be presented as a complete history.

**`ambiguous` has teeth and no probe.** Ambiguity yields an attention record and disarms. It SHALL NOT trigger an automated disambiguation message: sending one would be a conversation mutation performed under the very ambiguity being resolved. The human re-arms after reading the record.

**No cross-conversation contamination.** Only facts attributable to a watch's own correlation may enrich it. Global queries such as "all failing tests on this machine" SHALL NOT enrich any conversation. Insufficient attribution yields `facts_unavailable` and a fallback to browser-only substrate. This is fail-open on automation availability and fail-closed on attribution.

## 23. Comparable failure surface

Repeated failure detection is where ShellBeam contributes something no browser-only system can. It requires strict comparability, because a naive comparison silently compares different things.

```text
ComparableFailureSurface {
    producer_identity
    structured_adapter
    authority
    derivation_method
    invocation_qualification
    test_surface_signature      # from TestCase{Name, Package, Status}
                                # and Diagnostic{Code, Location}
}
```

Records carry `Authority` and `DerivationMethod` individually ([`internal/core/structuredresult/record.go`](../../../internal/core/structuredresult/record.go)), so comparison SHALL be authority-homogeneous and producer-homogeneous. `pytest tests/a` and `pytest tests/` are not comparable merely because their failing sets coincide; invocation qualification (see `27207d9 docs: bind pytest invocation qualification authority`) is the existing hook that makes this mechanical.

Outcomes:

```text
same_signature
different_signature
incomparable
insufficient_coverage
```

`incomparable` SHALL NOT collapse into either progress or no-advancement.

The counter is named `repeat_failure_signature_count`, never `no_progress_count`. Between two identical failing surfaces the model may have refactored, fixed other tests, or found the root cause. The literal claim is:

```text
Comparable failure signature repeated 3 times
```

never:

```text
Agent is stuck
```

New receipts SHALL NOT reset any budget. A repeatedly failing suite produces new receipts every turn while advancing nothing, so receipt novelty is not advancement.

## 24. Budgets

Budgets exist in P0 to bound human-triggered retry loops and to feed graduation metrics; they gain decision authority only in P1.

```text
absolute_action_budget                     never reset by assistant output
consecutive_without_attributable_progress  attributable machine progress only
browser_only_budget                        stricter, used when correlation
                                           is unavailable
```

Attributable machine progress means a new attributed operation receipt, an attributed event beyond the cursor, an attributed session state transition, a new attributed evidence record, or an attributed verification state change. When correlation is unavailable, machine progress is `unknown` and SHALL NOT be invented; the stricter browser-only budget applies.

Digest similarity SHALL NOT be used to detect stalling. Legitimate progress often reads near-identically (`"Tests 1–20 passed; proceeding"`, `"Tests 21–40 passed; proceeding"`), and similarity would classify real advancement as a stall.

## 25. Degradation taxonomy

Every degradation has one honest rendering and one remediation. None invents a fact, and none silently disables the system.

```text
host absent                  → enrichment unavailable; install browser host
daemon unreachable           → enrichment unavailable; check daemon
protocol incompatible        → enrichment unavailable; update component
activity not found           → facts_unavailable
activity stale               → correlation stale; facts shown with age
history compacted            → historical_coverage partial
adapter feature drifted      → that feature unknown; dependents refuse
adapter fully unqualified    → attention record; watch disarmed
controller lost              → takeover protocol, then RESYNC_REQUIRED
observation stale            → refuse action; require resync
binding ambiguous            → attention record; watch disarmed
response oversized           → truncated with explicit markers
```

## 26. Repository split and protocol ownership

The extension SHALL live in its own repository. The split follows the trust boundary, not the language boundary: one side is a privilege bridge over machine truth with a slow, deliberate release cadence; the other faces a moving, unowned DOM and needs to ship fast.

```text
Extension  ──depends on──▶  Browser Bridge Protocol v1
ShellBeam  ──implements──▶  Browser Bridge Protocol v1
```

The extension SHALL NOT depend on ShellBeam internal types. A monorepo would place them within import distance of each other, and the coupling would arrive gradually and be hard to reverse.

**This architecture spec is owned by the ShellBeam repository**, because the host is the privilege boundary and the protocol schema needs exactly one owner. After approval it produces two implementation plans:

```text
ShellBeam repo    browser host, fixed verbs, protocol schema,
                  manifest installer, doctor integration,
                  conformance fixtures
Extension repo    Firefox extension, ChatGPT adapter, WatchTask runtime,
                  Attention Ledger UI, adjudication, human-gated actuator
```

**Contract synchronization.** The extension pins fixtures copied from a *released* protocol version and never tracks `main`. ShellBeam CI carries conformance fixtures for valid requests, invalid requests, unknown verbs, cross-verb field rejection, response bounds, and oversized-response truncation. Scheduled cross-repo integration is permitted; PR correctness in either repo SHALL NOT depend on a network checkout of the other.

## 27. P0 scope

**P0 — Attention Router with Human-Controlled Actions.**

Included:

1. WatchTask identity and per-watch single-writer authority with `controller_generation`.
2. Observation-epoch fencing, with forced resync on takeover.
3. MV3-safe state: one record per watch in `storage.session`; `alarms` for deadlines.
4. ChatGPT adapter with per-feature bootstrap qualification and drift semantics.
5. Closed core vocabulary.
6. Stop-cause classification with separate cause quality; `unknown` first-class.
7. Late-bound conversation binding with global-lock collision reconciliation.
8. Automation turn provenance and `manual_override` detection.
9. Browser Bridge Protocol v1: connectionless host, fixed read verbs, bounded literal responses, three-way degradation.
10. Optional correlation with `facts_unavailable`, `stale`, `ambiguous`, and coverage loss all first-class.
11. Comparable failure surface with strict comparability.
12. Attention Ledger in `storage.local` with adjudication, badge, and inbox.
13. Human-gated actuator with full mechanical precondition checks.
14. Explicit, revocable native manifest install, separate from `shellbeam install`.

Excluded from P0: autonomous mutation of any kind; model-visible completion nonce; handoff integration; durable resume across browser restart; any task envelope; any ShellBeam core change.

## 28. Graduation gate to P1

P0 is instrumentation. Observer-only alone cannot produce this dataset: if nothing ever acts on a classification, nothing ever falsifies it, and three weeks of data would show only what the classifier said, never whether it was right. Human adjudication and human-triggered actuation are what make the dataset falsifiable.

P1 design may be *debated* only when the ledger provides:

```text
minimum adjudicated sample size            (threshold set at P0 review)
minimum adjudication coverage              reported with every metric
per-cause confusion matrix                 from adjudicated records
indeterminate rate                         observed distribution
attribution ambiguity rate
adapter-drift incident rate                per feature
human-actuation failure rate               precondition refusals and
                                           post-actuation surprises
```

Numeric thresholds are deliberately not frozen here; the *metrics* are frozen. A gate whose inputs are unmeasurable is worse than no gate, because it looks rigorous.

P1 SHALL reuse the P0 actuator path unchanged and change only the authorization source, from human gesture to a narrowly qualified policy.

## 29. Roadmap

```text
P0  Attention Router + Human-Controlled Actions
P1  Autonomous Continuation Controller   (authorization change only)
P2  Human Handoff Integration            (after handoff reaches main)
P3  Durable Resume                       (restart / offline reconciliation)
P4  Multi-Agent Task Envelope            (only if workload proves need)
```

P4 is where an `envelope_id` might finally be justified. [Machine Truth Harness §6.2](./2026-08-18-machine-truth-harness-architecture-design.md) permits a durable envelope handle only when resume/handoff use cases prove it necessary — and P0 is the instrument that would produce that proof. Building it now would be guessing.

## 30. Architecture invariants

Normative:

- **I1** No component asserts user-task completion. The ShellBeam completion-truth boundary extends to the browser.
- **I2** No ShellBeam fact overrules a human completion claim, in either direction.
- **I3** No global machine fact authorizes a per-watch action. UID-wide liveness is diagnostic or, at most, a short conservative delay.
- **I4** Absence of completion evidence never constitutes continuation authority. Continuation requires positive authorization.
- **I5** In P0, the only source of conversation mutation authority is a human gesture.
- **I6** Authority freshness and observation freshness are orthogonal; a mutation requires both.
- **I7** Native-continue eligibility is ephemeral and is never reconstructed from persisted state.
- **I8** Adapters map observations into the closed core vocabulary; they never extend it.
- **I9** An unqualified adapter feature yields `unknown` with a reason, never a negative fact.
- **I10** Facts enrich a conversation only when attributable to it; insufficient attribution yields `facts_unavailable`.
- **I11** The bridge exposes literal facts with authority, freshness, and coverage. No response field encodes a policy judgment.
- **I12** The bridge has no generic action passthrough. Verbs are a closed enum mapped to hardcoded requests.
- **I13** Only the extension background writes authority state; cross-record transitions take a global lock.
- **I14** Watch authority state lives only in `storage.session`; `storage.local` holds no authority.
- **I15** The Attention Ledger carries no execution authority and cannot resume automation.
- **I16** Adjudication never overwrites observation.
- **I17** Accuracy is never reported without adjudication coverage.
- **I18** Failure-surface comparison is homogeneous in producer, adapter, authority, derivation method, and invocation qualification; `incomparable` never collapses into a progress claim.
- **I19** Receipt novelty is not advancement and resets no budget.
- **I20** ShellBeam core gains no new authority, projection, or durable identity for P0.
- **I21** Installing ShellBeam never grants a browser extension access to machine facts; the manifest install is separate and revocable.

## 31. Design-completion gate

This design is ready for P0 implementation planning when reviewers confirm:

1. the three-authority model and invariants I1–I21 are accepted as normative;
2. the closed core vocabulary in §13 is complete enough for the ChatGPT adapter;
3. Browser Bridge Protocol v1's verb set and literal response shape are accepted, including the absence of any judgment field;
4. the install boundary in §21 is accepted, including a fixed `gecko.id`;
5. the repository split and protocol ownership in §26 are accepted;
6. the P0 scope in §27 is agreed to exclude autonomous mutation;
7. the graduation metrics in §28 are agreed, with numeric thresholds deferred to P0 review.

Open items deliberately left to implementation planning: the empirical Firefox `alarms` granularity floor; final admission constants `N` and `X`; the exact response budget within the ≤ 256 KiB bound; and the numeric graduation thresholds.
