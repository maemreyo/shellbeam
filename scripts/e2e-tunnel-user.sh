#!/bin/sh
set -eu
command -v tunnel-client >/dev/null 2>&1 || { echo 'tunnel-client missing' >&2; exit 3; }
[ -n "${SHELLBEAM_TUNNEL_E2E_ACK:-}" ] || { echo 'Set SHELLBEAM_TUNNEL_E2E_ACK=1 after reading docs/testing/tunnel-e2e.md' >&2; exit 3; }
[ "${SHELLBEAM_TUNNEL_E2E_ACK}" = 1 ] || exit 3
echo 'Prerequisites present. Follow docs/testing/tunnel-e2e.md in ChatGPT and record evidence; this script does not handle credentials or claim PASS.'
exit 3
