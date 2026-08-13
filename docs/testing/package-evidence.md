# Package evidence

The source ZIP is generated from an exact clean Git commit with `git archive`. A generated release evidence file and a complete per-file SHA-256 manifest are added in a temporary staging directory. The verification script unpacks into a new temporary directory, verifies every file, verifies modules, runs the full tests, builds the native binary, runs version/doctor, and proves `.git` is absent.

The ZIP intentionally excludes credentials, runtime/state/socket data, build caches, raw captured command output, and Git history.
