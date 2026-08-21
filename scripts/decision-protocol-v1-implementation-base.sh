#!/usr/bin/env bash
set -euo pipefail
ROOT="$(git rev-parse --show-toplevel)"
EVIDENCE="$ROOT/docs/superpowers/evidence/2026-08-19-decision-protocol-v1-baseline.md"
base="$(awk -F'`' '/^- implementation_base: `/ {print $2; exit}' "$EVIDENCE")"
if [[ ! "$base" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'invalid or missing Decision Protocol implementation_base evidence' >&2
  exit 1
fi
current_main="$(git -C "$ROOT" rev-parse main)"
if [ "$current_main" != "$base" ]; then
  printf 'Decision Protocol implementation base drift: recorded=%s current_main=%s\n' "$base" "$current_main" >&2
  exit 42
fi
git -C "$ROOT" merge-base --is-ancestor "$base" HEAD || {
  echo 'Decision Protocol implementation HEAD is not descended from recorded implementation base' >&2
  exit 1
}
printf '%s\n' "$base"
