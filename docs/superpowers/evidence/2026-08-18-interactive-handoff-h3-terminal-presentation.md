# H3 Automatic Terminal Presentation — Darwin Completion Evidence

**Status:** H3 automatic terminal presentation is complete for the qualified Darwin/Ghostty lane, subject to the final commit-gate for the exact staged tree containing this evidence.

```text
H2_MANUAL_BASE=PASS
H3_TERMINAL_PRESENTATION=PASS
H3_GHOSTTY_DARWIN=PASS
H3_LINUX=NOT_RUN_NO_QUALIFIED_PROVIDER
H3_H4_COMBINED=NOT_RUN_COUNTERPART_ABSENT
SECRET_HANDOFF_AVAILABLE=false
SHELL_READINESS_AVAILABLE=false
```

H3 is presentation only. H2 remains the authority source and manual fallback. No H4 privacy/readiness behavior is claimed or required by this completion evidence.

## 1. Frozen prerequisites and implementation lineage

Exact H2 base consumed by H3:

```text
H2 completion commit = 1434ac75f71cb8df99b71d208aaf82cbbc87d78e
H2 evidence          = docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-manual-authority.md
H2 evidence sha256   = 6787b0ba1835ceaba43c05202226619c039c9738e3fd35d5494e00203dfbea7d
```

H3 provider preflight gate:

```text
preflight commit       = cb5b98cbd4f59126fc6871e38842ce361add4c48
preflight evidence     = docs/superpowers/evidence/2026-08-18-interactive-handoff-h3-terminal-preflight.md
preflight evidence sha = 3c42a1f24d9a78672b30545733d2b5069cc2dbc02a17ee26c6bac4012400f8c6
```

Implementation lineage after preflight:

```text
cb5b98c docs: freeze h3 terminal provider preflight
3b63d66 feat: define terminal presentation resolution
a92af5d feat: resolve recent local terminal context
684b83b feat: add fresh bridge terminal affinity
bbf75e6 feat: launch qualified local terminals
adb2925 feat: make terminal launch retry safe
61326f8 feat: auto present human handoff terminal
```

Task-8 acceptance is intentionally a separate test/evidence commit from the feature implementation.

## 2. Qualified provider/platform matrix

Host used for promoted-provider acceptance:

```text
platform        = Darwin arm64
macOS           = 26.6.1 (25G76)
qualified tmux  = /opt/homebrew/bin/tmux
tmux version    = tmux 3.6a
tmux sha256     = 70cbf6697ac288f6fd7cfb6ea22016dc0f7d02043c10ddf5ec47b02d5c5495ef
```

Terminal presentation provider matrix:

| Platform | Provider | Identity | Launch adapter | Native lane | Capability |
|---|---|---|---|---|---|
| Darwin | Ghostty v1 | `com.mitchellh.ghostty` / `ghostty` | `/usr/bin/open -n -b com.mitchellh.ghostty --args -e` | PASS | advertised only when current-host probe proves the qualified provider running |
| Linux | none | none | none | `NOT_RUN_NO_QUALIFIED_PROVIDER` | unavailable |

No generic terminal name/PID/TTY guess is a qualified launcher identity. Production Darwin composition fails closed to H2 manual handoff when the qualified provider cannot be proven usable on the current host.

## 3. Resolver truth matrix and bounded freshness

Production composition uses these explicit data-bounded freshness constants:

```text
active terminal evidence = 5s
recent terminal evidence = 2m
single-running evidence   = 5s
Darwin provider probe     = 1s
```

`tests/integration/terminal_presentation_test.go` proves:

- active terminal evidence outranks recent evidence;
- a frontmost browser/nonterminal clears active evidence without erasing a still-fresh recent terminal;
- fresh bridge affinity outranks single-running fallback;
- stale bridge affinity downgrades rather than becoming durable preference;
- exactly one qualified running terminal can be used as fallback;
- multiple running terminal candidates are ambiguous rather than guessed;
- recent evidence expires at the configured freshness bound;
- bridge affinity is request-scoped evidence, not a stored preference.

The bridge signal is explicitly a fresh affinity hint. It is not named or treated as request-origin proof.

Capability advertises only the sources wired in production:

```text
active
recent
bridge_affinity
single_running
```

`existing_client` remains an internal exact-client proof input for launch reconciliation. H3 does not advertise reusable-client reveal support because the promoted Ghostty provider does not prove that capability.

## 4. GUI launch and retry matrix

Launch side effects are durably reserved before the GUI adapter runs. The acceptance matrix proves:

- lost launcher response becomes `launch_outcome_unknown`, not a blind retry;
- duplicate handoff request / lost handoff response reconciles the same durable launch claim;
- recovered `launching` state inspects exact-client proof instead of launching again;
- a later exact-client proof can promote unknown outcome to `launched_and_client_proven` without relaunch;
- known provider/launcher failure is durable and replays without another GUI attempt;
- conflicting launch identity/attach target cannot reuse an existing claim;
- a new GUI launch cannot be reserved after an exact human client is already durable.

No proven retry case launches a duplicate client/window.

The only attach argv admitted to the launcher has exact structural form:

```text
<absolute-shellbeam-executable> session attach --handoff-id <validated-handoff-id>
```

The provider adapter prepends only its frozen qualified argv. Model text is never interpolated into a shell command.

## 5. H2 + H3 composition and manual fallback

Fresh native composition used the real H2 delegated tmux provider/store and the H3 presentation coordinator:

```text
SHELLBEAM_H0_TMUX=/opt/homebrew/bin/tmux \
  go test ./tests/integration \
    -run 'TestInteractiveHandoffH2H3WithoutH4Composition|TestInteractiveHandoffH3UnavailableProviderKeepsH2ManualFallback' \
    -count=1 -v

PASS
TestInteractiveHandoffH2H3WithoutH4Composition                  PASS (1.27s)
TestInteractiveHandoffH3UnavailableProviderKeepsH2ManualFallback PASS (0.38s)
package                                                           PASS (2.479s)
```

The composition acceptance proves:

1. ordinary non-handoff delegated execution does no H3 resolver/launcher work;
2. H2 manual handoff remains advertised and remains the durable authority transition;
3. presentation occurs only after durable `HUMAN_CONNECTING` with agent ingress fenced;
4. lost/duplicate handoff response reconciles presentation without a second GUI launch;
5. launch unknown/unavailable does not escape as an H2 request failure;
6. the safe manual `shellbeam session attach --handoff-id ...` path remains available and can bind exact human ownership;
7. H3 adds only terminal-presentation capability; it does not make secret handoff or shell readiness available;
8. provider-unavailable composition advertises no H3 provider and constructs no presenter/resolver work.

No tracked H4 completion evidence or implementation lane is present in this tree. Therefore:

```text
H3_H4_COMBINED=NOT_RUN_COUNTERPART_ABSENT
```

This is intentionally not reported as PASS.

## 6. Shared-resource behavior

The 100-cycle integration acceptance proves terminal resolution does not allocate a watcher/timer/process per handoff. Production composition owns one shared recent-activity source/registry; each resolution only consumes bounded evidence from that shared source plus bounded running-provider probes.

The resource acceptance passed together with the resolver/retry matrix:

```text
TestTerminalPresentationResolverTruthMatrix                    PASS
TestTerminalPresentationGUIRetryMatrix                        PASS
TestTerminalPresentationSharedResourceBoundAcross100Resolutions PASS
```

Ordinary non-handoff commands are separately checked by the H2+H3 composition test and perform zero terminal-resolution work.

## 7. Native Ghostty promoted-provider smoke

The opt-in native Darwin smoke ran against the real qualified adapter:

```text
SHELLBEAM_NATIVE_TERMINAL_SMOKE=1 \
  go test ./tests/integration -run '^TestTerminalPresentationGhosttyNativeLauncherSmoke$' -count=1 -v

TestTerminalPresentationGhosttyNativeLauncherSmoke PASS (2.36s)
package                                              PASS (2.827s)
```

The smoke proves the real `/usr/bin/open` qualified argv launches a new Ghostty process, the launched child has a TTY, and the child receives the exact ShellBeam attach argv. Cleanup targets only the Ghostty PID created by the smoke.

No provider/platform without equivalent promoted native evidence is advertised by H3.

## 8. Capability, IPC, and diagnostic boundary

Task 7 production composition additionally locks these boundaries, exercised again by the final affected-suite gate:

- terminal presentation is an H3 capability extension over H2, not an authority capability;
- MCP/model input cannot synthesize `terminal_affinity`;
- bridge-observed affinity is admitted only through the bridge→daemon IPC lane;
- `inspect.server` / MCP output can expose the qualified H3 capability subset;
- Doctor reports provider ID, availability/failure category, and freshness configuration without PID, frontmost/recent app history, private paths, or other terminal metadata;
- unsupported/no-provider host composition degrades explicitly to H2 manual behavior.

## 9. Anti-goal scan

Required scan on the Task-8 tree:

```text
rg -n 'preferred_terminal|request_origin_terminal|osascript.*loop|sleep .*terminal|capture-pane' internal cmd api || true
```

Only one match exists:

```text
internal/core/terminalpresentation/types_test.go
  negative validation fixture using source="request_origin_terminal"
```

There is no production `preferred_terminal`, request-origin-terminal authority claim, AppleScript polling loop, terminal sleep heuristic, or `capture-pane` implementation.

## 10. Fresh generic verification

Fresh normal lane after Task-8 acceptance code:

```text
go mod verify
all modules verified

go test \
  ./internal/core/terminalpresentation \
  ./internal/app/terminalpresentation \
  ./internal/adapter/terminalpresentation \
  ./internal/app/interactivehandoff \
  ./cmd/shellbeam -count=1

core/terminalpresentation       PASS (0.447s)
app/terminalpresentation        PASS (0.902s)
adapter/terminalpresentation    PASS (3.759s)
app/interactivehandoff          PASS (1.443s)
cmd/shellbeam                   PASS (223.225s)
```

Fresh race lane:

```text
go test -race \
  ./internal/core/terminalpresentation \
  ./internal/app/terminalpresentation \
  ./internal/adapter/terminalpresentation -count=1

core/terminalpresentation       PASS (1.588s)
app/terminalpresentation        PASS (2.054s)
adapter/terminalpresentation    PASS (5.357s)
```

Fresh static gate:

```text
go run ./tools/devctl check
PASS
receipt = .build/receipts/20260820T075711.755313000Z-check.json
```

Fresh dirty affected-suite gate, base `33fe40999910a08410204993b9edb8f7e58698a5`:

```text
go run ./tools/devctl test --dirty --base "$(git merge-base HEAD main)" --json

status             = passed
exit_code          = 0
source_fingerprint = 10784c6b81691d39e1d1521ea745d2463aa41a02375fe4713ee2f8b708714ed5
started_at         = 2026-08-20T08:14:06.413409Z
finished_at        = 2026-08-20T08:18:33.20834Z
cmd/shellbeam      = PASS (247.938s)
```

The affected selection also passed schema, delegated tmux, IPC/MCP/store, terminal adapter, bridge, daemon, delegated/handoff/terminal apps, capability/core packages, contract tests, integration tests, and H0 tooling.

## 11. Test-harness timing stabilization discovered by final gate

Final broad gates exposed two pre-existing native fixture deadlines that were safe in isolation but flaky under parallel package scheduling:

1. two Darwin `Current()` mapping fixtures used the production-like 1s subprocess budget and were scheduler-killed at ~1.01s during broad package load; isolated 20 executions passed in ~0.16–0.42s;
2. `TestB1NativeHiddenSupervisorInheritedFDs` gave a native child only 2s to publish its test socket; isolated 10/10 passed, but one broad run exceeded the fixture deadline.

Task 8 changes **only test deadlines**:

```text
Darwin mapping fixture timeout: 1s -> 5s
B1 hidden-supervisor fixture publish deadline: 2s -> 5s
```

Production `terminalProviderProbeTimeout` remains exactly `1s`; production supervisor/runtime behavior is unchanged. After stabilization:

```text
go test ./internal/adapter/terminalpresentation -count=10  PASS
go test ./cmd/shellbeam -run '^TestB1NativeHiddenSupervisorInheritedFDs$' -count=10 PASS
final dirty affected-suite gate PASS
```

## 12. Completion boundary

H3 completion claims only automatic local terminal presentation on the qualified current-machine subset. It does **not** claim:

- secret/private handoff;
- shell readiness or automatic completion semantics;
- cross-platform terminal support without provider qualification;
- arbitrary terminal reveal/reuse without exact provider proof;
- static preferred-terminal configuration;
- request-origin terminal authority;
- duplicate-safe GUI retry without durable launch reconciliation;
- hidden terminal transcript capture.

H2 manual handoff remains the authority source and universal fallback for this feature slice.
