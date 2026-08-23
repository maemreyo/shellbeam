#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_ROOT="$ROOT/tests/fixtures/jest-json/real-doc-fixtures"
VERSIONS=("29.7.0" "30.4.2")
TMP="$(mktemp -d /tmp/shellbeam-jest-real-doc-fixtures.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$OUT_ROOT"
entries="$TMP/manifest.entries.jsonl"
: > "$entries"

normalize_json() {
  local src="$1" dst="$2" source_dir="$3" install_dir="$4"
  python3 - "$src" "$dst" "$source_dir" "$install_dir" <<'PY'
from pathlib import Path
import json, sys
src, dst, source_dir, install_dir = Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3], sys.argv[4]
value = json.loads(src.read_text(encoding="utf-8"))
DYNAMIC_NUMERIC_KEYS = {"startTime", "endTime", "runtime", "duration", "startAt", "memoryUsage"}

def normalize(node, key=None):
    if isinstance(node, dict):
        return {k: normalize(v, k) for k, v in node.items()}
    if isinstance(node, list):
        return [normalize(v, key) for v in node]
    if isinstance(node, str):
        return node.replace(source_dir, "/jest-fixture").replace(install_dir, "/jest-install")
    if key in DYNAMIC_NUMERIC_KEYS and isinstance(node, (int, float)) and not isinstance(node, bool):
        return 0
    return node

normalized = normalize(value)
dst.write_text(json.dumps(normalized, indent=2, sort_keys=True, ensure_ascii=False) + "\n", encoding="utf-8")
PY
}

profile_json() {
  local fixture="$1"
  python3 - "$fixture" <<'PY'
import json, sys
v = json.load(open(sys.argv[1], encoding="utf-8"))
results = v.get("testResults") or []
first_result = results[0] if results else {}
assertions = first_result.get("assertionResults") or []
first_assertion = assertions[0] if assertions else {}
print(json.dumps({
    "top_level_keys": sorted(v.keys()),
    "test_result_keys": sorted(first_result.keys()),
    "assertion_keys": sorted(first_assertion.keys()),
    "assertion_key_count": len(first_assertion),
    "assertion_has_failing": "failing" in first_assertion,
    "assertion_has_startAt": "startAt" in first_assertion,
}, sort_keys=True))
PY
}

for package_version in "${VERSIONS[@]}"; do
  install="$TMP/install-$package_version"
  src="$TMP/src-$package_version"
  out="$OUT_ROOT/jest-$package_version"
  mkdir -p "$install" "$src" "$out"

  (
    cd "$install"
    npm init -y >/dev/null 2>&1
    npm install --silent --no-audit --no-fund "jest@$package_version"
  )

  jest_bin="$install/node_modules/.bin/jest"
  installed_package="$(node -p "require('$install/node_modules/jest/package.json').version")"
  [[ "$installed_package" == "$package_version" ]] || {
    echo "jest package version mismatch: got $installed_package want $package_version" >&2
    exit 1
  }
  producer_version="$("$jest_bin" --version | awk '{print $1}')"

  cat > "$src/jest.config.cjs" <<'JS'
module.exports = {}
JS

  cat > "$src/pass.test.js" <<'JS'
test('passes mechanically', () => {
  expect(1 + 1).toBe(2)
})
JS
  printf '%s\n' '--bail' > "$src/args.txt"

  raw="$TMP/jest-$package_version-pass.raw.json"
  final="$out/pass.json"
  (
    cd "$src"
    env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$raw" pass.test.js >/dev/null 2>&1
  )
  [[ -s "$raw" ]] || { echo "jest $package_version did not create pass JSON" >&2; exit 1; }
  normalize_json "$raw" "$final" "$src" "$install"
  sha="$(shasum -a 256 "$final" | awk '{print $1}')"
  profile="$(profile_json "$final")"

  zero="$TMP/jest-$package_version-zero.json"
  rm -f "$zero"
  set +e
  (
    cd "$src"
    env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$zero" __definitely_no_such_test_file__ >/dev/null 2>&1
  )
  zero_status=$?
  set -e
  zero_present=false
  zero_total=-1
  [[ -f "$zero" ]] && zero_present=true
  if [[ "$zero_present" == true ]]; then
    zero_total="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["numTotalTests"])' "$zero")"
  fi

  argfile="$TMP/jest-$package_version-argfile.json"
  rm -f "$argfile"
  set +e
  (
    cd "$src"
    env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$argfile" @args.txt >/dev/null 2>&1
  )
  argfile_status=$?
  set -e
  argfile_present=false
  argfile_total=-1
  [[ -f "$argfile" ]] && argfile_present=true
  if [[ "$argfile_present" == true ]]; then
    argfile_total="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["numTotalTests"])' "$argfile")"
  fi

  explicit_bail="$TMP/jest-$package_version-explicit-bail.json"
  rm -f "$explicit_bail"
  (
    cd "$src"
    env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$explicit_bail" --bail pass.test.js >/dev/null 2>&1
  )
  [[ -f "$explicit_bail" ]] || { echo "jest $package_version explicit --bail control did not emit JSON" >&2; exit 1; }
  explicit_bail_total="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["numTotalTests"])' "$explicit_bail")"
  [[ "$explicit_bail_total" == "1" ]] || { echo "jest $package_version explicit --bail control was ambiguous: total=$explicit_bail_total" >&2; exit 1; }

  expansion_observed=false
  [[ "$argfile_total" != "0" ]] && expansion_observed=true

  python3 - "$entries" "$package_version" "$producer_version" "$sha" "$profile" \
    "$zero_status" "$zero_present" "$zero_total" "$argfile_status" "$argfile_present" "$argfile_total" "$explicit_bail_total" "$expansion_observed" <<'PY'
from pathlib import Path
import json, sys
(entries, package_version, producer_version, sha, profile,
 zero_status, zero_present, zero_total, argfile_status, argfile_present,
 argfile_total, explicit_bail_total, expansion_observed) = sys.argv[1:]
record = {
    "producer": "jest",
    "package_version": package_version,
    "producer_version": producer_version,
    "fixture": f"jest-{package_version}/pass.json",
    "sha256": sha,
    "generator_command": "env -u JEST_JASMINE <jest> --runInBand --json --outputFile=<raw.json> pass.test.js",
    "normalization": "canonical JSON; zero dynamic timing/memory fields; replace ephemeral source/install roots; preserve key membership and semantic counts/statuses",
    "observed_profile": json.loads(profile),
    "zero_match": {
        "argv": "<jest> --runInBand --json --outputFile=<zero.json> __definitely_no_such_test_file__",
        "exit_code": int(zero_status),
        "file_present": zero_present == "true",
        "num_total_tests": int(zero_total),
    },
    "argument_file_non_expansion": {
        "argv": "<jest> --runInBand --json --outputFile=<argfile.json> @args.txt",
        "args_file_content": "--bail",
        "exit_code": int(argfile_status),
        "file_present": argfile_present == "true",
        "num_total_tests": int(argfile_total),
        "explicit_bail_control_num_total_tests": int(explicit_bail_total),
        "expansion_observed": expansion_observed == "true",
        "runtime_qualification": "rejected_by_v1_at_token_rule",
    },
}
with Path(entries).open("a", encoding="utf-8") as f:
    f.write(json.dumps(record, sort_keys=True) + "\n")
PY
done

python3 - "$entries" "$OUT_ROOT/manifest.json" <<'PY'
from pathlib import Path
import json, sys
entries, manifest = map(Path, sys.argv[1:])
fixtures = [json.loads(line) for line in entries.read_text(encoding="utf-8").splitlines() if line]
manifest.write_text(json.dumps({
    "schema_version": 1,
    "generator": "scripts/generate-jest-real-doc-fixtures.sh",
    "fixtures": fixtures,
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY

echo "generated Jest real-document fixtures under $OUT_ROOT"
