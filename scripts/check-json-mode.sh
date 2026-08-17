#!/bin/sh
set -eu

EXPECTED_MODULE_VERSION='v0.0.0-20260623181947-01eb4420fa68'
MODE='go-json-experiment-library-boundary'

goversion=$(go env GOVERSION)
goexperiment=$(go env GOEXPERIMENT)
module_version=$(go list -m -f '{{.Version}}' github.com/go-json-experiment/json)

if [ -n "$goexperiment" ]; then
  echo "json mode mismatch: global GOEXPERIMENT must be empty in ${MODE}, got ${goexperiment}" >&2
  exit 4
fi

version=${goversion#go}
major=${version%%.*}
rest=${version#*.}
minor=${rest%%.*}
case "$major:$minor" in
  *[!0-9:]*|:*)
    echo "json mode mismatch: unsupported Go version ${goversion}" >&2
    exit 4
    ;;
esac
if [ "$major" -lt 1 ] || { [ "$major" -eq 1 ] && [ "$minor" -lt 26 ]; }; then
  echo "json mode mismatch: ${MODE} requires Go >=1.26, got ${goversion}" >&2
  exit 4
fi

if [ "$module_version" != "$EXPECTED_MODULE_VERSION" ]; then
  echo "json mode mismatch: github.com/go-json-experiment/json=${module_version}, want ${EXPECTED_MODULE_VERSION}" >&2
  exit 4
fi

printf '{"mode":"%s","go_version":"%s","goexperiment":"%s","module_version":"%s"}\n' \
  "$MODE" "$goversion" "$goexperiment" "$module_version"
