# H1 Darwin Provider Binding

**Status:** immutable H1 implementation input for the approved Darwin-only experimental lane.

- H0 gate JSON: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h0-tmux-qualification.json`
- H0 gate JSON SHA-256: `f2d1806ff8e364a2b866e4c46bf0969191d4dcf441297638a52d011f0eb5b3f1`
- Gate commit: `87331630dcc3277ffc77a60dc433054d8bcaaab2`
- Frozen master spec commit: `c3fc3d57dfbb5707e1b521e6acaaf79b33300bea`
- Gate schema version: `2`
- Gate kind: `provider_qualification`
- Cross-platform `H1_ALLOWED`: `false`
- Darwin `H1_ALLOWED`: `true`
- Linux `H1_ALLOWED`: `false` (`NOT_RUN`; unadvertised)

## Qualified Darwin provider identity

- Provider: `tmux_control_mode` v`1`
- Control adapter: `raw_control_mode`
- tmux executable: `/opt/homebrew/Cellar/tmux/3.6a/bin/tmux`
- tmux version: `tmux 3.6a`
- tmux SHA-256: `70cbf6697ac288f6fd7cfb6ea22016dc0f7d02043c10ddf5ec47b02d5c5495ef`
- Native report: `.build/interactive-handoff-h0/darwin/report.json`
- Native report SHA-256: `8bce845214ac688f8e76818dc304866154c7b8163f8e5b69907dec79ad6ca0ca`
- Native report source commit: `b765a30cfabb8d242d0de7c3d03bfccc9027fb16`
- Input fence mechanism: `tmux_same_client_switch-client_-E_-r_assume-paste-time_0`
- Observation topology: `per_session_observer`

## Genuine H0 gates bound for H1

| Gate | Darwin status |
|---|---|
| P3 | PASS |
| P4 | PASS |
| P5 | PASS |
| P6 | PASS |
| P14 | PASS |
| P15 | PASS |

All Darwin P0–P15 are `PASS` in the bound native report. Linux remains `NOT_RUN`; no Linux provider facts are inherited from Darwin.

## Implementation authority

```text
H1 code may implement only the mechanism/topology H0 qualified on Darwin.
A different tmux version/topology/wrapper requires requalification, not an in-code fallback.
Delegated-interactive capability MUST remain unavailable on non-Darwin platforms until their own platform gate is allowed.
```

`github.com/atomicstack/gotmuxcc@v0.1.4` remains an advisory H0 rejection and is not the H1 control adapter. H1 uses the ShellBeam-owned raw Control Mode semantics qualified above.
