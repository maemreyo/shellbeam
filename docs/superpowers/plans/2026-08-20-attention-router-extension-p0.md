# Attention Router Firefox Extension — P0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Repository placement:** this plan implements the **extension repository** (spec §26), not ShellBeam. It is stored here only as the handoff artifact produced alongside the architecture spec; move it into the extension repository's own `docs/superpowers/plans/` at repository creation. No TypeScript, web-extension tooling, or browser asset from this plan may be committed to the ShellBeam repository.

**Goal:** Ship an Attention Router for many concurrent ChatGPT Web conversations: observe, classify why each stopped, correlate against attributed ShellBeam facts, record what needs the human, and actuate a continuation only on an explicit human gesture — with zero autonomous mutation.

**Architecture:** All authority lives in the extension background, which is the single writer. Content scripts produce untrusted observations and hold no authority. Watch runtime state is one record per watch in `storage.session`, so a Manifest V3 background unload is invisible to semantics; the durable Attention Ledger lives in `storage.local` and carries no execution authority. Two orthogonal fences guard every mutation — `controller_generation` for authority and `observation_epoch` for freshness. ShellBeam facts arrive through `runtime.sendNativeMessage` under an extension-side admission limiter, and enrichment is always optional.

**Tech Stack:** TypeScript 5.x (strict); Firefox Manifest V3 with a non-persistent background script; `browser.storage.session` / `browser.storage.local` / `browser.alarms` / `browser.tabs` / `browser.notifications` / `browser.runtime.sendNativeMessage`; vitest for unit tests with an in-memory fake of the `browser.*` surface; `web-ext` for manual loading. No bundler-level framework, no UI library, no state-management library.

**Spec:** `docs/superpowers/specs/2026-08-20-browser-continuity-attention-router-design.md` (approved architecture freeze)

## Global Constraints

- **Zero autonomous conversation mutation.** The only source of actuator authority is an explicit human gesture directed at that actuator (I5). A human typing into the conversation is `manual_override`, never an authorization.
- No component asserts user-task completion, and no ShellBeam fact overrules a human completion claim in either direction (I1, I2).
- No global machine fact authorizes a per-watch action (I3). UID-wide liveness is diagnostic at most.
- Absence of completion evidence never constitutes continuation authority (I4). P0 does not parse completion markers at all.
- A mutation requires both a current `controller_generation` and a current `observation_epoch` (I6).
- Native-continue eligibility is ephemeral and is never reconstructed from persisted state (I7).
- Adapters map observations into the closed core vocabulary and never extend it (I8). A new term is a core change with a protocol version bump, not an adapter change.
- An unqualified adapter feature yields `unknown` with a reason, never a negative fact (I9).
- Facts enrich a conversation only when attributable to it; insufficient attribution yields `facts_unavailable` (I10).
- Only the background writes authority state. Cross-record authority transitions serialize on a global lock; native read admission uses a separate bounded limiter, and the authority lock is never held across a slot wait or native I/O (I13).
- Watch authority state lives only in `storage.session`; `storage.local` holds no authority (I14).
- The Attention Ledger carries no execution authority and cannot resume automation (I15).
- Adjudication never overwrites observation (I16). Accuracy is never reported without adjudication coverage (I17).
- In P0 every authority transition commits through exactly one WatchTask record. A pass detecting a stale controller schedules only the release and stops; acquisition needs a later pass, and a demoted observer needs an explicit human re-arm (I22).
- Budgets are telemetry only. Mechanical checks may refuse a human action; an arbitrary retry count may not (spec §24).
- Failure-surface comparison is homogeneous in producer, adapter, authority, derivation method, invocation qualification, and coverage scope; `incomparable` never collapses into a progress claim (I18). Receipt novelty is not advancement (I19).
- Digest similarity is never used to detect stalling (spec §24).
- `setTimeout` is never used for a wait longer than one turn of the event loop. All deadlines use `browser.alarms` plus an authoritative deadline in `storage.session`.
- Never write multiple `storage.session` keys for one logical transition. One record, one `set`.
- TDD is mandatory: focused RED → minimal GREEN → focused regression → commit.
- Files target 150–300 lines, hard cap 500; functions hard cap 80 lines.
- `strict: true`, `noUncheckedIndexedAccess: true`, and no `any` in committed code.

## File Structure

```text
manifest.json                        MV3, fixed browser_specific_settings.gecko.id
src/core/vocabulary.ts               the closed axes; no other file defines a state string
src/core/watchRecord.ts              WatchTask record type, schema version, revision
src/core/attention.ts                attention record type + deterministic idempotency key
src/core/stopCause.ts                stop_cause x cause_quality classification (pure)
src/core/correlation.ts              binding state machine + staged counters (pure)
src/core/comparability.ts            ComparableFailureSurface + signature equality (pure)
src/background/store.ts              one-record read/modify/write over storage.session
src/background/locks.ts              keyed serial executor + globalTransitionLock
src/background/reconcile.ts          late-bind, collision, release-only pass, re-arm
src/background/resync.ts             forced reload → fresh observation epoch → armed
src/background/liveness.ts           content-script ping with timeout
src/background/deadlines.ts          alarms + authoritative deadlines
src/background/nativeBridge.ts       sendNativeMessage client + bootstrap outcomes
src/background/admission.ts          N-slot limiter, per-watch interval, jitter
src/background/ledger.ts             storage.local ledger, ordering, idempotency
src/background/actuator.ts           human-gated actuation, mechanical preconditions
src/background/index.ts             wiring only; no logic
src/content/adapter.ts               ChatGPT adapter: qualification matrix + fingerprint
src/content/observe.ts               DOM observation → vocabulary, posts to background
src/ui/inbox.ts                      ledger inbox, badge, adjudication controls
test/fakes/browser.ts                in-memory browser.* fake used by every test
```

Boundary rule: `src/core/**` is pure and imports nothing from `src/background/**`, `src/content/**`, or the `browser` global — which is what makes it directly unit-testable. `src/content/**` never imports `src/background/**`.

---

### Task 1: Scaffold, manifest, and the browser fake

**Files:**
- Create: `package.json`, `tsconfig.json`, `vitest.config.ts`, `manifest.json`, `test/fakes/browser.ts`, `test/fakes/browser.test.ts`

**Interfaces:**
- Produces: `makeFakeBrowser(): FakeBrowser` with `storage.session`, `storage.local`, `alarms`, `tabs`, `runtime.sendNativeMessage`, `notifications`, plus test controls `fake.advanceTime(ms)`, `fake.fireAlarm(name)`, `fake.crashBetweenWrites()`.

The fake is the foundation for every later task. `crashBetweenWrites()` makes the crash-window tests in Tasks 4 and 8 possible, so it is built here rather than retrofitted.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { makeFakeBrowser } from "./browser";

describe("browser fake", () => {
  it("keeps session and local storage separate", async () => {
    const fake = makeFakeBrowser();
    await fake.storage.session.set({ a: 1 });
    await fake.storage.local.set({ a: 2 });
    expect(await fake.storage.session.get("a")).toEqual({ a: 1 });
    expect(await fake.storage.local.get("a")).toEqual({ a: 2 });
  });

  it("clears session storage on simulated browser shutdown but keeps local", async () => {
    const fake = makeFakeBrowser();
    await fake.storage.session.set({ lease: "held" });
    await fake.storage.local.set({ ledger: ["item"] });
    fake.shutdownBrowser();
    expect(await fake.storage.session.get("lease")).toEqual({});
    expect(await fake.storage.local.get("ledger")).toEqual({ ledger: ["item"] });
  });

  it("fires alarms only when told, never on a real timer", async () => {
    const fake = makeFakeBrowser();
    let fired = 0;
    fake.alarms.onAlarm.addListener(() => { fired += 1; });
    await fake.alarms.create("watch:w1", { delayInMinutes: 0.2 });
    expect(fired).toBe(0);
    fake.fireAlarm("watch:w1");
    expect(fired).toBe(1);
  });

  it("can simulate a background death between two writes", async () => {
    const fake = makeFakeBrowser();
    await fake.storage.session.set({ first: true });
    fake.crashBetweenWrites();
    await expect(fake.storage.session.set({ second: true })).rejects.toThrow(/background terminated/);
    expect(await fake.storage.session.get("second")).toEqual({});
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run test/fakes/browser.test.ts`
Expected: FAIL — cannot resolve `./browser`.

- [ ] **Step 3: Write minimal implementation**

Write `manifest.json` first, because the pinned extension id is what the ShellBeam manifest allowlists:

```json
{
  "manifest_version": 3,
  "name": "ShellBeam Attention Router",
  "version": "0.1.0",
  "browser_specific_settings": {
    "gecko": { "id": "attention-router@shellbeam.local", "strict_min_version": "128.0" }
  },
  "background": { "scripts": ["background.js"] },
  "permissions": ["storage", "alarms", "tabs", "notifications", "nativeMessaging"],
  "host_permissions": ["https://chatgpt.com/*"],
  "content_scripts": [{ "matches": ["https://chatgpt.com/*"], "js": ["content.js"], "run_at": "document_idle" }],
  "action": { "default_title": "Attention Router", "default_popup": "inbox.html" }
}
```

The id is fixed and must match the value passed to `shellbeam browser-host install --extension-id=`. Changing it later breaks the native manifest allowlist.

Then implement `test/fakes/browser.ts` as a plain object graph: two independent `Map`s behind `get`/`set`/`remove` returning promises; an alarms registry that records created alarms and only invokes listeners from `fireAlarm`; a `tabs.sendMessage` that resolves from a per-tab handler map or rejects when a tab was marked discarded; `runtime.sendNativeMessage` that resolves from a queue of scripted replies or rejects with `Error("no such native application")`; `shutdownBrowser()` clearing only the session map; and `crashBetweenWrites()` arming a one-shot flag that makes the next `set` reject with `Error("background terminated")` without mutating the map.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run test/fakes/browser.test.ts`
Expected: PASS, four tests.

- [ ] **Step 5: Commit**

```bash
git add package.json tsconfig.json vitest.config.ts manifest.json test/fakes
git commit -m "chore: scaffold extension with a browser API fake"
```

---

### Task 2: Closed vocabulary and the watch record

**Files:**
- Create: `src/core/vocabulary.ts`, `src/core/watchRecord.ts`
- Test: `src/core/watchRecord.test.ts`

**Interfaces:**
- Produces: `WATCH_STATES`, `GENERATION_STATES`, `ADAPTER_STATES`, `STOP_CAUSES`, `CAUSE_QUALITIES`, `CORRELATION_STATES`, `ACTIONS`, `ATTENTION_REASONS` as `readonly` tuples plus their derived union types; `WATCH_RECORD_SCHEMA_VERSION`; `type WatchRecord`; `newWatchRecord(id, tabId, now): WatchRecord`; `validateWatchRecord(value: unknown): WatchRecord`.

The axes are orthogonal by construction: `watch_state` and `generation_state` are separate fields, so no code can collapse them.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { ATTENTION_REASONS, STOP_CAUSES, WATCH_STATES, GENERATION_STATES } from "./vocabulary";
import { newWatchRecord, validateWatchRecord, WATCH_RECORD_SCHEMA_VERSION } from "./watchRecord";

describe("vocabulary", () => {
  it("keeps watch_state and generation_state as separate axes", () => {
    expect(WATCH_STATES).toEqual(["armed", "resync_required", "attention_required", "disarmed"]);
    expect(GENERATION_STATES).toEqual(["generating", "stopped", "unknown"]);
    for (const state of WATCH_STATES) {
      expect(GENERATION_STATES).not.toContain(state);
    }
  });

  it("uses normal_turn_end and never the word completion in stop causes", () => {
    expect(STOP_CAUSES).toContain("normal_turn_end");
    for (const cause of STOP_CAUSES) {
      expect(cause).not.toMatch(/complet/i);
    }
  });

  it("closes attention_reason rather than leaving it free text", () => {
    expect(ATTENTION_REASONS).toContain("controller_lost");
    expect(ATTENTION_REASONS).toContain("manual_override");
    expect(ATTENTION_REASONS).not.toContain("budget_exhausted");
  });
});

describe("watch record", () => {
  it("starts disarmed, unbound, with no controller and observer_origin never", () => {
    const record = newWatchRecord("w1", 17, 1000);
    expect(record.schemaVersion).toBe(WATCH_RECORD_SCHEMA_VERSION);
    expect(record.watchState).toBe("disarmed");
    expect(record.generationState).toBe("unknown");
    expect(record.controller).toBe(false);
    expect(record.controllerGeneration).toBe(0);
    expect(record.observationEpoch).toBe(0);
    expect(record.observerOrigin).toBe("never");
    expect(record.conversationKey).toBeUndefined();
    expect(record.correlation.state).toBe("unbound");
    expect(record.revision).toBe(1);
  });

  it("rejects a record whose state strings are outside the closed axes", () => {
    const record = { ...newWatchRecord("w1", 17, 1000), watchState: "running" };
    expect(() => validateWatchRecord(record)).toThrow(/watchState/);
  });

  it("rejects a record carrying a judgment field", () => {
    const record = { ...newWatchRecord("w1", 17, 1000), shouldContinue: true };
    expect(() => validateWatchRecord(record)).toThrow(/unexpected field/);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/core/watchRecord.test.ts`
Expected: FAIL — cannot resolve `./vocabulary`.

- [ ] **Step 3: Write minimal implementation**

```ts
// src/core/vocabulary.ts
//
// The closed core vocabulary. Axes are orthogonal: a watch is routinely armed
// AND generating, so collapsing these into one enum would force invented
// precedence rules between independent facts. Adapters map observations into
// these values and never extend them.
export const WATCH_STATES = ["armed", "resync_required", "attention_required", "disarmed"] as const;
export const GENERATION_STATES = ["generating", "stopped", "unknown"] as const;
export const ADAPTER_STATES = ["qualified", "partially_qualified", "unqualified"] as const;
export const STOP_CAUSES = ["normal_turn_end", "cutoff", "stream_error", "rate_limit", "refusal", "content_filter", "unknown"] as const;
export const CAUSE_QUALITIES = ["explicit", "inferred", "unavailable"] as const;
export const CORRELATION_STATES = ["unbound", "declared", "current", "stale", "ambiguous", "unavailable"] as const;
export const ACTIONS = ["native_continue", "send_continuation"] as const;
export const ATTENTION_REASONS = [
  "stop_detected", "rate_limited", "controller_lost", "manual_override",
  "binding_ambiguous", "attribution_lost", "adapter_feature_drift",
  "adapter_unqualified", "repeated_failure_surface", "resync_failed",
] as const;

export type WatchState = (typeof WATCH_STATES)[number];
export type GenerationState = (typeof GENERATION_STATES)[number];
export type AdapterState = (typeof ADAPTER_STATES)[number];
export type StopCause = (typeof STOP_CAUSES)[number];
export type CauseQuality = (typeof CAUSE_QUALITIES)[number];
export type CorrelationState = (typeof CORRELATION_STATES)[number];
export type ActionName = (typeof ACTIONS)[number];
export type AttentionReason = (typeof ATTENTION_REASONS)[number];
```

`watchRecord.ts` defines the record with one field per axis plus `controller`, `controllerGeneration`, `observationEpoch`, `observerOrigin: "never" | "demoted"`, `conversationKey?`, `correlation: { state; activityHint?; bindingNonce?; latestObservedAt? }`, `deadlines: { idleDeadlineAt?; }`, `telemetry: { humanActionCount; consecutiveWithoutAttributableMachineChange; repeatFailureSignatureCount }`, `outboundTurn?: { id: string; state: "prepared" | "committed" }`, `nativeContinueObservedAtDocumentEpoch?: number`, `eventCursor?`, `lastNativeReadAt?`, `revision`, `schemaVersion`, `tabId`, `watchTaskId`. `validateWatchRecord` checks every axis value against its tuple, rejects unknown keys by comparing against an explicit key allowlist, and throws with the offending field name.

Declare `outboundTurn` and `nativeContinueObservedAtDocumentEpoch` here even though Task 7 and Task 15 are what populate them. The allowlist in `validateWatchRecord` rejects unknown keys by design, so a field introduced later would be rejected by the validator written now — the whole record shape belongs in one place.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/core/watchRecord.test.ts`
Expected: PASS, six tests.

- [ ] **Step 5: Commit**

```bash
git add src/core/vocabulary.ts src/core/watchRecord.ts src/core/watchRecord.test.ts
git commit -m "feat: define closed vocabulary and watch record"
```

---

### Task 3: One-record store and the two lock primitives

**Files:**
- Create: `src/background/store.ts`, `src/background/locks.ts`
- Test: `src/background/locks.test.ts`, `src/background/store.test.ts`

**Interfaces:**
- Consumes: `WatchRecord`, `validateWatchRecord`.
- Produces: `makeStore(browser)` with `readWatch(id)`, `writeWatch(record)`, `listWatches()`; `makeLocks()` with `withWatchLock<T>(id, fn)`, `withGlobalTransitionLock<T>(fn)`.

`withWatchLock` and `withGlobalTransitionLock` are distinct primitives. Conflating them either fails to protect cross-record transitions or reintroduces head-of-line blocking.

- [ ] **Step 1: Write the failing test**

```ts
import { describe, expect, it } from "vitest";
import { makeLocks } from "./locks";

describe("locks", () => {
  it("serializes interleaved read-modify-write on the same watch", async () => {
    const locks = makeLocks();
    let value = 0;
    const bump = () => locks.withWatchLock("w1", async () => {
      const seen = value;
      await Promise.resolve();
      value = seen + 1;
    });
    await Promise.all([bump(), bump(), bump()]);
    expect(value).toBe(3);
  });

  it("does not serialize different watches against each other", async () => {
    const locks = makeLocks();
    const order: string[] = [];
    let releaseSlow: () => void = () => {};
    const slow = locks.withWatchLock("w1", async () => {
      order.push("slow-start");
      await new Promise<void>((resolve) => { releaseSlow = resolve; });
      order.push("slow-end");
    });
    const fast = locks.withWatchLock("w2", async () => { order.push("fast"); });
    await fast;
    expect(order).toEqual(["slow-start", "fast"]);
    releaseSlow();
    await slow;
  });

  it("serializes cross-record transitions against each other", async () => {
    const locks = makeLocks();
    let inside = 0;
    let maxInside = 0;
    const pass = () => locks.withGlobalTransitionLock(async () => {
      inside += 1;
      maxInside = Math.max(maxInside, inside);
      await Promise.resolve();
      inside -= 1;
    });
    await Promise.all([pass(), pass(), pass()]);
    expect(maxInside).toBe(1);
  });

  it("does not block a watch transition while the global lock is held", async () => {
    const locks = makeLocks();
    let release: () => void = () => {};
    const held = locks.withGlobalTransitionLock(() => new Promise<void>((resolve) => { release = resolve; }));
    let ran = false;
    await locks.withWatchLock("w1", async () => { ran = true; });
    expect(ran).toBe(true);
    release();
    await held;
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/background/locks.test.ts`
Expected: FAIL — cannot resolve `./locks`.

- [ ] **Step 3: Write minimal implementation**

```ts
// src/background/locks.ts
//
// The background is the only writer of authority state, so compare-and-swap
// SEMANTICS are required while a CAS storage primitive is not: a keyed serial
// executor is enough, because two background incarnations never run at once.
// The global lock exists only for transitions that span records.
type Task<T> = () => Promise<T> | T;

const chain = (tails: Map<string, Promise<unknown>>, key: string) => <T>(task: Task<T>): Promise<T> => {
  const previous = tails.get(key) ?? Promise.resolve();
  const next = previous.then(task, task);
  tails.set(key, next.then(() => undefined, () => undefined));
  return next as Promise<T>;
};

export const makeLocks = () => {
  const perWatch = new Map<string, Promise<unknown>>();
  const global = new Map<string, Promise<unknown>>();
  return {
    withWatchLock: <T>(watchTaskId: string, task: Task<T>) => chain(perWatch, watchTaskId)(task),
    withGlobalTransitionLock: <T>(task: Task<T>) => chain(global, "global")(task),
  };
};
```

`store.ts` writes the whole record under one key `watch:<id>` with a single `set`, bumping `revision`, and validates on read; `listWatches` uses one `get(null)`-style read and filters keys by prefix. Write the store test to assert that one logical transition performs exactly one `set` call (count calls on the fake) and that a record failing validation is surfaced as an error rather than silently repaired.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/background/`
Expected: PASS, four lock tests plus the store tests.

- [ ] **Step 5: Commit**

```bash
git add src/background/store.ts src/background/locks.ts src/background/locks.test.ts src/background/store.test.ts
git commit -m "feat: add one-record store and separate lock primitives"
```

---

### Task 4: Late binding, collision, and the release-only pass

**Files:**
- Create: `src/background/reconcile.ts`, `src/background/liveness.ts`
- Test: `src/background/reconcile.test.ts`

**Interfaces:**
- Consumes: store, locks, `WatchRecord`.
- Produces: `pingController(browser, record, timeoutMs): Promise<PingResult>`; `reconcileConversation(deps, { watchTaskId, conversationKey }): Promise<ReconcileOutcome>` where `ReconcileOutcome` is one of `{ kind: "bound_as_controller" } | { kind: "demoted_to_observer" } | { kind: "released_stale_controller"; releasedWatchTaskId: string }`.

This is the task the architecture was rewritten twice for. Read spec §10 and §11 before writing code.

- [ ] **Step 1: Write the failing test**

```ts
describe("reconcileConversation", () => {
  it("binds a first late-binding watch as controller", async () => {
    const deps = await harness([{ id: "w1", tabId: 17 }]);
    const outcome = await reconcileConversation(deps, { watchTaskId: "w1", conversationKey: "C1" });
    expect(outcome.kind).toBe("bound_as_controller");
    const w1 = await deps.store.readWatch("w1");
    expect(w1.controller).toBe(true);
    expect(w1.conversationKey).toBe("C1");
  });

  it("demotes a newcomer when a live controller already holds the conversation", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, controllerOf: "C1", alive: true }, { id: "w2", tabId: 21 }]);
    const outcome = await reconcileConversation(deps, { watchTaskId: "w2", conversationKey: "C1" });
    expect(outcome.kind).toBe("demoted_to_observer");
    const w2 = await deps.store.readWatch("w2");
    expect(w2.controller).toBe(false);
    expect(w2.observerOrigin).toBe("demoted");
    const w1 = await deps.store.readWatch("w1");
    expect(w1.controller).toBe(true);
  });

  it("releases a stale controller and stops, promoting nobody in that pass", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, controllerOf: "C1", alive: false }, { id: "w2", tabId: 21 }]);
    const outcome = await reconcileConversation(deps, { watchTaskId: "w2", conversationKey: "C1" });
    expect(outcome).toEqual({ kind: "released_stale_controller", releasedWatchTaskId: "w1" });
    const w1 = await deps.store.readWatch("w1");
    expect(w1.controller).toBe(false);
    expect(w1.watchState).toBe("disarmed");
    const w2 = await deps.store.readWatch("w2");
    expect(w2.controller).toBe(false);
  });

  it("writes exactly one watch record in a release pass", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, controllerOf: "C1", alive: false }, { id: "w2", tabId: 21 }]);
    deps.fake.resetWriteCount();
    await reconcileConversation(deps, { watchTaskId: "w2", conversationKey: "C1" });
    expect(deps.fake.sessionWriteCount()).toBe(1);
  });

  it("never leaves two controllers on one conversation across a crash", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, controllerOf: "C1", alive: false }, { id: "w2", tabId: 21 }]);
    deps.fake.crashBetweenWrites();
    await reconcileConversation(deps, { watchTaskId: "w2", conversationKey: "C1" }).catch(() => undefined);
    const controllers = (await deps.store.listWatches()).filter((r) => r.controller && r.conversationKey === "C1");
    expect(controllers.length).toBeLessThanOrEqual(1);
  });

  it("refuses to promote a demoted observer without an explicit re-arm", async () => {
    const deps = await harness([{ id: "w2", tabId: 21, observerOrigin: "demoted", conversationKey: "C1" }]);
    const outcome = await reconcileConversation(deps, { watchTaskId: "w2", conversationKey: "C1" });
    expect(outcome.kind).not.toBe("bound_as_controller");
    expect((await deps.store.readWatch("w2")).controller).toBe(false);
  });

  it("treats a discarded tab as a dead controller rather than a live one", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, controllerOf: "C1", alive: false, tabRecordExists: true }, { id: "w2", tabId: 21 }]);
    const outcome = await reconcileConversation(deps, { watchTaskId: "w2", conversationKey: "C1" });
    expect(outcome.kind).toBe("released_stale_controller");
  });
});
```

Write `harness(specs)` in the test file: it builds a fake browser, a store, locks, seeds each spec as a watch record, and registers a `tabs.sendMessage` handler per tab that replies to a ping when `alive` is true and never resolves when it is false, so the timeout path is exercised.

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/background/reconcile.test.ts`
Expected: FAIL — cannot resolve `./reconcile`.

- [ ] **Step 3: Write minimal implementation**

```ts
// src/background/reconcile.ts
//
// Controller loss and re-arming are two authority transitions separated in
// time. A pass that finds a stale controller releases it and STOPS: promotion
// requires a later pass after the release is durably committed. Promoting in
// the same pass would need two record writes, and a crash between them would
// leave two records each claiming control.
//
// Leaving the stale record armed instead would deadlock every future re-arm:
// each pass would find a controller, ping it, fail, and decline to promote
// anyone forever.
export const reconcileConversation = async (deps: Deps, input: { watchTaskId: string; conversationKey: string }) =>
  deps.locks.withGlobalTransitionLock(async () => {
    const watches = await deps.store.listWatches();
    const holder = watches.find((r) => r.conversationKey === input.conversationKey && r.controller);
    const self = watches.find((r) => r.watchTaskId === input.watchTaskId);
    if (!self) throw new Error(`unknown watch ${input.watchTaskId}`);

    if (holder && holder.watchTaskId !== input.watchTaskId) {
      const ping = await pingController(deps.browser, holder, deps.pingTimeoutMs);
      if (ping.alive) {
        await deps.store.writeWatch({ ...self, conversationKey: input.conversationKey, controller: false, observerOrigin: "demoted" });
        return { kind: "demoted_to_observer" as const };
      }
      await deps.ledger.record({ reason: "controller_lost", watchTaskId: holder.watchTaskId, conversationKey: input.conversationKey, controllerGeneration: holder.controllerGeneration, observationEpoch: holder.observationEpoch });
      await deps.store.writeWatch({ ...holder, controller: false, watchState: "disarmed" });
      return { kind: "released_stale_controller" as const, releasedWatchTaskId: holder.watchTaskId };
    }

    if (self.observerOrigin === "demoted") {
      return { kind: "demoted_to_observer" as const };
    }
    await deps.store.writeWatch({ ...self, conversationKey: input.conversationKey, controller: true, controllerGeneration: self.controllerGeneration + 1, watchState: "resync_required" });
    return { kind: "bound_as_controller" as const };
  });
```

A watch that reaches `resync_required` here is not yet usable: Task 15 implements the transition from `resync_required` to `armed`, and nothing may actuate until it completes.

`liveness.ts` implements `pingController` as `browser.tabs.sendMessage(tabId, { kind: "ping", watchTaskId })` raced against a timeout, returning `{ alive: false, reason: "timeout" }` on timeout or rejection. It must not use `tabs.get`: a discarded tab keeps its record while its content script is dead, and trusting the record would strand the lease on a zombie tab.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/background/reconcile.test.ts`
Expected: PASS, seven tests.

- [ ] **Step 5: Commit**

```bash
git add src/background/reconcile.ts src/background/liveness.ts src/background/reconcile.test.ts
git commit -m "feat: reconcile conversations with a release-only stale pass"
```

---

### Task 5: Adapter qualification matrix and per-feature drift

**Files:**
- Create: `src/content/adapter.ts`
- Test: `src/content/adapter.test.ts`

**Interfaces:**
- Produces: `ADAPTER_FEATURES` tuple; `qualifyAdapter(doc: Document): AdapterQualification` returning `{ adapterId, adapterVersion, fingerprint, features: Record<Feature, "qualified" | "drifted" | "unavailable"> , adapterState }`; `featureValue<T>(q, feature, read: () => T): { value: T | "unknown"; reason?: string }`.

Drift is per feature. A cosmetic change to one banner must not disarm every watch.

- [ ] **Step 1: Write the failing test**

```ts
describe("qualifyAdapter", () => {
  it("qualifies each feature independently and reports a fingerprint", () => {
    const q = qualifyAdapter(domWith({ composer: true, sendAction: true, messageContainer: true, generationIndicator: true, nativeContinue: false, rateLimitSignal: false, networkErrorSignal: true, assistantLastMessage: true }));
    expect(q.features.composer).toBe("qualified");
    expect(q.features.nativeContinue).toBe("unavailable");
    expect(q.features.rateLimitSignal).toBe("unavailable");
    expect(q.adapterState).toBe("partially_qualified");
    expect(q.fingerprint).toMatch(/^[0-9a-f]{16}$/);
  });

  it("yields unknown with a reason instead of a negative fact for an unqualified feature", () => {
    const q = qualifyAdapter(domWith({ rateLimitSignal: false, composer: true, sendAction: true, messageContainer: true, assistantLastMessage: true, generationIndicator: true, nativeContinue: true, networkErrorSignal: true }));
    const read = featureValue(q, "rateLimitSignal", () => false);
    expect(read.value).toBe("unknown");
    expect(read.reason).toBe("feature_unqualified");
  });

  it("returns a stable fingerprint for an unchanged DOM shape and a different one after drift", () => {
    const shape = { composer: true, sendAction: true, messageContainer: true, generationIndicator: true, nativeContinue: true, rateLimitSignal: true, networkErrorSignal: true, assistantLastMessage: true };
    const a = qualifyAdapter(domWith(shape));
    const b = qualifyAdapter(domWith(shape));
    expect(a.fingerprint).toBe(b.fingerprint);
    const drifted = qualifyAdapter(domWith({ ...shape, rateLimitSignal: false }));
    expect(drifted.fingerprint).not.toBe(a.fingerprint);
  });

  it("reports unqualified only when a required feature is missing", () => {
    const q = qualifyAdapter(domWith({ composer: false, sendAction: false, messageContainer: false, generationIndicator: false, nativeContinue: false, rateLimitSignal: false, networkErrorSignal: false, assistantLastMessage: false }));
    expect(q.adapterState).toBe("unqualified");
  });
});
```

Write `domWith(shape)` using `document.implementation.createHTMLDocument()` and inserting a marker element per feature with the selector the adapter looks for, so the test exercises real DOM queries rather than mocking the adapter's internals.

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/content/adapter.test.ts`
Expected: FAIL — cannot resolve `./adapter`.

- [ ] **Step 3: Write minimal implementation**

Implement `ADAPTER_FEATURES = ["messageContainer","assistantLastMessage","composer","sendAction","generationIndicator","nativeContinue","rateLimitSignal","networkErrorSignal"] as const`, a selector table keyed by feature, and `qualifyAdapter` probing each selector and recording `qualified` when found or `unavailable` when not. `adapterState` is `qualified` when all are found, `unqualified` when none of `messageContainer`, `assistantLastMessage`, `composer`, `sendAction` are found, and `partially_qualified` otherwise. `fingerprint` is a 16-hex-char digest over the sorted `feature:state` pairs plus `adapterVersion`, computed with a small non-cryptographic hash written inline (no dependency).

`featureValue` returns `{ value: "unknown", reason: "feature_unqualified" }` unless the feature is `qualified`, in which case it returns `{ value: read() }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/content/adapter.test.ts`
Expected: PASS, four tests.

- [ ] **Step 5: Commit**

```bash
git add src/content/adapter.ts src/content/adapter.test.ts
git commit -m "feat: qualify chatgpt adapter features independently"
```

---

### Task 6: Stop-cause classification with separate cause quality

**Files:**
- Create: `src/core/stopCause.ts`
- Test: `src/core/stopCause.test.ts`

**Interfaces:**
- Consumes: `StopCause`, `CauseQuality`, adapter qualification shape (as a plain input type, not an import from `src/content`).
- Produces: `classifyStop(input: StopObservation): { cause: StopCause; quality: CauseQuality; evidence: string[] }`.

`unknown` is an ordinary outcome and is expected to be the plurality. The classifier is enrichment; time, attribution and machine facts are the substrate.

- [ ] **Step 1: Write the failing test**

```ts
describe("classifyStop", () => {
  it("reports rate_limit explicitly when a qualified signal is present", () => {
    const out = classifyStop({ features: { rateLimitSignal: "qualified", networkErrorSignal: "qualified" }, rateLimited: true, networkError: false, nativeContinueVisible: false, lastMessageEndsMidSentence: false });
    expect(out).toMatchObject({ cause: "rate_limit", quality: "explicit" });
  });

  it("reports unknown with unavailable quality when the deciding feature is unqualified", () => {
    const out = classifyStop({ features: { rateLimitSignal: "drifted", networkErrorSignal: "qualified" }, rateLimited: "unknown", networkError: false, nativeContinueVisible: false, lastMessageEndsMidSentence: false });
    expect(out).toMatchObject({ cause: "unknown", quality: "unavailable" });
  });

  it("infers cutoff from a visible native continue affordance", () => {
    const out = classifyStop({ features: { rateLimitSignal: "qualified", networkErrorSignal: "qualified", nativeContinue: "qualified" }, rateLimited: false, networkError: false, nativeContinueVisible: true, lastMessageEndsMidSentence: true });
    expect(out).toMatchObject({ cause: "cutoff", quality: "explicit" });
  });

  it("never claims normal_turn_end with explicit quality from DOM shape alone", () => {
    const out = classifyStop({ features: { rateLimitSignal: "qualified", networkErrorSignal: "qualified", nativeContinue: "qualified" }, rateLimited: false, networkError: false, nativeContinueVisible: false, lastMessageEndsMidSentence: false });
    expect(out.quality).not.toBe("explicit");
    expect(["normal_turn_end", "unknown"]).toContain(out.cause);
  });

  it("carries evidence strings so a ledger entry can explain itself", () => {
    const out = classifyStop({ features: { rateLimitSignal: "qualified", networkErrorSignal: "qualified" }, rateLimited: true, networkError: false, nativeContinueVisible: false, lastMessageEndsMidSentence: false });
    expect(out.evidence.length).toBeGreaterThan(0);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/core/stopCause.test.ts`
Expected: FAIL — cannot resolve `./stopCause`.

- [ ] **Step 3: Write minimal implementation**

Classify in priority order: a qualified `rateLimitSignal` reading true gives `rate_limit`/`explicit`; a qualified `networkErrorSignal` reading true gives `stream_error`/`explicit`; a qualified `nativeContinue` visible gives `cutoff`/`explicit`; a mid-sentence ending with `nativeContinue` qualified-and-absent gives `cutoff`/`inferred`; a clean ending with every deciding feature qualified gives `normal_turn_end`/`inferred`; anything where a deciding feature is `drifted` or `unavailable` gives `unknown`/`unavailable`. Push one evidence string per rule that fired.

`normal_turn_end` is never `explicit` because no DOM signal distinguishes a model that finished from one that was cut off without an affordance.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/core/stopCause.test.ts`
Expected: PASS, five tests.

- [ ] **Step 5: Commit**

```bash
git add src/core/stopCause.ts src/core/stopCause.test.ts
git commit -m "feat: classify stop cause with separate cause quality"
```

---

### Task 7: Automation turn provenance and manual override

**Files:**
- Create: `src/background/provenance.ts`
- Test: `src/background/provenance.test.ts`

**Interfaces:**
- Produces: `prepareOutboundTurn(deps, watchTaskId): Promise<{ outboundTurnId: string }>`; `confirmOutboundTurn(deps, watchTaskId, outboundTurnId)`; `observeUserMessage(deps, watchTaskId, message): Promise<"adopted" | "manual_override">`.

Provenance needs no model cooperation: the background knows which outbound turns it prepared.

- [ ] **Step 1: Write the failing test**

```ts
describe("turn provenance", () => {
  it("adopts an outbound turn the background prepared and confirmed", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed" }]);
    const { outboundTurnId } = await prepareOutboundTurn(deps, "w1");
    await confirmOutboundTurn(deps, "w1", outboundTurnId);
    expect(await observeUserMessage(deps, "w1", { outboundTurnId })).toBe("adopted");
  });

  it("treats a user message with no prepared outbound as manual override and disarms", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed" }]);
    expect(await observeUserMessage(deps, "w1", {})).toBe("manual_override");
    const record = await deps.store.readWatch("w1");
    expect(record.watchState).toBe("disarmed");
  });

  it("records an attention entry naming the human as the cause", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed" }]);
    await observeUserMessage(deps, "w1", {});
    const entries = await deps.ledger.list();
    expect(entries[0]?.reason).toBe("manual_override");
  });

  it("does not adopt a turn whose outbound id was never confirmed", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed" }]);
    const { outboundTurnId } = await prepareOutboundTurn(deps, "w1");
    expect(await observeUserMessage(deps, "w1", { outboundTurnId })).toBe("manual_override");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/background/provenance.test.ts`
Expected: FAIL — cannot resolve `./provenance`.

- [ ] **Step 3: Write minimal implementation**

Store `outboundTurn: { id, state: "prepared" | "committed" }` in the watch record. `observeUserMessage` adopts only when the message carries an id matching a `committed` outbound turn; otherwise it records a `manual_override` attention entry and writes the record with `watchState: "disarmed"`. The watch must not adopt the turn into an automated lineage, because the browser has no authority to decide whether a human follow-up is the same logical task.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/background/provenance.test.ts`
Expected: PASS, four tests.

- [ ] **Step 5: Commit**

```bash
git add src/background/provenance.ts src/background/provenance.test.ts
git commit -m "feat: bind assistant turns to background-prepared outbound provenance"
```

---

### Task 8: Attention Ledger with cross-store ordering and idempotency

**Files:**
- Create: `src/core/attention.ts`, `src/background/ledger.ts`
- Test: `src/background/ledger.test.ts`

**Interfaces:**
- Consumes: `AttentionReason`, `StopCause`, `CauseQuality`.
- Produces: `attentionKey(input): string`; `makeLedger(browser)` with `record(entry)`, `list()`, `acknowledge(id)`, `adjudicate(id, verdict)`, `metrics()`.

Ordering is normative because there is no atomic commit across `storage.local` and `storage.session`.

- [ ] **Step 1: Write the failing test**

```ts
describe("attention ledger", () => {
  it("derives a deterministic idempotency key from the triggering facts", () => {
    const input = { watchTaskId: "w1", controllerGeneration: 7, observationEpoch: 3, reason: "controller_lost" as const, observationId: "obs-9" };
    expect(attentionKey(input)).toBe(attentionKey({ ...input }));
    expect(attentionKey(input)).not.toBe(attentionKey({ ...input, controllerGeneration: 8 }));
  });

  it("dedupes a re-emitted entry after a background restart", async () => {
    const fake = makeFakeBrowser();
    const ledger = makeLedger(fake);
    const entry = { watchTaskId: "w1", controllerGeneration: 7, observationEpoch: 3, reason: "controller_lost" as const, observationId: "obs-9" };
    await ledger.record(entry);
    await ledger.record(entry);
    expect((await ledger.list()).length).toBe(1);
  });

  it("survives a simulated browser restart because it lives in storage.local", async () => {
    const fake = makeFakeBrowser();
    const ledger = makeLedger(fake);
    await ledger.record({ watchTaskId: "w1", controllerGeneration: 1, observationEpoch: 1, reason: "stop_detected", observationId: "o1" });
    fake.shutdownBrowser();
    expect((await makeLedger(fake).list()).length).toBe(1);
  });

  it("keeps adjudication separate from observation", async () => {
    const fake = makeFakeBrowser();
    const ledger = makeLedger(fake);
    await ledger.record({ watchTaskId: "w1", controllerGeneration: 1, observationEpoch: 1, reason: "stop_detected", observationId: "o1", observation: { stopCause: "unknown", causeQuality: "unavailable" } });
    const [entry] = await ledger.list();
    await ledger.adjudicate(entry!.attentionId, { verdict: "wrong", actualCause: "rate_limit" });
    const [after] = await ledger.list();
    expect(after!.observation.stopCause).toBe("unknown");
    expect(after!.adjudication).toMatchObject({ verdict: "wrong", actualCause: "rate_limit" });
  });

  it("never reports accuracy without adjudication coverage", async () => {
    const fake = makeFakeBrowser();
    const ledger = makeLedger(fake);
    for (const id of ["o1", "o2", "o3", "o4"]) {
      await ledger.record({ watchTaskId: "w1", controllerGeneration: 1, observationEpoch: 1, reason: "stop_detected", observationId: id, observation: { stopCause: "unknown", causeQuality: "unavailable" } });
    }
    const entries = await ledger.list();
    await ledger.adjudicate(entries[0]!.attentionId, { verdict: "correct" });
    const metrics = await ledger.metrics();
    expect(metrics.classifiedRecords).toBe(4);
    expect(metrics.adjudicatedRecords).toBe(1);
    expect(metrics.adjudicationCoverage).toBeCloseTo(0.25);
    expect(metrics).toHaveProperty("selectionBiasCaveat");
    expect(Object.keys(metrics)).not.toContain("accuracy");
    expect(metrics.accuracyOnAdjudicated).toBeCloseTo(1);
  });

  it("holds no execution authority", async () => {
    const fake = makeFakeBrowser();
    const ledger = makeLedger(fake);
    expect(Object.keys(ledger)).not.toContain("continue");
    expect(Object.keys(ledger)).not.toContain("actuate");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/background/ledger.test.ts`
Expected: FAIL — cannot resolve `../core/attention`.

- [ ] **Step 3: Write minimal implementation**

`attentionKey` concatenates `watchTaskId`, `controllerGeneration`, `observationEpoch`, `reason`, and `observationId` with a separator that cannot appear in any of them, then hashes to hex. `record` reads the ledger array from `storage.local`, returns early when the key already exists, appends otherwise, and writes once. `metrics` returns `{ classifiedRecords, adjudicatedRecords, adjudicationCoverage, accuracyOnAdjudicated, perCauseConfusion, selectionBiasCaveat }` and deliberately exposes no bare `accuracy` field, so a caller cannot report a rate without its denominator.

Document the ordering rule at the top of `ledger.ts`, since callers must follow it:

```ts
// Cross-store commit ordering is normative: the ledger record (storage.local)
// is written BEFORE the watch authority record (storage.session), because the
// two stores have no atomic commit between them. A termination between the two
// leaves the watch still armed, so the next wake re-evaluates and re-emits,
// and the deterministic key makes that re-emission a no-op. The reverse order
// would produce a disarmed watch with no ledger entry — a silently unwatched
// conversation, which is the failure this whole subsystem exists to prevent.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/background/ledger.test.ts`
Expected: PASS, six tests.

- [ ] **Step 5: Commit**

```bash
git add src/core/attention.ts src/background/ledger.ts src/background/ledger.test.ts
git commit -m "feat: add durable attention ledger with adjudication"
```

---

### Task 9: Native bridge client and bootstrap outcomes

**Files:**
- Create: `src/background/nativeBridge.ts`
- Test: `src/background/nativeBridge.test.ts`

**Interfaces:**
- Produces: `HOST_NAME = "com.shellbeam.browser_bridge"`; `PROTOCOL_VERSION = 1`; `bridgeHello(browser): Promise<BootstrapOutcome>` where `BootstrapOutcome` is `{ kind: "ready" } | { kind: "host_absent" } | { kind: "daemon_unreachable" } | { kind: "protocol_incompatible"; hostSupports: number[] }`; `bridgeRead(browser, verb, params): Promise<BridgeResult>`.

- [ ] **Step 1: Write the failing test**

```ts
describe("native bridge", () => {
  it("reports host_absent when the spawn itself fails", async () => {
    const fake = makeFakeBrowser();
    fake.runtime.failNativeMessage(new Error("No such native application com.shellbeam.browser_bridge"));
    expect(await bridgeHello(fake)).toEqual({ kind: "host_absent" });
  });

  it("reports daemon_unreachable from a hello reply", async () => {
    const fake = makeFakeBrowser();
    fake.runtime.queueNativeReply({ protocol_version: 1, supported_versions: [1], verb: "hello", status: "daemon_unreachable" });
    expect(await bridgeHello(fake)).toEqual({ kind: "daemon_unreachable" });
  });

  it("distinguishes protocol incompatibility and keeps the supported set", async () => {
    const fake = makeFakeBrowser();
    fake.runtime.queueNativeReply({ protocol_version: 2, supported_versions: [2, 3], verb: "hello", status: "protocol_incompatible" });
    expect(await bridgeHello(fake)).toEqual({ kind: "protocol_incompatible", hostSupports: [2, 3] });
  });

  it("sends only closed-enum verbs and never an action or command field", async () => {
    const fake = makeFakeBrowser();
    fake.runtime.queueNativeReply({ protocol_version: 1, supported_versions: [1], verb: "activity_facts", status: "ok", coverage: { compacted_operations: 0, truncated: false } });
    await bridgeRead(fake, "activity_facts", { correlationId: "wt" });
    const sent = fake.runtime.sentNativeMessages()[0];
    expect(sent).toEqual({ protocol_version: 1, verb: "activity_facts", correlation_id: "wt" });
    expect(Object.keys(sent)).not.toContain("action");
    expect(Object.keys(sent)).not.toContain("command");
  });

  it("rejects an unknown verb before it reaches the host", async () => {
    const fake = makeFakeBrowser();
    await expect(bridgeRead(fake, "start" as never, { correlationId: "wt" })).rejects.toThrow(/verb/);
    expect(fake.runtime.sentNativeMessages().length).toBe(0);
  });

  it("treats facts_unavailable as absence of facts, never as settled", async () => {
    const fake = makeFakeBrowser();
    fake.runtime.queueNativeReply({ protocol_version: 1, supported_versions: [1], verb: "activity_facts", status: "facts_unavailable", reason: "activity_not_found" });
    const result = await bridgeRead(fake, "activity_facts", { correlationId: "wt" });
    expect(result.status).toBe("facts_unavailable");
    expect(result).not.toHaveProperty("settled");
    expect(result).not.toHaveProperty("idle");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/background/nativeBridge.test.ts`
Expected: FAIL — cannot resolve `./nativeBridge`.

- [ ] **Step 3: Write minimal implementation**

`bridgeHello` calls `browser.runtime.sendNativeMessage(HOST_NAME, { protocol_version: PROTOCOL_VERSION, verb: "hello" })`; a rejected promise is `host_absent`, because a missing manifest or binary means Firefox never spawned a process and no reply can carry the diagnosis. A reply with `status: "protocol_incompatible"` yields that kind with the host's `supported_versions`; `daemon_unreachable` maps straight through; anything else with `status: "ok"` is `ready`.

`bridgeRead` validates the verb against a local closed tuple before sending, builds the request with exactly `protocol_version`, `verb`, `correlation_id`, and optionally `cursor`, and returns the parsed reply unchanged. It never derives a judgment; the caller applies policy.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/background/nativeBridge.test.ts`
Expected: PASS, six tests.

- [ ] **Step 5: Commit**

```bash
git add src/background/nativeBridge.ts src/background/nativeBridge.test.ts
git commit -m "feat: add read-only native bridge client with bootstrap outcomes"
```

---

### Task 10: Admission limiter and acquire-then-revalidate ordering

**Files:**
- Create: `src/background/admission.ts`
- Test: `src/background/admission.test.ts`

**Interfaces:**
- Produces: `MAX_INFLIGHT_NATIVE_READS = 2`; `PER_WATCH_MIN_INTERVAL_MS = 1500`; `makeAdmission(deps)` with `runNativeRead<T>(watchTaskId, revalidate: () => Promise<Revalidation>, read: (r: Revalidation) => Promise<T>): Promise<T | "rejected">`; `jitterFor(watchTaskId, windowMs): number`.

Admission lives here because a connectionless host holds no state and the ShellBeam daemon has no read-path budget, so this is the only place a bound can exist.

- [ ] **Step 1: Write the failing test**

```ts
describe("admission", () => {
  it("caps concurrent native reads", async () => {
    const admission = makeAdmission(deps());
    let inflight = 0;
    let peak = 0;
    const run = (id: string) => admission.runNativeRead(id, async () => ({ ok: true }), async () => {
      inflight += 1; peak = Math.max(peak, inflight);
      await Promise.resolve();
      inflight -= 1;
      return "done";
    });
    await Promise.all(["a", "b", "c", "d"].map(run));
    expect(peak).toBeLessThanOrEqual(MAX_INFLIGHT_NATIVE_READS);
  });

  it("revalidates AFTER acquiring the slot, not before", async () => {
    const order: string[] = [];
    const admission = makeAdmission(deps());
    let release: () => void = () => {};
    const blocker = admission.runNativeRead("blocker", async () => ({ ok: true }), () => new Promise((resolve) => { release = () => resolve("x"); }));
    const second = admission.runNativeRead("blocker2", async () => ({ ok: true }), async () => "y");
    const third = admission.runNativeRead("w3", async () => { order.push("revalidate"); return { ok: true }; }, async () => { order.push("read"); return "z"; });
    release();
    await Promise.all([blocker, second, third]);
    expect(order).toEqual(["revalidate", "read"]);
    expect(order.indexOf("revalidate")).toBeGreaterThanOrEqual(0);
  });

  it("rejects the read when revalidation fails after the wait", async () => {
    const admission = makeAdmission(deps());
    const result = await admission.runNativeRead("w1", async () => ({ ok: false, reason: "observation_stale" }), async () => "never");
    expect(result).toBe("rejected");
  });

  it("enforces a per-watch minimum interval", async () => {
    const clock = { now: 0 };
    const admission = makeAdmission(deps({ now: () => clock.now }));
    await admission.runNativeRead("w1", async () => ({ ok: true }), async () => "first");
    const immediate = await admission.runNativeRead("w1", async () => ({ ok: true }), async () => "second");
    expect(immediate).toBe("rejected");
    clock.now += PER_WATCH_MIN_INTERVAL_MS;
    expect(await admission.runNativeRead("w1", async () => ({ ok: true }), async () => "third")).toBe("third");
  });

  it("spreads wakes deterministically per watch", () => {
    const a = jitterFor("w1", 4000);
    expect(jitterFor("w1", 4000)).toBe(a);
    expect(a).toBeGreaterThanOrEqual(0);
    expect(a).toBeLessThan(4000);
    expect(jitterFor("w2", 4000)).not.toBe(a);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/background/admission.test.ts`
Expected: FAIL — cannot resolve `./admission`.

- [ ] **Step 3: Write minimal implementation**

Implement a small counting semaphore with a FIFO waiter queue. `runNativeRead` acquires a slot first, then calls `revalidate()`, then `read()`. Document why:

```ts
// Ordering is normative: acquire the slot, THEN reload and revalidate.
// Validating first and waiting second means a queued request executes on
// assumptions that expired while it waited. Revalidating after acquisition
// lets an expired request be rejected instead.
```

`jitterFor` hashes the watch id to a number in `[0, windowMs)` with the same inline hash used by the adapter fingerprint. Per-watch interval state is read from and written to the watch record's `lastNativeReadAt`, so it survives a background unload.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/background/admission.test.ts`
Expected: PASS, five tests.

- [ ] **Step 5: Commit**

```bash
git add src/background/admission.ts src/background/admission.test.ts
git commit -m "feat: bound native reads with acquire-then-revalidate admission"
```

---

### Task 11: Correlation handshake and staged counters

**Files:**
- Create: `src/core/correlation.ts`
- Test: `src/core/correlation.test.ts`

**Interfaces:**
- Produces: `mintBinding(watchTaskId, random): { activityHint: string; bindingNonce: string }`; `classifyDeclaration(input): CorrelationState`; `promoteWithFacts(state, facts, turnWindowMs, now): CorrelationState`; `stagedCounters(records): StagedCounters`; `derivedRatios(counters): { bindingAdoption; bindingDeliverySuccess; declarationSuccess; currentCorrelationYield }`.

- [ ] **Step 1: Write the failing test**

```ts
describe("correlation", () => {
  it("requires a structured declaration carrying the current nonce", () => {
    expect(classifyDeclaration({ declared: [{ activityHint: "sb-wt-1", nonce: "n1" }], currentNonce: "n1" })).toBe("declared");
    expect(classifyDeclaration({ declared: [], currentNonce: "n1" })).toBe("unbound");
  });

  it("treats a superseded nonce as stale, never as declared", () => {
    expect(classifyDeclaration({ declared: [{ activityHint: "sb-wt-1", nonce: "n0" }], currentNonce: "n1" })).toBe("stale");
  });

  it("treats conflicting hints against one nonce as ambiguous and picks neither", () => {
    expect(classifyDeclaration({ declared: [{ activityHint: "sb-wt-1", nonce: "n1" }, { activityHint: "sb-wt-2", nonce: "n1" }], currentNonce: "n1" })).toBe("ambiguous");
  });

  it("does not reach current on declaration alone", () => {
    expect(promoteWithFacts("declared", { found: false }, 60_000, 1000)).toBe("declared");
  });

  it("reaches current only with existence and recency inside the turn window", () => {
    expect(promoteWithFacts("declared", { found: true, latestOperationAt: 100_000 }, 60_000, 120_000)).toBe("current");
    expect(promoteWithFacts("declared", { found: true, latestOperationAt: 1_000 }, 60_000, 120_000)).toBe("stale");
  });

  it("reports four adjacent-stage ratios and never one coverage figure", () => {
    const counters = stagedCounters([
      { eligible: true, delivered: true, declared: true, current: true },
      { eligible: true, delivered: true, declared: true, current: false },
      { eligible: true, delivered: false, declared: false, current: false },
      { eligible: false, delivered: false, declared: false, current: false },
    ]);
    expect(counters).toMatchObject({ watchTotal: 4, bindingEligible: 3, bindingInstructionDelivered: 2, bindingDeclared: 2, bindingCurrent: 1 });
    const ratios = derivedRatios(counters);
    expect(ratios.bindingAdoption).toBeCloseTo(3 / 4);
    expect(ratios.bindingDeliverySuccess).toBeCloseTo(2 / 3);
    expect(ratios.declarationSuccess).toBeCloseTo(2 / 2);
    expect(ratios.currentCorrelationYield).toBeCloseTo(1 / 2);
    expect(Object.keys(ratios)).not.toContain("correlationCoverage");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/core/correlation.test.ts`
Expected: FAIL — cannot resolve `./correlation`.

- [ ] **Step 3: Write minimal implementation**

`stagedCounters` counts each stage independently; `derivedRatios` divides adjacent stages only. Document the reason inline:

```ts
// Four ratios, not three. bindingEligible and bindingInstructionDelivered are
// separate stages, so delivered/watchTotal would recombine UX adoption with
// delivery reliability and reproduce the conflation these counters exist to
// prevent. There is deliberately no single "coverage" figure.
```

`promoteWithFacts` returns `current` only when the facts report `found` and `latestOperationAt` falls inside `turnWindowMs` of `now`; a found-but-old activity is `stale`, because an activity persists forever after a single operation.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/core/correlation.test.ts`
Expected: PASS, six tests.

- [ ] **Step 5: Commit**

```bash
git add src/core/correlation.ts src/core/correlation.test.ts
git commit -m "feat: stage correlation handshake and adjacent-stage ratios"
```

---

### Task 12: Comparable failure surface

**Files:**
- Create: `src/core/comparability.ts`
- Test: `src/core/comparability.test.ts`

**Interfaces:**
- Produces: `compareSurfaces(a, b): "same_signature" | "different_signature" | "incomparable" | "insufficient_coverage"`; `signatureOf(surface): string`.

- [ ] **Step 1: Write the failing test**

```ts
describe("compareSurfaces", () => {
  const base = { producerIdentity: "pytest", structuredAdapter: "pytest-junit-xml@1", authority: "mechanical", derivationMethod: "deterministic_normalization", invocationQualification: "q1", coverageScope: "tests/", cases: [{ name: "a", status: "fail" }, { name: "b", status: "fail" }] };

  it("reports same_signature for identical comparable surfaces", () => {
    expect(compareSurfaces(base, { ...base })).toBe("same_signature");
  });

  it("reports incomparable when the producer differs", () => {
    expect(compareSurfaces(base, { ...base, producerIdentity: "vitest" })).toBe("incomparable");
  });

  it("reports incomparable when authority or derivation differs", () => {
    expect(compareSurfaces(base, { ...base, authority: "advisory" })).toBe("incomparable");
    expect(compareSurfaces(base, { ...base, derivationMethod: "heuristic_extraction" })).toBe("incomparable");
  });

  it("reports incomparable when coverage scope differs even if failing sets coincide", () => {
    expect(compareSurfaces(base, { ...base, coverageScope: "tests/a" })).toBe("incomparable");
  });

  it("reports different_signature when the failing set changes under equal comparability", () => {
    expect(compareSurfaces(base, { ...base, cases: [{ name: "a", status: "fail" }] })).toBe("different_signature");
  });

  it("reports insufficient_coverage when either side was truncated", () => {
    expect(compareSurfaces(base, { ...base, truncated: true })).toBe("insufficient_coverage");
  });

  it("never returns a progress or stuck judgment", () => {
    const outcomes = new Set(["same_signature", "different_signature", "incomparable", "insufficient_coverage"]);
    expect(outcomes.has(compareSurfaces(base, { ...base }))).toBe(true);
    expect(outcomes.has("no_progress" as never)).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/core/comparability.test.ts`
Expected: FAIL — cannot resolve `./comparability`.

- [ ] **Step 3: Write minimal implementation**

Compare the six comparability fields first and return `incomparable` on any mismatch; return `insufficient_coverage` when either side is truncated; otherwise compare sorted `name:package:status` signatures. Document the naming rule inline: the counter this feeds is `repeatFailureSignatureCount`, never `noProgressCount`, because between two identical failing surfaces the model may have refactored, fixed other tests, or found the root cause.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/core/comparability.test.ts`
Expected: PASS, seven tests.

- [ ] **Step 5: Commit**

```bash
git add src/core/comparability.ts src/core/comparability.test.ts
git commit -m "feat: compare failure surfaces only when homogeneous"
```

---

### Task 13: Human-gated actuator

**Files:**
- Create: `src/background/actuator.ts`
- Test: `src/background/actuator.test.ts`

**Interfaces:**
- Consumes: store, locks, liveness, adapter qualification, provenance.
- Produces: `actuate(deps, { watchTaskId, action, humanGesture }): Promise<ActuationResult>` where `ActuationResult` is `{ kind: "actuated"; outboundTurnId?: string } | { kind: "refused"; reason: RefusalReason }`.

A human gesture bypasses decision policy. It does not bypass mechanical safety.

- [ ] **Step 1: Write the failing test**

```ts
describe("actuate", () => {
  it("refuses without a human gesture", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", controller: true, observationEpoch: 4 }]);
    expect(await actuate(deps, { watchTaskId: "w1", action: "send_continuation", humanGesture: false })).toEqual({ kind: "refused", reason: "no_human_gesture" });
  });

  it("refuses when the observation epoch is stale even with a human gesture", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", controller: true, observationEpoch: 4, liveObservationEpoch: 9 }]);
    expect(await actuate(deps, { watchTaskId: "w1", action: "send_continuation", humanGesture: true })).toEqual({ kind: "refused", reason: "observation_stale" });
  });

  it("refuses when the conversation identity no longer matches", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", controller: true, conversationKey: "C1", liveConversationKey: "C2" }]);
    expect(await actuate(deps, { watchTaskId: "w1", action: "send_continuation", humanGesture: true })).toEqual({ kind: "refused", reason: "conversation_mismatch" });
  });

  it("refuses send_continuation when composer or send_action is unqualified", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", controller: true, features: { composer: "drifted", sendAction: "qualified" } }]);
    expect(await actuate(deps, { watchTaskId: "w1", action: "send_continuation", humanGesture: true })).toEqual({ kind: "refused", reason: "feature_unqualified" });
  });

  it("refuses native_continue after a reload because the affordance is ephemeral", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", controller: true, documentEpochAtObservation: 3, liveDocumentEpoch: 4, features: { nativeContinue: "qualified" } }]);
    expect(await actuate(deps, { watchTaskId: "w1", action: "native_continue", humanGesture: true })).toEqual({ kind: "refused", reason: "native_continue_expired" });
  });

  it("actuates send_continuation when every precondition holds and records provenance", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", controller: true, observationEpoch: 4, liveObservationEpoch: 4, conversationKey: "C1", liveConversationKey: "C1", features: { composer: "qualified", sendAction: "qualified" } }]);
    const result = await actuate(deps, { watchTaskId: "w1", action: "send_continuation", humanGesture: true });
    expect(result.kind).toBe("actuated");
    const record = await deps.store.readWatch("w1");
    expect(record.outboundTurn?.state).toBe("prepared");
    expect(record.telemetry.humanActionCount).toBe(1);
  });

  it("never refuses on an arbitrary retry count in P0", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", controller: true, observationEpoch: 4, liveObservationEpoch: 4, conversationKey: "C1", liveConversationKey: "C1", features: { composer: "qualified", sendAction: "qualified" }, telemetry: { humanActionCount: 999 } }]);
    const result = await actuate(deps, { watchTaskId: "w1", action: "send_continuation", humanGesture: true });
    expect(result.kind).toBe("actuated");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/background/actuator.test.ts`
Expected: FAIL — cannot resolve `./actuator`.

- [ ] **Step 3: Write minimal implementation**

Check in order: human gesture present; controller confirmed under `withGlobalTransitionLock`; record reloaded; conversation identity matches the live ping; `observationEpoch` matches the live epoch; required features qualified for the chosen action; for `native_continue` only, the live `documentEpoch` equals the epoch at which the affordance was observed. Then prepare the outbound turn, increment `humanActionCount`, and dispatch to the content script. Every failure returns a typed refusal, never a best-effort attempt.

Document why the counter cannot refuse:

```ts
// Telemetry counters never refuse an action in P0. A budget that can reject a
// human's eleventh click already holds decision authority, which I5 reserves
// for the human. Mechanical checks above may refuse, because they are
// statements about whether the action can be performed correctly.
```

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/background/actuator.test.ts`
Expected: PASS, seven tests.

- [ ] **Step 5: Commit**

```bash
git add src/background/actuator.ts src/background/actuator.test.ts
git commit -m "feat: gate actuation on a human gesture and mechanical preconditions"
```

---

### Task 14: Deadlines, wiring, and the inbox

**Files:**
- Create: `src/background/deadlines.ts`, `src/background/index.ts`, `src/content/observe.ts`, `src/ui/inbox.ts`, `inbox.html`
- Test: `src/background/deadlines.test.ts`, `src/ui/inbox.test.ts`

**Interfaces:**
- Produces: `scheduleIdleGrace(deps, watchTaskId, graceMs)`; `onAlarmFired(deps, alarmName)`; `renderInbox(entries, metrics): InboxModel`.

- [ ] **Step 1: Write the failing test**

```ts
describe("deadlines", () => {
  it("stores an authoritative deadline and wakes through alarms, not setTimeout", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", generationState: "stopped" }]);
    await scheduleIdleGrace(deps, "w1", 12_000);
    const record = await deps.store.readWatch("w1");
    expect(record.deadlines.idleDeadlineAt).toBe(deps.now() + 12_000);
    expect(deps.fake.alarms.created().map((a) => a.name)).toContain("watch:w1");
  });

  it("re-reads the record on wake and does nothing before the authoritative deadline", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", generationState: "stopped" }]);
    await scheduleIdleGrace(deps, "w1", 12_000);
    deps.setNow(deps.now() + 1_000);
    await onAlarmFired(deps, "watch:w1");
    expect((await deps.ledger.list()).length).toBe(0);
  });

  it("records attention once the authoritative deadline has passed", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "armed", generationState: "stopped" }]);
    await scheduleIdleGrace(deps, "w1", 12_000);
    deps.setNow(deps.now() + 12_000);
    await onAlarmFired(deps, "watch:w1");
    const entries = await deps.ledger.list();
    expect(entries[0]?.reason).toBe("stop_detected");
  });
});

describe("inbox", () => {
  it("shows a per-reason count and never a bare accuracy figure", () => {
    const model = renderInbox([
      { attentionId: "1", reason: "controller_lost" },
      { attentionId: "2", reason: "rate_limited" },
      { attentionId: "3", reason: "rate_limited" },
    ], { classifiedRecords: 3, adjudicatedRecords: 0, adjudicationCoverage: 0, accuracyOnAdjudicated: 0, selectionBiasCaveat: "…" });
    expect(model.badgeCount).toBe(3);
    expect(model.byReason.rate_limited).toBe(2);
    expect(model.metricsLine).toContain("0/3 adjudicated");
    expect(model.metricsLine).not.toMatch(/accuracy \d/);
  });

  it("offers an adjudication control on every entry", () => {
    const model = renderInbox([{ attentionId: "1", reason: "stop_detected" }], { classifiedRecords: 1, adjudicatedRecords: 0, adjudicationCoverage: 0, accuracyOnAdjudicated: 0, selectionBiasCaveat: "…" });
    expect(model.rows[0]?.adjudicationOptions).toEqual(["correct", "wrong", "unclear"]);
  });

  it("labels a manual_override entry as caused by the human typing", () => {
    const model = renderInbox([{ attentionId: "1", reason: "manual_override" }], { classifiedRecords: 1, adjudicatedRecords: 0, adjudicationCoverage: 0, accuracyOnAdjudicated: 0, selectionBiasCaveat: "…" });
    expect(model.rows[0]?.label).toMatch(/you typed/i);
    expect(model.rows[0]?.actions).toContain("re-arm");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/background/deadlines.test.ts src/ui/inbox.test.ts`
Expected: FAIL — cannot resolve `./deadlines`.

- [ ] **Step 3: Write minimal implementation**

`scheduleIdleGrace` writes `deadlines.idleDeadlineAt = now + graceMs` into the record with one `set` and creates `browser.alarms.create("watch:" + id, { delayInMinutes: (graceMs + jitterFor(id, 4000)) / 60000 })`. `onAlarmFired` reloads the record and compares `now()` against the stored deadline, treating the alarm purely as a wake signal — Firefox may fire late, and lateness must cost throughput, never correctness.

`renderInbox` returns a badge count, per-reason counts, one metrics line that always shows the denominator, and per-row adjudication options plus a `re-arm` action for `manual_override` and `controller_lost`. A `manual_override` row states plainly that the watch stopped because the human typed, so a correct-but-annoying rule is not mistaken for a bug.

`src/background/index.ts` contains wiring only: build the store, locks, ledger, admission, register the alarm and message listeners, and delegate. Any logic belongs in the modules above.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run`
Expected: PASS, the whole suite.

- [ ] **Step 5: Commit**

```bash
git add src/background/deadlines.ts src/background/index.ts src/content/observe.ts src/ui inbox.html src/background/deadlines.test.ts src/ui/inbox.test.ts
git commit -m "feat: wake through alarms and surface the attention inbox"
```

---

### Task 15: The resync transition to armed

**Files:**
- Create: `src/background/resync.ts`
- Test: `src/background/resync.test.ts`

**Interfaces:**
- Consumes: store, locks, liveness, adapter qualification, `WatchRecord`.
- Produces: `resync(deps, watchTaskId): Promise<ResyncResult>` where `ResyncResult` is `{ kind: "armed"; observationEpoch: number } | { kind: "failed"; reason: "reload_failed" | "conversation_mismatch" | "adapter_unqualified" }`.

This completes the transition Task 4 begins. Task 4 writes `watchState: "resync_required"` and stops; nothing may actuate from that state, so this task is what makes a re-armed watch usable. Authority freshness and observation freshness are orthogonal: holding a current `controller_generation` proves nothing about whether the view being acted on is fresh, which is why a new controller must establish an observation epoch before reaching `armed`.

- [ ] **Step 1: Write the failing test**

```ts
describe("resync", () => {
  it("forces a reload before establishing an observation epoch", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "resync_required", controller: true, conversationKey: "C1", liveConversationKey: "C1", features: allQualified() }]);
    const result = await resync(deps, "w1");
    expect(result.kind).toBe("armed");
    expect(deps.fake.tabs.reloadCalls()).toEqual([17]);
    const order = deps.fake.callOrder();
    expect(order.indexOf("tabs.reload")).toBeLessThan(order.indexOf("tabs.sendMessage:observe"));
  });

  it("advances the observation epoch and reaches armed", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "resync_required", controller: true, observationEpoch: 4, conversationKey: "C1", liveConversationKey: "C1", features: allQualified() }]);
    const result = await resync(deps, "w1");
    expect(result).toMatchObject({ kind: "armed" });
    const record = await deps.store.readWatch("w1");
    expect(record.watchState).toBe("armed");
    expect(record.observationEpoch).toBeGreaterThan(4);
  });

  it("fails closed when the reloaded tab shows a different conversation", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "resync_required", controller: true, conversationKey: "C1", liveConversationKey: "C2", features: allQualified() }]);
    expect(await resync(deps, "w1")).toEqual({ kind: "failed", reason: "conversation_mismatch" });
    const record = await deps.store.readWatch("w1");
    expect(record.watchState).not.toBe("armed");
  });

  it("fails closed when the adapter cannot qualify a required feature after reload", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "resync_required", controller: true, conversationKey: "C1", liveConversationKey: "C1", features: { ...allQualified(), composer: "unavailable" } }]);
    expect(await resync(deps, "w1")).toEqual({ kind: "failed", reason: "adapter_unqualified" });
  });

  it("records attention and stays out of armed when resync fails", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "resync_required", controller: true, conversationKey: "C1", liveConversationKey: "C2", features: allQualified() }]);
    await resync(deps, "w1");
    const entries = await deps.ledger.list();
    expect(entries[0]?.reason).toBe("resync_failed");
  });

  it("never carries a native_continue affordance across the reload", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "resync_required", controller: true, conversationKey: "C1", liveConversationKey: "C1", features: allQualified(), nativeContinueObservedAtDocumentEpoch: 3 }]);
    await resync(deps, "w1");
    const record = await deps.store.readWatch("w1");
    expect(record.nativeContinueObservedAtDocumentEpoch).toBeUndefined();
  });

  it("writes exactly one watch record", async () => {
    const deps = await harness([{ id: "w1", tabId: 17, watchState: "resync_required", controller: true, conversationKey: "C1", liveConversationKey: "C1", features: allQualified() }]);
    deps.fake.resetWriteCount();
    await resync(deps, "w1");
    expect(deps.fake.sessionWriteCount()).toBe(1);
  });
});
```

Add `allQualified()` to the test file returning every one of the eight adapter features set to `"qualified"`, and extend the fake with `tabs.reloadCalls()` and `callOrder()` recorders.

- [ ] **Step 2: Run test to verify it fails**

Run: `npx vitest run src/background/resync.test.ts`
Expected: FAIL — cannot resolve `./resync`.

- [ ] **Step 3: Write minimal implementation**

```ts
// src/background/resync.ts
//
// A new controller holds valid authority and a stale view. Fencing on
// controller_generation proves who may act; it proves nothing about whether the
// information being acted on is current. An observer tab does not receive the
// streaming updates of another tab's generation, so its DOM can show an old
// state that looks exactly like "stopped and incomplete".
//
// The reload is not a convenience. Only a fresh document proves the view is
// current, and the native-continue affordance is client state that a reload
// necessarily destroys — so it is cleared here rather than carried forward.
export const resync = async (deps: Deps, watchTaskId: string): Promise<ResyncResult> =>
  deps.locks.withWatchLock(watchTaskId, async () => {
    const record = await deps.store.readWatch(watchTaskId);
    await deps.browser.tabs.reload(record.tabId, { bypassCache: false });
    const observed = await deps.observeAfterReload(record.tabId, deps.resyncTimeoutMs);
    const fail = async (reason: ResyncFailure) => {
      await deps.ledger.record({
        reason: "resync_failed", watchTaskId, conversationKey: record.conversationKey,
        controllerGeneration: record.controllerGeneration, observationEpoch: record.observationEpoch,
        observationId: `resync:${reason}`,
      });
      await deps.store.writeWatch({ ...record, watchState: "attention_required", nativeContinueObservedAtDocumentEpoch: undefined });
      return { kind: "failed" as const, reason };
    };
    if (!observed) return fail("reload_failed");
    if (observed.conversationKey !== record.conversationKey) return fail("conversation_mismatch");
    if (REQUIRED_FEATURES.some((f) => observed.features[f] !== "qualified")) return fail("adapter_unqualified");

    const observationEpoch = record.observationEpoch + 1;
    await deps.store.writeWatch({
      ...record,
      watchState: "armed",
      generationState: observed.generationState,
      adapterState: observed.adapterState,
      observationEpoch,
      nativeContinueObservedAtDocumentEpoch: undefined,
    });
    return { kind: "armed" as const, observationEpoch };
  });

const REQUIRED_FEATURES = ["messageContainer", "assistantLastMessage", "composer", "sendAction"] as const;
```

Note that the failure path writes one record and the success path writes one record; they are alternatives, never both, so the one-record rule holds on every branch.

- [ ] **Step 4: Run test to verify it passes**

Run: `npx vitest run src/background/resync.test.ts`
Expected: PASS, seven tests.

- [ ] **Step 5: Commit**

```bash
git add src/background/resync.ts src/background/resync.test.ts
git commit -m "feat: establish a fresh observation epoch before arming"
```

---

### Task 16: Manual end-to-end verification against a real browser

**Files:**
- Modify: none. This task produces an evidence note.

Unit tests cannot prove the adapter matches the live DOM, which is the one component qualified against a moving target.

- [ ] **Step 1: Build and load the extension**

Run: `npx web-ext run --source-dir . --target firefox-desktop`
Expected: Firefox launches with the extension loaded and no manifest errors.

- [ ] **Step 2: Install the native manifest from ShellBeam**

```bash
shellbeam browser-host install --extension-id=attention-router@shellbeam.local --host-path="$(command -v shellbeam-browser-host)"
```

Then run `shellbeam doctor --json` and confirm the `browser_bridge` check reports `pass` with the pinned id.

- [ ] **Step 3: Qualify the adapter against the live DOM**

Open a ChatGPT conversation, arm a watch, and read the logged qualification matrix. Record which of the eight features qualify. Expect `nativeContinue` and `rateLimitSignal` to be `unavailable` until a real cutoff or rate limit occurs.

- [ ] **Step 4: Exercise the two-tab case**

Open the same conversation in two tabs, arm both, confirm exactly one becomes controller and the other reports `observerOrigin: "demoted"`. Close the controller tab, confirm an attention entry with reason `controller_lost` appears and that the observer is **not** promoted. Re-arm the observer explicitly and confirm it forces a reload before reaching `armed`.

- [ ] **Step 5: Write the evidence note and commit**

Record the observed qualification matrix, the two-tab outcome, and any adapter selector that needed changing in `docs/evidence/2026-08-20-p0-manual-verification.md`, then commit. This note is the first input to the §28 graduation dataset.

---

## Out of scope for this plan

Deferred by the spec, not by convenience:

- Autonomous continuation (spec §27; P1). P0 ships the actuator under human authority so P1 changes only the authorization source.
- Automatic controller takeover and any canonical conversation-lease record (spec §29; P2).
- Completion-marker parsing of any kind, including turn-scoped nonces (spec §17; P1).
- Budget enforcement (spec §24; P1). P0 counters never refuse.
- Durable resume across browser restart (spec §29; P3).
- Any task envelope (spec §29; P4).
- Chrome support. The alarms floor and MV3 background lifetime differ, and the design deliberately does not weaken the Firefox architecture to accommodate Chrome.
