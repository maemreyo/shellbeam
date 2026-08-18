#!/usr/bin/env bash
set -euo pipefail
mode="${1:-baseline}"
case "$mode" in
  baseline)
    printf '%s\n' '{"scenario":"docs_only_four_markdown_specs","source_fingerprint":"8aff94e1f3110a3b5358711ee013fd342e558d494e452f2b547d59846184266e","checkpoint_selection":"full","checkpoint_wall_ms":480000,"commit_gate_selection":"affected:contract:markdown","commit_gate_wall_ms":null,"measurement_quality":"historical_approx","status":"historical_baseline"}'
    ;;
  --measure-current)
    python3 - "${SHELLBEAM_BASE_REF:-origin/main}" <<'PYBENCH'
import json, subprocess, sys, time
base = sys.argv[1]
def run(kind, args):
    started = time.monotonic_ns()
    proc = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    wall_ms = (time.monotonic_ns() - started) // 1_000_000
    if proc.returncode != 0:
        sys.stderr.write(proc.stdout + proc.stderr)
        raise SystemExit(proc.returncode)
    lines = [line for line in proc.stdout.splitlines() if line.lstrip().startswith("{")]
    if not lines:
        raise SystemExit(f"{kind}: missing devctl JSON")
    payload = json.loads(lines[-1])
    return payload, wall_ms
commit, commit_ms = run("commit_gate", ["go","run","./tools/devctl","commit-gate","--base",base,"--json"])
checkpoint, checkpoint_ms = run("checkpoint", ["go","run","./tools/devctl","verify","--checkpoint","--base",base,"--json"])
print(json.dumps({
    "scenario":"docs_only_four_markdown_specs",
    "source_fingerprint":checkpoint["source_fingerprint"],
    "checkpoint_selection":checkpoint["selection"],
    "checkpoint_wall_ms":checkpoint_ms,
    "commit_gate_selection":commit["selection"],
    "commit_gate_wall_ms":commit_ms,
    "measurement_quality":"measured_local",
    "status":"measured",
}, separators=(",",":"), sort_keys=True))
PYBENCH
    ;;
  *) echo "usage: $0 [baseline|--measure-current]" >&2; exit 2 ;;
esac
