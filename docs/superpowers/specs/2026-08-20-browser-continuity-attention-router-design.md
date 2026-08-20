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

Controller loss SHALL resolve to an attention record and a disarm, not to automatic takeover. Every authority transition therefore writes exactly one watch record, which is what makes the single-record state model crash-safe (§11).

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
existing controller fails liveness check → controller-release transition on the
                                           OLD controller (§11); W is NOT
                                           promoted and stays observer
```

The rule is *existing valid controller wins*, never last-writer-wins.

**Every authority transition SHALL write exactly one record, and no transition SHALL require an atomic multi-record write.** This is the property that makes one-record-per-watch sound, and it is why P0 has no automatic controller takeover (§11).

A failed liveness check does **not** leave records untouched. It releases the stale claim, writing the *old controller's* record. That release is its own transition, distinct from the newcomer's late-bind transition, which writes the newcomer's record. A single scan may therefore schedule two transitions; each writes one record, each is independently valid, and the sequence is retriable:

```text
crash after release, before newcomer's bind
    → zero controllers; the newcomer late-binds again on next observation
crash after newcomer's bind, before release
    → the stale claim survives; the next scan pings, fails, and releases it
```

Neither residual state has two records claiming control, and neither is terminal. Releasing before promoting is what keeps this true: the intermediate state is always *zero* controllers, never two.

**Liveness is a ping, not a tab record.** `tabs.get()` proves only that the browser still has a tab record. Firefox can discard a tab, leaving the record present and the content script dead. Controller liveness SHALL be established by a ping to the content script with a timeout, returning `watch_task_id`, `document_epoch`, `conversation_key?`, and `observation_epoch`.

## 11. Authority and freshness are orthogonal

Two fencing dimensions, both required for any mutation:

```text
controller_generation   proves the actor currently holds authority
observation_epoch       proves the information the actor is acting on is fresh
```

`controller_generation` alone is insufficient. Scenario S7 is a valid generation acting on a stale view.

**P0 has no automatic controller takeover.** Automatic takeover cannot be made crash-safe under one-record-per-watch. Duplicate tabs on one conversation hold separate WatchTask records, so "who controls conversation X" is asserted in two places. Promoting an observer requires clearing the old controller's claim and setting the new one — two records, two writes, and a crash between them leaves torn authority in which two records each claim control at different generations. A global transition lock serializes concurrent JavaScript but cannot make two `storage.session.set()` calls atomic. The alternative, a canonical per-conversation lease record, adds a second authority domain and its own reconciliation rules to P0 for no P0 benefit, since P0 never mutates without a human present.

Therefore:

Controller loss and re-arming are **two authority transitions separated in time**, each writing exactly one record:

```text
TRANSITION 1 — controller release
    controller ping fails
        ↓
    write Attention Ledger  (reason = controller_lost; §18 ordering)
        ↓
    write the OLD controller's WatchTask record:
        watch_state = disarmed
        controller  = false
        ← exactly one authority record
        ↓
    any newcomer remains observer; zero controllers now hold the conversation

--- no automation until a human acts ---

TRANSITION 2 — explicit human re-arm
    human re-arms a chosen tab
        ↓
    global reconciliation sees no active controller
        ↓
    write the CHOSEN WatchTask record:
        controller_generation++
        watch_state = resync_required
        ← exactly one authority record
        ↓
    forced reload of the conversation
        ↓
    adapter bootstrap + qualification (§12)
        ↓
    conversation identity confirmation
        ↓
    observation_epoch established
        ↓
    watch_state = armed
```

The release SHALL happen even though no human has acted yet. Leaving the stale record `armed` with `controller = true` would deadlock every future re-arm: each subsequent reconciliation scan would find an existing controller, ping it, fail, and decline to promote anyone — forever. Releasing first makes the intermediate state *zero controllers*, which is safe precisely because P0 mutates nothing without a human present.

This is acceptable precisely because the Attention Ledger (§18) makes controller loss visible. An unwatched conversation produces a durable record and a badge, so the failure is loud rather than silent — the S10 failure mode is closed by visibility, not by automation. Automatic takeover is deferred to P2 (§29), where it must arrive with an explicit crash-atomicity design.

`RESYNC_REQUIRED` remains in P0 for same-tab reload, navigation, and human re-arm. There SHALL be no edge from authority acquisition directly to actuation in any path.

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

The vocabulary is a set of **orthogonal axes**, not one enum. A watch is routinely `armed` *and* `generating`, or `resync_required` *and* `generation_state = unknown`. Collapsing these into a single `state` field is a specification violation, because it forces an implementer to invent precedence rules between independent facts.

```text
watch_state         armed | resync_required | attention_required | disarmed
generation_state    generating | stopped | unknown
adapter_state       qualified | partially_qualified | unqualified
stop_cause          normal_turn_end | cutoff | stream_error | rate_limit |
                    refusal | content_filter | unknown
cause_quality       explicit | inferred | unavailable
correlation_state   unbound | declared | current | stale | ambiguous |
                    unavailable
action              native_continue | send_continuation
attention_reason    stop_detected | rate_limited | controller_lost |
                    manual_override | binding_ambiguous | attribution_lost |
                    adapter_feature_drift | adapter_unqualified |
                    repeated_failure_surface | resync_failed
```

`attention_reason` is part of the closed vocabulary, not a free-text field in the ledger. `observing` is deliberately absent: it was a restatement of `generation_state`, not a watch state of its own. `budget_exhausted` is deliberately absent from P0 (§24) and would require a vocabulary addition when enforcement arrives in P1.

Adapters map external observations **into** this vocabulary. An adapter SHALL NOT introduce states, causes, actions, or authority semantics. A new term requires a core vocabulary change, invariant review, and a protocol version bump — never an adapter-local extension.

This closes the extension point through which plugin architectures usually erode into rule engines.

## 14. Stop cause and cause quality are separate axes

`indeterminate` is not a cause. Collapsing it into the cause enum hides an absence inside a claim.

```text
stop_cause     normal_turn_end | cutoff | stream_error |
               rate_limit | refusal | content_filter | unknown
cause_quality  explicit | inferred | unavailable
```

The term is `normal_turn_end`, never `normal_completion`. In a subsystem whose first invariant is that no component knows whether the task is complete, the word *completion* SHALL NOT appear in the stop-cause axis: a UI reading `Cause: normal_completion` invites exactly the semantic misreading I1 exists to prevent. `adjudication.actual_cause` (§18) uses the same term.

`stop_cause = unknown` with `cause_quality = unavailable` is an ordinary, expected outcome, and is expected to be the plurality outcome in practice. Distinguishing "the model ended its turn" from "the model was cut off" has no reliable DOM signal in the general case.

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
                       observation_epoch
                       watch_state, generation_state, adapter_state
                       conversation binding
                       shellbeam correlation + binding nonce
                       deadlines, cursors, telemetry counters
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
    observation_epoch,
    watch_state, generation_state, adapter_state,
    conversation_binding, shellbeam_binding,
    deadlines, telemetry_counters, event_cursor,
    last_native_read_at
}
```

A transition is: read record → validate → construct the entire next record → one `set`. Multi-key writes are forbidden, because a crash between two `set` calls leaves torn state that no serializer can repair.

No secondary index (`conversation_key → watch_task_id`) SHALL be maintained in P0. A bounded scan over active watches at late-bind time is trivially cheap at the intended scale and avoids a torn index.

## 16. Serialization model

The background is the only writer of authority state. Content scripts, the host, and any popup/options page SHALL NOT write it. This gives a single logical writer, so compare-and-swap **semantics** are required while a CAS storage **primitive** is not.

Three distinct primitives are required, and conflating any two is a bug:

```text
watchLock(watch_task_id)      per-watch transitions
                              state changes, deadlines, cursors,
                              telemetry counters

globalTransitionLock()        cross-record AUTHORITY transitions ONLY
                              conversation late-binding
                              controller collision reconciliation
                              controller arm / re-arm

nativeReadLimiter             bounded N-slot admission queue
                              NOT a lock, NOT an authority primitive
```

A per-watch lock does not protect conversation late-binding: that transition reads every record and claims a shared resource. Two tabs late-binding to the same conversation concurrently would each scan clean and each claim. Conversely, taking the global lock for ordinary per-watch transitions would reintroduce head-of-line blocking.

**Native read admission SHALL NOT use `globalTransitionLock`.** Waiting for a read slot inside the authority lock would let one slow native read block conversation binding and arming for every watch — the same head-of-line blocking that alternative E was rejected for, reappearing one layer down. `globalTransitionLock` SHALL NOT be held across a slot wait or across native I/O.

If the background dies mid-transition, no `set` has occurred and the old record stands; if it dies after the `set`, the new record stands. Every wake re-reads. There is never a second concurrent background incarnation competing to write.

## 17. Human-controlled actuator

P0 has exactly one mutation authority: a human gesture. The safety property is **zero autonomous mutation**, not zero mutation code.

Building the actuator in P0 under human authority is deliberate. The actuator is the easy part; the decision is the hard part. By the time P1 is considered, the composer targeting, send path, conversation identity confirmation, stale-tab resync, controller fencing, native-continue path, drift handling, and multi-tab collision handling will have been exercised hundreds of times against real conversations. P1 then changes only *who authorizes*, not *what executes*.

A human gesture bypasses **decision policy**. It SHALL NOT bypass **mechanical safety**:

```text
human clicks Continue
    ↓
confirm controller under globalTransitionLock
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

**Completion-marker support is deferred entirely to P1.** P0 does not parse, detect, or act on any completion marker. The Attention Router does not need one: a stop is an attention event regardless of whether the model claimed to be finished, and P0 never continues autonomously, so nothing downstream consumes a completion claim. Adding a marker in P0 would introduce a model-cooperation protocol that instrumentation does not require.

When it does arrive in P1, the following remain normative. A marker is a **stop hint only**: it removes authority rather than granting it. Acceptance requires the marker to be the last non-whitespace token of the latest assistant message *and* to sit outside any code or preformatted element — a DOM check, not a text regex, which is what defeats the model quoting the instruction back at itself. A turn-scoped nonce is required, because a watch-scoped nonce leaves an accepted marker from an earlier turn sitting in the transcript. Absence of a marker is never evidence of anything (I4).

Note that the **binding** declaration in §22 is a different mechanism with a different purpose and IS in P0: it establishes attribution, which P0 core depends on, whereas a completion marker only reports a semantic claim nothing in P0 consumes. An implementer SHALL NOT merge the two markers.

## 18. Attention Ledger

The ledger is the product output. Every path in the system terminates in an attention record, so if the ledger is weak, the system is weak regardless of how sound its authority model is.

Failure mode S10 is the one being closed: transient notifications across many conversations overnight produce a human who believes automation is running while everything has stopped. A durable, enumerable ledger with a badge count is the only defence.

```text
attention_id
watch_task_id
conversation_key?
created_at
severity
reason            one value from the closed attention_reason axis (§13)

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
    actual_cause?  one value from the stop_cause axis (§13, §14),
                   or `other`
    adjudicated_at
```

The ledger lives in `storage.local` and carries **zero execution authority**. It survives a browser restart precisely so the human can read what happened overnight; it can never resume automation by itself.

**Observation and adjudication SHALL remain separate fields.** Observation is what the system believed. Adjudication is what the human later said. Overwriting the former with the latter destroys the only dataset that can falsify the classifier.

**Cross-store commit ordering is normative, because there is none available.** The authority record lives in `storage.session` and the ledger lives in `storage.local`, so an attention event cannot be committed atomically with the disarm that accompanies it. The ordering SHALL be:

```text
1. write the ledger record          (storage.local)
2. write the watch authority record (storage.session)
```

with the ledger write carrying a **deterministic idempotency key** derived from `watch_task_id`, `controller_generation`, `observation_epoch`, `attention_reason`, and the identity of the triggering observation. A background termination between the two writes leaves the watch still armed, so the next wake re-evaluates and re-emits — and the key makes the re-emission a no-op rather than a duplicate.

The ordering is chosen for its failure direction, not its elegance. Writing authority first would produce a disarmed watch with no ledger entry: a silently unwatched conversation, which is scenario S10 exactly. Writing the ledger first can at worst produce a visible duplicate, and the key removes even that. This is the same asymmetry that governs §17: bias every residual failure toward being seen.

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

There is no `action` field, no `argv`, no `command`, no `cwd`, no path, no caller-chosen session id, and no generic passthrough anywhere in the protocol. Verbs are a closed enum; the host maps each to a fixed hardcoded read plan (§20). `internal/app/bridge.Handler` SHALL NOT be reused, because it forwards a caller-supplied action and would hand the browser the entire ShellBeam surface including execution.

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

Each verb's response is assembled by a fixed read plan (§20), not by a single daemon call. The plan is fixed in the host; the caller never names an operation, session, workspace, or path.

**Bridge bootstrap distinguishes three outcomes whose remediations differ.** These are outcomes of *bootstrap*, not of `hello` itself: if the manifest or host binary is absent, Firefox cannot spawn a process, so no `hello` reply exists to carry the diagnosis.

```text
sendNativeMessage rejects           → host or manifest absent;
                                      install the browser host
hello → daemon_unreachable          → start or repair the daemon
hello → protocol set incompatible   → update host or extension
```

The `hello` envelope SHALL be version-stable across protocol versions: a host implementing only v2 must still parse a v1 `hello` and reply with its supported version set. Without that guarantee a version mismatch is indistinguishable from a broken host, and the extension cannot tell the user which remediation applies. The envelope therefore carries only a protocol-version list and a status; every richer field belongs to a versioned verb.

All three outcomes degrade to *ShellBeam enrichment unavailable*. None disables the Attention Router: the browser-only substrate remains useful, and this is the honest fallback identified in alternative A.

## 20. ShellBeam host responsibilities

The host is a security membrane, not a convenience library. It:

1. reads exactly one JSON message from stdin, validates it against the closed verb enum, and rejects anything else;
2. executes that verb's **fixed, bounded, hardcoded read plan**;
3. bounds and truncates the response;
4. writes one JSON message and exits.

**A verb is a read plan, not a single IPC call.** No existing ShellBeam read answers a whole verb, and P0 adds no core surface, so each verb composes several existing reads:

```text
activity_facts
    inspect.activity(activity_id)
        → operation refs, workspace_ids, compacted_operations, timestamps
    inspect.sessions(activity_id)
        → session states  (Activity carries no session state of its own)

activity_events
    inspect.events(target = activity:activity_id, cursor, max_events)
        → single target, single cursor  (TargetActivity exists)

verification_facts
    inspect.activity(activity_id)              → workspace_ids
    inspect.verification(workspace_id, activity_id)
        for each retained workspace, bounded
        (inspect.verification resolves a workspace before deriving
         anything, so a correlation id alone cannot execute it)

structured_failure_facts
    inspect.activity(activity_id)              → operation refs (≤ 64)
    inspect.structured(operation_id)
        bounded newest-first walk
        (inspect.structured is strictly operation-scoped)
```

The least-privilege property is preserved because **every id after the first is derived by the host from the activity, never supplied by the caller.** The caller names one correlation id and nothing else; it cannot reach an operation, session, workspace, or path of its choosing. A read plan is fixed at compile time and is not expressible in the protocol.

Each plan SHALL declare its own coverage. `structured_failure_facts` can only walk retained operation refs, so `compacted_operations > 0` yields partial historical structured coverage; a bounded walk that stops early yields partial coverage for that reason instead. Coverage is reported, never inferred by the extension.

**`verification_facts` SHALL return per-workspace results and SHALL NOT aggregate across workspaces.** An `Activity` may carry several `WorkspaceIDs`, and each workspace has its own policy, its own policy generation, and its own authority. Summing `blocking` counts across workspaces whose policy generations differ produces a number that corresponds to no evaluable question — the flattening forbidden by I11 here and by invariant 13 of the [Machine Truth Harness](./2026-08-18-machine-truth-harness-architecture-design.md), which requires model-facing projections to preserve authority, freshness, and coverage rather than flatten uncertainty. The plan returns a bounded, per-workspace list, each entry carrying its own counts, authority, freshness, and truncation state; whether to compose them into one line of UI is the extension's decision, made with the per-workspace facts still visible.

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

`activity_id` correlation is **optional, best-effort, and may be absent, stale, or wrong**. It is a model-supplied field ([`internal/adapter/mcp/input.go`](../../../internal/adapter/mcp/input.go), the `ActivityID` field on the MCP input struct), so a model may simply omit it, and `inspect.activity` then fails with `ActivityNotFound` ([`internal/app/activity/service.go`](../../../internal/app/activity/service.go), in `Service.Inspect`).

`ActivityNotFound` SHALL map to `facts_unavailable`. It SHALL NOT map to idle, settled, or complete.

### 22.1 Where the correlation id comes from

Attribution is P0 core, so the origin of the correlation id SHALL be frozen here rather than left to a plan. The exact marker syntax is a plan decision; the causality, the ownership, and the promotion rule are not.

```text
1. ARM
   the extension mints, inside the watch record:
       activity_hint   a correlation id it owns and never reuses
       binding_nonce   unguessable, scoped to this watch and this arming
   Neither is derived from a conversation id or from any ShellBeam value.

2. DELIVERY
   the binding instruction reaches the model ONLY through a
   human-controlled path: the human sends it at arm time, or it rides
   a human-triggered continuation (§17). P0 never delivers it autonomously.

3. DECLARATION
   the assistant visibly acknowledges the binding in a structured form
   carrying both activity_hint and binding_nonce.
       → correlation_state = declared

4. CONFIRMATION
   the host reports the activity exists and its latest attributed
   operation falls inside the current turn window.
       → correlation_state = current
```

`declared` requires a **structured acknowledgement carrying the current `binding_nonce`**, not a prose mention and not a bare id. A declaration whose nonce belongs to an earlier arming of the same watch is `stale`, never `declared`; this is what stops a marker left in the transcript by a previous task from binding a new one. Two declarations with different `activity_hint` values against the same nonce are `ambiguous`.

`activity_hint` is a value the extension owns. It is not an identity ShellBeam guarantees: the model may echo it and then omit `activity_id` on its actual calls. That is exactly why step 4 exists and why declaration alone never reaches `current`.

**P0 correlation coverage is partial by construction, and that is intended.** Because delivery is human-gated, watches the human never arms with a preamble and never actuates will stay `unbound` for their whole life. Those watches still work: they run on the browser-only substrate. The resulting coverage number is itself a P0 measurement feeding §28, not a defect to engineer around.

There SHALL be no mechanism that guesses an activity in order to raise coverage. An `unbound` watch stays `unbound`.

### 22.2 Correlation quality

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

### 22.3 Correlation is counted in stages, never as one ratio

A single ratio such as `current / watch_total` mixes two unrelated things — whether the human opted into binding at all, and whether the protocol worked once attempted — into one number from which neither can be recovered. The counters SHALL therefore be staged:

```text
watch_total
binding_eligible                  armed in a mode that carries correlation
binding_instruction_delivered     instruction actually reached the model
binding_declared                  structured acknowledgement observed
binding_current
binding_stale
binding_ambiguous
enrichment_unavailable            host / daemon / protocol (§19)
```

`enrichment_unavailable` is counted separately from every binding stage, because a bridge that is not reachable says nothing about whether binding would have succeeded.

Derived metrics SHALL be reported as ratios between adjacent stages, never as a single coverage figure:

```text
binding_adoption          = binding_eligible / watch_total
binding_delivery_success  = binding_instruction_delivered / binding_eligible
declaration_success       = binding_declared / binding_instruction_delivered
current_correlation_yield = binding_current / binding_declared
```

Four ratios, not three: `binding_eligible` and `binding_instruction_delivered` are separate stages, so a ratio spanning both — `delivered / watch_total` — would recombine adoption with delivery reliability and reproduce the very conflation these counters exist to prevent.

Each ratio answers a different question and implies a different response: adoption is a UX question, delivery is an extension-path question, declaration is model cooperation with the handshake, and yield is ShellBeam correlation reliability. Reporting only their product would make all four unactionable.

## 23. Comparable failure surface

Repeated failure detection is where ShellBeam contributes something no browser-only system can. It requires strict comparability, because a naive comparison silently compares different things.

```text
ComparableFailureSurface {
    producer_identity
    structured_adapter
    authority
    derivation_method
    invocation_qualification
    coverage_scope              # what the execution actually covered,
                                # and the retained/compacted coverage of
                                # the records the signature was built from
    test_surface_signature      # from TestCase{Name, Package, Status}
                                # and Diagnostic{Code, Location}
}
```

Records carry `Authority` and `DerivationMethod` individually ([`internal/core/structuredresult/record.go`](../../../internal/core/structuredresult/record.go)), so comparison SHALL be authority-homogeneous and producer-homogeneous. `pytest tests/a` and `pytest tests/` are not comparable merely because their failing sets coincide; invocation qualification (see `27207d9 docs: bind pytest invocation qualification authority`) is the existing hook through which comparability is encoded, but comparability SHALL be stated in terms of coverage and scope directly rather than assumed to follow from qualification alone. Two executions with equal qualification but unequal coverage are `incomparable`.

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

New receipts SHALL NOT reset any counter or budget. A repeatedly failing suite produces new receipts every turn while advancing nothing, so receipt novelty is not advancement.

## 24. Telemetry counters in P0; enforcement in P1

P0 keeps **counters, not budgets**. A budget that can refuse a human's eleventh click already holds decision authority, which would contradict I5. The two concepts are therefore separated by phase:

```text
P0  telemetry only, never refuses an action
    human_action_count
    consecutive_without_attributable_machine_change
    repeat_failure_signature_count

P1  enforcement thresholds over the same counters
```

Mechanical safety is unaffected by this split and remains active in P0. A stale `observation_epoch`, a conversation identity mismatch, an unqualified adapter feature, or a lost controller SHALL still refuse a human-triggered action (§17), because those are statements about whether the action can be performed correctly. An arbitrary retry count is not; it is a policy judgment about whether the action is *wise*, and in P0 that judgment belongs to the human.

Counter definitions carry into P1 unchanged. Attributable machine progress means a new attributed operation receipt, an attributed event beyond the cursor, an attributed session state transition, a new attributed evidence record, or an attributed verification state change. When correlation is unavailable, machine progress is `unknown` and SHALL NOT be invented; the counter records `unknown` rather than assuming either progress or stall, and the `unknown` rate is itself reported to §28.

Digest similarity SHALL NOT be used to detect stalling. Legitimate progress often reads near-identically (`"Tests 1–20 passed; proceeding"`, `"Tests 21–40 passed; proceeding"`), and similarity would classify real advancement as a stall.

## 25. Degradation taxonomy

Every degradation has one honest rendering and one remediation. None invents a fact, and none silently disables the system.

```text
spawn rejected               → enrichment unavailable; install browser host
daemon unreachable           → enrichment unavailable; check daemon
protocol incompatible        → enrichment unavailable; update component
activity not found           → facts_unavailable
activity stale               → correlation stale; facts shown with age
history compacted            → historical_coverage partial
read plan stopped early      → that plan's coverage partial, with reason
binding declared, unconfirmed→ correlation declared, not current
binding nonce mismatched     → correlation stale
adapter feature drifted      → that feature unknown; dependents refuse
adapter fully unqualified    → attention record; watch disarmed
controller lost              → attention record; watch disarmed;
                               human re-arm required (no auto takeover)
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

1. WatchTask identity and per-watch single-writer authority with `controller_generation`, where every authority transition writes exactly one record.
2. Observation-epoch fencing, with forced resync on same-tab reload, navigation, and human re-arm.
3. Controller-loss **detection** by content-script ping, resolving to an attention record and disarm.
4. MV3-safe state: one record per watch in `storage.session`; `alarms` for deadlines.
5. ChatGPT adapter with per-feature bootstrap qualification and drift semantics.
6. Closed core vocabulary as orthogonal axes, including `attention_reason`.
7. Stop-cause classification with separate cause quality; `unknown` first-class.
8. Late-bound conversation binding with collision reconciliation under `globalTransitionLock`.
9. Automation turn provenance and `manual_override` detection.
10. Browser Bridge Protocol v1: connectionless host, fixed read verbs backed by fixed read plans, bounded literal responses, version-stable `hello`, three-way bootstrap degradation.
11. Binding handshake (§22.1) with `activity_hint` and `binding_nonce`, delivered only through a human-controlled path.
12. Optional correlation with `declared`, `current`, `stale`, `ambiguous`, `unavailable`, and coverage loss all first-class.
13. Comparable failure surface with strict comparability including coverage scope.
14. Attention Ledger in `storage.local` with adjudication, badge, and inbox.
15. Telemetry counters only, with no enforcement authority.
16. Human-gated actuator with full mechanical precondition checks.
17. Explicit, revocable native manifest install, separate from `shellbeam install`.

Excluded from P0, each for a stated reason:

```text
autonomous mutation of any kind      the thing P0 exists to measure
automatic controller takeover        not crash-safe under one record per
                                     watch (§11); needs its own design
budget enforcement                   would give a counter decision
                                     authority, contradicting I5 (§24)
completion-marker support            nothing in P0 consumes a completion
                                     claim (§17)
handoff integration                  branch not on main; P2
durable resume across restart        needs a recovery protocol; P3
any task envelope                    no evidence it is needed yet; P4
any ShellBeam core change            P0 adds surface, not authority
```

## 28. Graduation gate to P1

P0 is instrumentation. Observer-only alone cannot produce this dataset: if nothing ever acts on a classification, nothing ever falsifies it, and three weeks of data would show only what the classifier said, never whether it was right. Human adjudication and human-triggered actuation are what make the dataset falsifiable.

P1 design may be *debated* only when the ledger provides:

```text
minimum adjudicated sample size            (threshold set at P0 review)
minimum adjudication coverage              reported with every metric
per-cause confusion matrix                 from adjudicated records
unknown-cause rate                         observed distribution
binding_adoption                           staged, per §22.3
binding_delivery_success                   staged, per §22.3
declaration_success                        staged, per §22.3
current_correlation_yield                  staged, per §22.3
binding_ambiguous rate                     of declared bindings
enrichment_unavailable rate                counted apart from binding
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
      + budget enforcement over P0 counters
      + completion-marker support with turn-scoped nonce
P2  Human Handoff Integration            (after handoff reaches main)
      + automatic controller takeover, with a crash-atomicity design
P3  Durable Resume                       (restart / offline reconciliation)
P4  Multi-Agent Task Envelope            (only if workload proves need)
```

Automatic controller takeover sits in P2 rather than P1 because it needs a second authority domain (a canonical per-conversation lease) whose reconciliation rules are closer in kind to the handoff work than to the authorization change in P1.

P4 is where an `envelope_id` might finally be justified. [Machine Truth Harness §6.2](./2026-08-18-machine-truth-harness-architecture-design.md) permits a durable envelope handle only when resume/handoff use cases prove it necessary — and P0 is the instrument that would produce that proof. Building it now would be guessing.

## 30. Architecture invariants

Normative:

- **I1** No component asserts user-task completion. The ShellBeam completion-truth boundary extends to the browser.
- **I2** No ShellBeam fact overrules a human completion claim, in either direction.
- **I3** No global machine fact authorizes a per-watch action. UID-wide liveness is diagnostic or, at most, a short conservative delay.
- **I4** Absence of completion evidence never constitutes continuation authority. Continuation requires positive authorization.
- **I5** In P0, the only source of extension-actuator mutation authority is an explicit human gesture directed at that actuator. A human typing into the conversation directly is not an authorization for the extension to act; it is `manual_override`.
- **I6** Authority freshness and observation freshness are orthogonal; a mutation requires both.
- **I7** Native-continue eligibility is ephemeral and is never reconstructed from persisted state.
- **I8** Adapters map observations into the closed core vocabulary; they never extend it.
- **I9** An unqualified adapter feature yields `unknown` with a reason, never a negative fact.
- **I10** Facts enrich a conversation only when attributable to it; insufficient attribution yields `facts_unavailable`.
- **I11** The bridge exposes literal facts with authority, freshness, and coverage. No response field encodes a policy judgment.
- **I12** The bridge has no generic action passthrough. Verbs are a closed enum mapped to fixed hardcoded read plans.
- **I13** Only the extension background writes authority state. Cross-record authority transitions are serialized globally; native read admission uses a separate bounded limiter, and the authority lock is never held across a slot wait or native I/O.
- **I14** Watch authority state lives only in `storage.session`; `storage.local` holds no authority.
- **I15** The Attention Ledger carries no execution authority and cannot resume automation.
- **I16** Adjudication never overwrites observation.
- **I17** Accuracy is never reported without adjudication coverage.
- **I18** Failure-surface comparison is homogeneous in producer, adapter, authority, derivation method, invocation qualification, **and coverage scope**; `incomparable` never collapses into a progress claim.
- **I19** Receipt novelty is not advancement and resets no counter or budget.
- **I20** ShellBeam core gains no new authority, projection, or durable identity for P0.
- **I21** Installing ShellBeam never grants a browser extension access to machine facts; the manifest install is separate and revocable.
- **I22** In P0, every authority transition commits through exactly one WatchTask authority record, and no P0 authority transition requires an atomic multi-record write. Controller loss writes the old controller's record; a later human re-arm writes the selected WatchTask in a separate transition. Where one scan schedules both, each step is independently valid and retriable, and the intermediate state holds zero controllers rather than two. This invariant is scoped to P0 deliberately: the canonical conversation lease that automatic takeover needs (§29, P2) will change the authority storage model, and it must arrive as an explicit successor design rather than as a violation of this one.
- **I23** A bridge verb's read plan is fixed in the host. Every id after the caller's single correlation id is derived by the host; the caller can never name an operation, session, workspace, or path.
- **I24** A correlation reaches `current` only through confirmed existence and recency. A transcript declaration alone never reaches `current`, and a declaration carrying a superseded nonce is `stale`.

## 31. Design-completion gate

This design is ready for P0 implementation planning when reviewers confirm:

1. the three-authority model and invariants I1–I24 are accepted as normative;
2. the closed core vocabulary in §13 is accepted as orthogonal axes, and is complete enough for the ChatGPT adapter;
3. Browser Bridge Protocol v1's verb set, per-verb read plans (§20), version-stable `hello`, and literal response shape are accepted, including the absence of any judgment field;
4. the binding handshake in §22.1 is accepted, including that P0 correlation coverage is partial by construction and is reported as staged counters per §22.3, never as one ratio;
5. the install boundary in §21 is accepted, including a fixed `gecko.id`;
6. the repository split and protocol ownership in §26 are accepted;
7. the P0 scope in §27 is agreed, including the exclusion of automatic controller takeover, budget enforcement, and completion-marker support;
8. the graduation metrics in §28 are agreed, with numeric thresholds deferred to P0 review.

Open items deliberately left to implementation planning: the empirical Firefox `alarms` granularity floor; final admission constants `N` and `X`; the exact response budget within the ≤ 256 KiB bound; the concrete binding-marker syntax (its causality and promotion rule are frozen in §22.1, its spelling is not); per-verb read-plan walk limits within the response budget; and the numeric graduation thresholds.
