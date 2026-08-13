# Doctor

`shellbeam doctor --json` emits closed version-1 checks for configuration, state/runtime path safety, daemon socket presence and external tunnel-client presence. `pass` proves that boundary check, `warn` is an actionable absent/nonfatal prerequisite, `fail` blocks safe operation, and `not_run` means evidence was unavailable.

Doctor never reads or prints tunnel credentials, commands, cwd, environment, stdin or captured output.
