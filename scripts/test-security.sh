#!/bin/sh
set -eu
go mod verify
go mod tidy -diff
! rg -n 'Listen\("tcp"|ListenAndServe|SO_REUSEPORT' --glob '*.go' internal cmd
! rg -n '(token|credential|command|stdin|output|cwd).*slog' --glob '*.go' internal
go test ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/localfs ./internal/adapter/mcp ./internal/app/bridge ./internal/app/daemon ./internal/observability ./tests/contract ./tests/integration -count=1
if command -v govulncheck >/dev/null 2>&1; then
  govulncheck ./...
else
  echo 'govulncheck: NOT_RUN (not installed)'
fi
echo 'security: PASS for mandatory checks'
