# Nushell shell-integration qualification evidence

Qualification performed: 2026-08-22
Final native revalidation: 2026-08-23
Plan authority: `docs/superpowers/plans/2026-08-18-interactive-handoff-h5-context-exec-high-assurance.md`, Task 9

## Scope and verdict

This artifact qualifies only the Nushell shell-integration lane for the existing L2/L3/H4/H5 contracts. It does not create a weaker provider path and does not qualify a new terminal-presentation or interactive-session provider.

```text
NUSHELL_PROMOTION_ALLOWED=true
ADDITIONAL_TERMINAL_PROVIDER_PROMOTION=NOT_RUN
ADDITIONAL_SESSION_PROVIDER_PROMOTION=NOT_RUN
```

## Runtime provenance

The qualification used Nushell `0.115.0` from the Homebrew `arm64_tahoe` bottle without linking or globally installing it. The bottle was extracted under `/tmp` and executed from:

```text
/tmp/shellbeam-task9-nu-0.115.0/nushell/0.115.0/bin/nu
```

Fresh revalidation proved the executable is Mach-O arm64 and reports `0.115.0`.

The exact cached bottle SHA-256 was independently rechecked on 2026-08-23:

```text
79f19134e6573994c2ed3b213e31f8aac187b5a427af426dd3a5b66d9a271916
```

Receipt: operation `task9-verify-bottle-cache-digest-2621`, session `01M0NYNZJJ0E8B18DX04CVSERE`.

The qualification used isolated temporary HOME/config/runtime directories. It did not run `brew install`, did not link Nushell, and did not mutate user Nushell configuration.

## Native qualification authority

The authoritative sanitized native qualification run is:

```text
operation: task9-nushell-final-qualified-clean-2326
session:   01M0N37GRXMW0ZX7M9SEWXR1V5
outcome:   SUCCESS / exit 0
```

It launched Nushell through `/usr/bin/env -i` so host environment state could not satisfy the fixture accidentally. Only a synthetic fixture canary was used; no host secret value was printed, serialized, hashed, or captured as qualification evidence.

An earlier exploratory run was explicitly rejected as authority because outer fish expansion corrupted the intended Nushell assignment and the host happened to contain a variable with the same name. That run is not evidence for promotion.

## Contract proofs

The authoritative run proved all Task-9 Nushell requirements:

- **Exact current-shell detection:** pane PID itself was the `nu` process; current command was `nu`; `pgid == tpgid` proved the exact foreground shell.
- **Ephemeral safe-boundary hook:** readiness used a `pre_prompt` closure. The first prompt only armed the watcher; the later prompt satisfied the requirement. The ShellBeam hook removed itself exactly while a pre-existing user hook remained.
- **Environment-exported-nonempty without value capture:** the hook checked only the length/non-empty predicate of the requested exported environment variable inside Nushell and sent only a boolean satisfaction notification.
- **Private notifier:** notifier argv was exactly `__handoff_notify --satisfied true`; it was launched through sanitized environment isolation and the synthetic secret was absent from notifier argv/environment evidence.
- **Fixed context-helper launch:** helper argv was exactly `__context_exec_helper` plus the opaque launch ID. No target command, bearer claim, socket, or user secret appeared in shell text or helper argv.
- **Context inheritance:** the helper inherited the qualified shell cwd and synthetic exported user context. Its runtime locator existed only in the scoped `with-env` invocation and did not remain in the shell afterward.
- **Foreground helper:** helper process group matched the terminal foreground process group.
- **Cleanup/no config mutation:** the ShellBeam hook was removed, the pre-existing user hook continued to execute, and the isolated Nushell config bytes were unchanged.
- **No POSIX fallback:** Nushell delivery is direct Nushell syntax and does not depend on a nonexistent `eval` command or Bourne/fish/zsh hook syntax.

Representative authoritative facts from the run included:

```text
NU_VERSION=0.115.0
FIRST_PROMPT_NOTIFIED=false
HOOKS_AFTER_READINESS=1
ARM_PRESENT=false
NOTIFIER_SECRET_LEAK=false
HOOKS_AFTER_HELPER=1
RUNTIME_ENV_PRESENT=false
HELPER_SECRET_COUNT=1
HELPER_RUNTIME_COUNT=1
CWD_SAMEFILE=true
HELPER_FOREGROUND=true
CONFIG_UNCHANGED=true
PANE_ERRORS=false
```

## Tracked native regression acceptance

The repository contains opt-in Darwin acceptance `TestNushellQualifiedDarwinNativePromptAndHelper`. It requires an explicit qualified Nushell executable path and tmux path; ordinary CI does not depend on the temporary bottle.

Fresh final revalidation after removing all temporary diagnostics:

```text
operation: task9-nushell-native-final-count3-2620
session:   01M0NYNB5XJHX430PD1JCVQVTC
runs:      3/3 PASS
elapsed:   3.61s / 3.35s / 3.21s
```

The tracked test covers exact Nu pane identity, prompt-boundary readiness, preservation of a pre-existing user hook/config, private notifier behavior, fixed context-helper argv, inherited cwd/exported user context, scoped runtime locator, and foreground helper ownership.

## Existing-provider regression found during promotion

Task-9 validation exposed two regressions/edge conditions in already-qualified paths and they were fixed without weakening contracts:

1. POSIX watcher delivery had accidentally been refactored to place a literal `\n` between hook installation and installed-ACK instead of a real command boundary. `TestPosixWatcherDeliveryPreservesRealCommandBoundary` locks the real newline semantics.
2. tmux control teardown could observe read-loop EOF before the control process actually exited and then enter an unbounded `cmd.Wait()`, allowing private-observer replacement to exceed the daemon HTTP write timeout. The close path now times the actual process exit, sends SIGTERM, and kills after the existing 250 ms bound. `TestControlClientCloseWaitsForProcessExitNotReadLoopEOF` locks this case.

Fresh bounded-close regression: operation `task9-control-close-green-2613`, 5/5 PASS at approximately 0.26-0.28 s each.

Fresh existing H5 native post-secret/context-exec regression after all temporary diagnostics were removed: operation `task9-h5-clean-stability-2614`, session `01M0NYHM4WH75X6Q5YS02H7J1Z`, 3/3 PASS at 15.48 s / 14.75 s / 11.93 s.

## Additional terminal-provider lane

`NOT_RUN` for Task-9 promotion. Fresh candidate scan found Ghostty only among the targeted installed terminal candidates. Ghostty is already qualified by the existing H3 terminal-presentation evidence, so there is no new terminal provider to promote in this task. Absence of another candidate does not weaken the Nushell verdict or existing H5 core guarantees.

Fresh host-facts receipt: operation `task9-fresh-provider-facts-fish-2619`, session `01M0NYMZ4TFNB94X2GZQN14PFG`.

## Additional interactive-session-provider lane

`NOT_RUN` for Task-9 promotion. `tmux` is the already-qualified stable provider. A system `screen` executable is present, but no full H0-equivalent P0-P15 qualification was performed for it, so it is deliberately **not** advertised or implemented as a provider. Arbitrary external PTY adoption remains outside stable V1.

## Capability claim

With the native qualification above, Nushell may join the exact qualified shell-adapter set:

```text
fish,zsh,bash,nushell
```

This promotion is additive only. It does not alter terminal-provider identity rules, session-provider authority, H4 privacy topology, H5 helper authority, or the evidence strength of already-qualified providers.

## Final implementation verification

Final verification on the Task-9 implementation/evidence tree:

```text
affected matrix: task9-affected-matrix-2623 — PASS
race matrix:     task9-race-matrix-2624 — PASS
full cmd gate:   task9-full-cmd-gate-2625 — PASS, cmd/shellbeam 240.013s
native after structural refactor: task9-native-after-structural-refactor-2633 — PASS, 2.07s
devctl check:    task9-devctl-check-after-refactor-2634 — PASS
receipt:         .build/receipts/20260823T000722.688503000Z-check.json
git diff --check: PASS
```

The first final `devctl check` correctly rejected an 85-line native test function against the repository hard cap of 80 lines. That was resolved by a test-only extraction of tool/version qualification into a helper; native semantics were re-run non-skipped afterward. No `dev/test-impact.toml` change was required.
