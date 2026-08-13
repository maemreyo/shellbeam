# ShellBeam contributor contract

ShellBeam is one Go modular monolith and one shipped binary. ChatGPT is the only reasoning agent; ShellBeam is a bounded local shell runtime, not an agent, sandbox, SSH fabric, or public gateway.

Dependency direction is `cmd -> app + adapters`, `adapters -> app + core`, `app -> core`, and `core -> standard library`. Adapters never import sibling adapters. Ports live with their application consumer. Do not add catch-all packages named utils, common, helpers, shared, base, misc, or models.

Production files warn above 350 lines and fail above 500. Test files warn above 600 and fail above 800. Functions require review above 60 lines and fail above 80. Interfaces fail above eight methods. `main.go` warns above 100 lines and fails above 150.

Use a focused failing test, minimum implementation, focused pass, then `go run ./tools/devctl test --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json`. Use `go run ./tools/devctl build --dirty --base "${SHELLBEAM_BASE_REF:-origin/main}" --json` for source-bound incremental builds. Empty dirty selections stay empty; do not silently broaden them. Before every local commit, stage the intended scope and invoke Git as `git -c core.hooksPath=.githooks commit ...`; the tracked pre-commit hook runs `devctl commit-gate`, which checks staged diff whitespace, staged Go policy/format, affected compile/tests, and affected `go vet`. Do not install the hook into the repository-common `.git/hooks`, because linked worktrees share that directory. Use checkpoint verification before handoff. Do not run full fresh suites in the edit loop; release verification owns that cost.

Never reset or overwrite unrelated work. Do not push, open PRs, install services, manage tunnel credentials, or contact external systems without explicit user authorization. Only a durable terminal receipt with required spawn/exit, reap, output-drain, and input-delivery evidence can prove command success.
