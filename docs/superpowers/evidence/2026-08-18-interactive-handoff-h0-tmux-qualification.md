# Interactive Handoff H0 tmux Qualification

- Spec commit: `5351215de2c02ac61ac82751c1680a35744047af`
- Provider: `tmux_control_mode` v1
- H1_ALLOWED: `false`
- Final H0 verdict: `NOT_RUN`
- Input fence mechanism: `unqualified`
- Observation topology: `unqualified`
- Control adapter: `raw_control_mode`
- Gate reason: one or more required native lanes are `NOT_RUN`; native Linux qualification remains required before H1 can open.

## Optional wrapper qualification

- Candidate: `github.com/atomicstack/gotmuxcc@v0.1.4`
- Verdict: `FAIL` (advisory; not part of `H1_ALLOWED`)
- Reason: `P5_FIRST_BYTE_PRIVACY+PANE_OUTPUT_PARSE_ERROR`
- Recommendation: `own thin Control Mode adapter`

| Platform | tmux | Verdict | Report SHA-256 |
|---|---|---|---|
| darwin/arm64 | `tmux 3.6a` | PASS | `8bce845214ac688f8e76818dc304866154c7b8163f8e5b69907dec79ad6ca0ca` |
| linux/unknown | `NOT_RUN` | NOT_RUN | `e90a600884e9e5a8fd98aa430ae024f19bcd2a5f1bb13d8d2d712929d789c070` |

## Load-bearing provider evidence

### darwin/arm64

- Raw report: `.build/interactive-handoff-h0/darwin/report.json`
- P4 eligible privacy candidates: `per_session_observer,shared_observer_with_per_pane_off,shared_observer_with_daemon_demux_simulation`
- P5 first-byte passing candidates: `per_session_observer,shared_observer_with_daemon_demux_simulation`
- per-pane off stops tmux reading: `true`
- daemon demux private bytes enter parser: `true`
- negative control without `-E`: `mutated`
- attach with `-E`: `preserved`; switch with `-E`: `preserved`; control reconnect with `-E`: `preserved`
- OOB transport: `tmux_wait-for`; foreground child received control key: `false`
- read-only detach to local control: `reachable`; ingress proxy introduced: `false`
- all-control-off backpressure: `true`; human display prevents backpressure: `true`; cross-client total ordering claimed: `false`

### linux/unknown

- Raw report: `.build/interactive-handoff-h0/linux/report.json`
- Native lane verdict: `NOT_RUN`; provider facts were not inferred.

## P0–P15

### darwin/arm64

| Probe | Status | Summary |
|---|---|---|
| P0 | PASS | private tmux server/socket identity is isolated and H0 control-key paste heuristic is disabled |
| P1 | PASS | two real PTY clients have stable distinct name/tty/pid identity |
| P2 | PASS | exact terminal client read-only state toggles without mutating another client; missing/ambiguous targets reject |
| P3 | PASS | same human-client stream rejected every post-fence marker after acknowledged read-only transition; no PTY/application quiescence claim |
| P4 | PASS | privacy scope measured; at least one candidate suppresses private A while preserving public B/C; topology remains unqualified pending P5/P6 and cross-platform evidence |
| P5 | PASS | at least one measured P4 candidate can establish privacy before its first model-visible A byte |
| P6 | PASS | at least one measured candidate reconnects private-from-first-byte without replaying gap/private history into the public path |
| P7 | PASS | negative control proves attach environment mutation without -E; ShellBeam attachment/switch/recovery paths preserve session environment with -E |
| P8 | PASS | writable human client reaches shell-independent OOB control while foreground child owns pane stdin |
| P9 | PASS | read-only client can detach to a local control surface with resume/status/terminate reachable and no pane byte proxy |
| P10 | PASS | manual size ownership makes observer/read-only/detach size-stable; only explicit human adoption resizes |
| P11 | PASS | client/observer loss preserves tmux object identity; server loss creates a new provider incarnation even under the same friendly name |
| P12 | PASS | same-control-client ACK ordering is sufficient for qualified privacy transitions; cross-client total ordering is not claimed; backpressure topology is measured explicitly |
| P13 | PASS | 100 serial cycles preserve exact tmux live shape and converge server/socket/PTY/client/helper/temp-root plus H0 FD/goroutine resources to baseline |
| P14 | PASS | at least one P4-P6-compatible topology keeps private A absent while B/C remain complete through 128 privacy cycles |
| P15 | PASS | eligible topology keeps every old/new observer private across six replacement fault classes while public B/C remain observable |

### linux/unknown

| Probe | Status | Summary |
|---|---|---|
| P0 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P1 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P2 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P3 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P4 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P5 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P6 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P7 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P8 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P9 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P10 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P11 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P12 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P13 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P14 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
| P15 | NOT_RUN | native Linux runner unavailable; local Docker context is Docker Desktop/linuxkit and is not accepted as native Linux evidence |
