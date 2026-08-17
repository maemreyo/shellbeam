#!/bin/sh
set -eu
go test ./internal/adapter/mcp -run 'TestMetadata|TestInMemoryConformance|Test.*Media|TestReadMedia' -count=1
if command -v npx >/dev/null 2>&1; then
  echo 'MCP SDK in-memory conformance: PASS; external Inspector invocation remains operator-controlled.'
else
  echo 'MCP Inspector CLI: NOT_RUN (npx not installed)'
fi
