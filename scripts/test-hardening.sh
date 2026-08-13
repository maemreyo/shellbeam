#!/bin/sh
set -eu
: "${GOCACHE:?set GOCACHE to a writable persistent directory}"
go test -race ./internal/app/daemon ./internal/adapter/process ./internal/adapter/store ./internal/adapter/ipc ./internal/adapter/mcp ./tests/integration -count=1
go test ./internal/core/receipt ./internal/core/session -count=20
go test ./internal/core/receipt -run '^$' -fuzz FuzzVisibleOutput -fuzztime=1s
go test ./internal/core/operation -run '^$' -fuzz FuzzFingerprintDeterministic -fuzztime=1s
echo 'hardening: PASS (bounded current-host campaign)'
