# H2 Human Authority — Immutable Darwin Prerequisite Binding

**Status:** approved H2 implementation input for the Darwin/macOS experimental lane only.

## Gate and H1 baseline

- H1 gate evidence: `docs/superpowers/evidence/2026-08-18-interactive-handoff-h1-delegated-core.md`
- H1 evidence SHA-256: `3cfa6ac8eba758a092f6f84517878cea2e05c76030cf0904fdbf597e12493ac1`
- H1 implementation HEAD: `62f9a364b3c319d776b0b480b7c86184b09eaa22`
- H1 implementation source fingerprint: `9b76007a9d45e3517b1e8efba7c47e4b43f72010f648483956640018b6fe178f`
- H1 attestation commit: `1fc7929fa8cf2ed15db9ea8a5400189a00002819`
- H2 gate: `H2_ALLOWED=true`
- H2 scope: `darwin_experimental`
- Linux: disabled / `NOT_RUN`; no Darwin human-control fact is inherited by Linux.

H2 may build only on the exact H1 authority/session/provider contract attested above. A different provider, tmux binary/hash, or platform requires requalification rather than fallback.

## Provider identity

- Provider: `tmux_control_mode` v`1`
- Control adapter: `raw_control_mode`
- Qualified tmux: `/opt/homebrew/Cellar/tmux/3.6a/bin/tmux`
- tmux version: `tmux 3.6a`
- tmux SHA-256: `70cbf6697ac288f6fd7cfb6ea22016dc0f7d02043c10ddf5ec47b02d5c5495ef`
- H0 gate SHA-256: `f2d1806ff8e364a2b866e4c46bf0969191d4dcf441297638a52d011f0eb5b3f1`
- H1 provider-binding SHA-256: `3af6deba3bed46c39f5ae2e358d255e64d711c00b75e300a8d40cc5cabaecbba`
- Observation topology: `per_session_observer`

## Required H0 human-control gates

| Probe | Darwin | Bound fact |
|---|---|---|
| P2 | PASS | exact terminal-client read-only/writable state toggles without affecting another client; missing/ambiguous identity rejects |
| P3 | PASS | same human-client input stream fences all post-fence markers after acknowledged read-only transition; 1,000/1,000 measured |
| P8 | PASS | writable human client reaches shell-independent OOB control while foreground child owns pane stdin |
| P9 | PASS | read-only client can detach to local ShellBeam control; `resume,status,terminate` remain reachable without pane-byte proxy |

Linux P2/P3/P8/P9 are `NOT_RUN`; this binding is Darwin-only.

## Exact human writable/read-only mechanism

H0 bound client authority to an **exact tmux terminal client identity**, not a pane name or foreground process guess.

Qualified terminal-client state transition:

```text
tmux -S <private-socket> switch-client -E -c <exact-client-name> -r
```

H0 source records this mechanism as:

```text
client_flag_control = switch-client_-E_-c_exact-client_-r
```

For tmux 3.6a, `switch-client ... -r` is the measured/documented exact-client read-only toggle used in both directions after first observing the current flag. ShellBeam must re-read the exact client state after each transition; acknowledgement alone is insufficient.

Missing or ambiguous exact client identity is rejected. H2 must not fall back to “current pane”, TTY guessing, PID guessing, or the user tmux server.

## Exact agent-ingress fence mechanism

Qualified fence:

```text
input_fence_mechanism = tmux_same_client_switch-client_-E_-r_assume-paste-time_0
```

The H0 measurement binds a no-prefix key on the **human client's own stream** to:

```text
switch-client -E -r
```

and requires private-server:

```text
assume-paste-time=0
```

The acknowledgement proves that subsequent markers on that same old-authority human stream are rejected by tmux after the read-only transition. It does **not** claim that pre-fence bytes have drained from the application or that a shell/process is quiescent.

H2 therefore treats this as `IngressFenceProof`, not `TransferBoundary` and not `PrivacyReleaseProof`.

## Writable-state OOB HumanControl mechanism

H0 P8 qualified tmux-native OOB control on the private server:

```text
bind-key -n <control-key> wait-for -S <private-channel>
```

and a ShellBeam-side waiter:

```text
tmux -S <private-socket> -f /dev/null wait-for <private-channel>
```

Bound facts:

```text
signal_transport                = tmux_wait-for
foreground_child_received_key   = false
shell_prompt_required           = false
shell_command_fallback          = pane_stdin_not_control_plane
permanent_user_config_changed   = false
```

H2 may use this class of mechanism for writable-state HumanControl only when generation-bound to the current handoff/epoch and installed on the private ShellBeam tmux server. It must not turn arbitrary user tmux bindings into authority.

## Fenced/read-only HumanControl reachability

H0 P9 proved that a read-only client blocks arbitrary key bindings, but a detach binding remains usable:

```text
bind-key -n <detach-key> detach-client
```

After detach, the human reaches a ShellBeam local same-user control surface. Qualified H0 actions:

```text
resume,status,terminate
```

Bound facts:

```text
arbitrary_binding_while_readonly = blocked
detach_while_readonly            = reachable
ingress_proxy_introduced          = false
pane_control_bytes_injected       = false
```

Therefore H2's fenced/read-only control path is **detach → local ShellBeam control surface**. H2 must not regrant pane write authority merely to issue `Status`, `Resume`, or `Terminate`, and it must not add a byte ingress proxy unless a later qualified design explicitly requires one.

## Attach/environment preservation mechanism

H0 P7 negative control showed attachment without `-E` can mutate session `SSH_AUTH_SOCK` / `DISPLAY` from client environment. Qualified presentation paths preserve the existing delegated session environment with `-E`:

```text
attach with -E              = preserved
switch-client with -E       = preserved
Control Mode reconnect -E   = preserved
```

H2 human attach/switch/re-attach is presentation only. It must use the qualified `-E` behavior and must not synchronize human-client environment into the delegated session.

## H2 implementation boundary

This binding authorizes H2 to implement:

- durable human↔agent ownership transfer;
- exact human client attach/identity;
- exact writable/read-only transition and ingress fencing;
- writable-state OOB HumanControl;
- read-only detach to local same-user control;
- manual `Ready` transfer boundaries;
- restart/client-loss reconciliation.

It does **not** authorize:

- `privacy=secret`;
- shell-aware readiness or environment-export proofs;
- automatic GUI terminal resolver/launcher;
- `capture-pane` replay;
- arbitrary pane-stdin control commands;
- automatic process takeover;
- Linux support.

H2 runtime must remain `privacy=standard` + `completion.kind=manual_ready`; any secret/automatic readiness request fails before a human client becomes writable.
