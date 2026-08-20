# H4 Secret Privacy Topology Binding

**Status:** H4 privacy work is allowed for the approved Darwin/macOS lane only. Linux remains `NOT_RUN`, unadvertised, and fail-closed.

```text
H4_ALLOWED=true
H4_ALLOWED_DARWIN=true
H4_ALLOWED_LINUX=false
H4_PRIVACY_TOPOLOGY=per_session_observer
H4_PRIVATE_FROM_FIRST_BYTE=true
H4_HISTORY_REPLAY_ALLOWED=false
```

## 1. Frozen prerequisites

- Master design spec: `docs/superpowers/specs/2026-08-18-human-agent-interactive-session-handoff-design.md`
- Frozen spec commit: `c3fc3d57dfbb5707e1b521e6acaaf79b33300bea`
- H2 completion commit: `1434ac75f71cb8df99b71d208aaf82cbbc87d78e` (`test: verify manual human agent handoff`)
- H2 evidence: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h2-manual-authority.md`
- H2 evidence SHA-256: `6787b0ba1835ceaba43c05202226619c039c9738e3fd35d5494e00203dfbea7d`
- H0 Markdown evidence: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.md`
- H0 Markdown SHA-256: `a3d8dc9c518572660436cbdc7b604debd054e616a7834080066ef6eaa24f3a28`
- H0 machine gate: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json`
- H0 machine-gate SHA-256: `f2d1806ff8e364a2b866e4c46bf0969191d4dcf441297638a52d011f0eb5b3f1`
- Darwin raw-report SHA-256 recorded by H0: `8bce845214ac688f8e76818dc304866154c7b8163f8e5b69907dec79ad6ca0ca`

H2 explicitly records `H4_ALLOWED=true`. The H0 machine gate is authoritative for platform qualification: aggregate cross-platform `h1_allowed=false` because native Linux evidence is absent, while `platform_h1[darwin].allowed=true`. H4 therefore inherits only the approved Darwin experimental lane and must not infer Linux support from Darwin evidence or cross-builds.

## 2. Exact production provider identity

The H4 privacy lane is bound to the same H0-qualified production provider identity already consumed by H1/H2:

```text
platform                  = darwin/arm64
provider_id               = tmux_control_mode
provider_version          = 1
control_adapter           = raw_control_mode
qualified_tmux            = /opt/homebrew/bin/tmux
tmux_version              = tmux 3.6a
tmux_sha256               = 70cbf6697ac288f6fd7cfb6ea22016dc0f7d02043c10ddf5ec47b02d5c5495ef
input_fence_mechanism     = tmux_same_client_switch-client_-E_-r_assume-paste-time_0
observation_topology      = per_session_observer
```

The current host identity matches the H0-qualified binary identity above. Production code must still use the provider identity already carried by the delegated-session runtime; this note does not authorize a generic tmux path/version fallback.

## 3. H0 privacy gates consumed by H4

The Darwin H0 evidence records all H4-required gates as PASS:

| Gate | Status | H4-bound fact |
|---|---|---|
| P4 | PASS | private session A can be suppressed while public B/C remain observable; suppression is scoped rather than global |
| P5 | PASS | `per_session_observer` is private from its first possible model-visible A byte |
| P6 | PASS | replacement observer reconnects private from first byte and does not replay gap/private history into the public path |
| P14 | PASS | `per_session_observer` keeps A private while B and C remain complete across 128 privacy cycles |
| P15 | PASS | old/new observer overlap and replacement remain private across the qualified replacement fault matrix while B/C stay observable |

The H4 plan's sample `rg '^P4 ... PASS'` command assumes a plain-text line shape, while the tracked H0 Markdown renders these gates as table rows (`| P4 | PASS | ... |`). The canonical machine gate plus the tracked Markdown table are the binding authority; the gate is not treated as missing merely because the rendering differs from that sample regex.

## 4. Frozen privacy topology

H4 production implementation must use the H0-qualified `per_session_observer` topology. It must not silently substitute either of the other H0 experiment candidates:

```text
selected:      per_session_observer
not selected:  shared_observer_with_per_pane_off
not selected:  shared_observer_with_daemon_demux_simulation
```

For the selected topology, H0 measured:

```text
private attach flag                 = no-output
private_from_attach                 = true
private_bytes_enter_control_parser  = false
private reconnect attach flag       = no-output
history_replayed                    = false
overlap_private                     = true
P14 private A count                 = 0
P14 public B complete               = true (128/128)
P14 public C complete               = true (128/128)
P15 old_private                     = true
P15 new_private                     = true
P15 survivor_private                = true
P15 B/C public                      = true
```

This choice is load-bearing. H0 showed that per-pane `refresh-client -A ...:off` can stop tmux reading and did not pass the P5 first-byte requirement in the measured candidate topology. H0 also showed that daemon-demux can keep model projection private while raw A bytes still enter the parser. H4 therefore does not redefine either candidate as equivalent to the selected `per_session_observer` mechanism.

## 5. First-byte and no-replay requirements

The implementation contract frozen by this binding is:

```text
private-before-human-write
all model-visible paths covered
A private does not suppress B/C
observer replacement has no exposure window
no history/capture-pane recovery
```

More precisely:

1. A secret handoff may not make the human client writable until every model-visible observation path for that delegated session is already private and the provider has acknowledged/proved that state.
2. The observer is created private from attach; attaching publicly and filtering/discarding A after receipt is not privacy.
3. During a private interval, no private A byte may be replayed to a replacement observer, public transcript, ordinary receipt, Event Journal, telemetry, repro, log, or error after reconnect.
4. `capture-pane` or equivalent history recovery is forbidden across a private interval.
5. A replacement observer must begin private before it can observe A. Old/new overlap is allowed only when every observer capable of seeing A is private.
6. Public B/C sessions continue through A's private interval; suppressing all Control Mode output is not an acceptable implementation.
7. Public capture for A may resume only at an explicit new forward-only boundary after human ingress is fenced and a current `PrivacyReleaseProof` exists.

## 6. Observer replacement and provider-loss rules

H0 records `observer_restart=recoverable_same_object_identity`: Control Mode observer loss does not change the tmux session/pane provider object identity. H4 may therefore reconstruct the per-session observer for the same proved provider incarnation, but only using the private-from-attach sequence while privacy is active.

Provider/server identity loss is different. H0 P11 treats server loss as a new provider incarnation even under the same friendly name. H4 must fail closed rather than guessing that the prior privacy proof applies to the replacement provider.

For private observer replacement/reconnect:

- old observer alive while new starts: both must be private;
- old observer private, new observer startup: new is private before exposure;
- old dies before new private ACK: human writability/public release cannot be inferred from the gap;
- new private ACK before old close: overlap remains private;
- rapid repeated reconnect: every replacement follows the same private-from-first-byte rule;
- observer receives server-exit while private: no history replay or public fallback is permitted.

## 7. Environment and control boundaries retained from H0/H2

Attachment/switch/control reconnect continues to use the H0-qualified `-E` behavior so observer/human presentation does not mutate the delegated session environment. H4 privacy does not add a pane-byte ingress proxy.

H2 remains the authority source. Privacy state is orthogonal to owner/ingress state: proving an authority transfer does not release output privacy, and a human-attested ready event may establish a transfer boundary without establishing `PrivacyReleaseProof`.

The H4 binding does not authorize secret values, hashes, deterministic derived values, lengths, shell history, or environment values in durable/public metadata. Only privacy boundaries/proof quality/timestamps and typed satisfied/not-satisfied/unavailable requirement results may later be represented by H4 contracts.

## 8. Task-1 decision

The prerequisite decision is:

```text
H4_TASK1_GATE=PASS_DARWIN_ONLY
H2_H4_GATE=PASS
H0_P4=PASS
H0_P5=PASS
H0_P6=PASS
H0_P14=PASS
H0_P15=PASS
PRODUCTION_PRIVACY_TOPOLOGY=per_session_observer
LINUX_PRIVACY=NOT_RUN
```

Task 2 may define shell/readiness/privacy proof contracts against this exact topology. Any implementation that needs a different privacy mechanism must return to H0 qualification rather than silently changing this binding.
