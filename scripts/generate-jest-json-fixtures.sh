#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_ROOT="$ROOT/tests/fixtures/jest-json"
VERSIONS=("29.7.0" "30.4.2")
TMP="$(mktemp -d /tmp/shellbeam-jest-json-fixtures.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT
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
dst.parent.mkdir(parents=True, exist_ok=True)
dst.write_text(json.dumps(normalized, indent=2, sort_keys=True, ensure_ascii=False) + "\n", encoding="utf-8")
PY
}

write_cases() {
  local src="$1"
  cat > "$src/jest.config.cjs" <<'JS'
module.exports = {}
JS
  cat > "$src/pass.test.js" <<'JS'
test('ordinary pass', () => {
  expect(1 + 1).toBe(2)
})
JS
  cat > "$src/fail.test.js" <<'JS'
test('ordinary failure', () => {
  expect({answer: 41}).toEqual({answer: 42})
})
JS
  cat > "$src/test_skip.test.js" <<'JS'
test.skip('explicit test skip', () => {
  throw new Error('must not execute')
})
JS
  cat > "$src/test_todo.test.js" <<'JS'
test.todo('todo test')
JS
  cat > "$src/describe_skip.test.js" <<'JS'
describe.skip('skipped describe', () => {
  test('nested skipped test', () => {
    throw new Error('must not execute')
  })
})
JS
  cat > "$src/before_all_throw.test.js" <<'JS'
beforeAll(() => {
  throw new Error('beforeAll boom')
})
test('body never becomes authoritative', () => {
  expect(true).toBe(true)
})
JS
  cat > "$src/before_each_throw.test.js" <<'JS'
beforeEach(() => {
  throw new Error('beforeEach boom')
})
test('body blocked by beforeEach', () => {
  expect(true).toBe(true)
})
JS
  cat > "$src/after_all_throw.test.js" <<'JS'
afterAll(() => {
  throw new Error('afterAll boom')
})
test('body passes before afterAll fails suite', () => {
  expect(true).toBe(true)
})
JS
  cat > "$src/module_throw.test.js" <<'JS'
throw new Error('module load boom')
JS
  cat > "$src/retry_failed.test.js" <<'JS'
jest.retryTimes(2)
test('retry remains failed', () => {
  expect('left').toBe('right')
})
JS
  cat > "$src/retry_passed.test.js" <<'JS'
jest.retryTimes(2)
let attempts = 0
test('retry eventually passes', () => {
  attempts += 1
  expect(attempts).toBe(3)
})
JS
  cat > "$src/failing_expected.test.js" <<'JS'
test.failing('expected failure', () => {
  expect('left').toBe('right')
})
JS
  cat > "$src/failing_unexpected.test.js" <<'JS'
test.failing('unexpected pass', () => {
  expect('same').toBe('same')
})
JS
  cat > "$src/focused_trap.test.js" <<'JS'
test('one ordinary pass', () => {
  expect(true).toBe(true)
})
test.skip('one explicit skip', () => {
  throw new Error('must not execute')
})
JS
  python3 - "$src/over_cap.test.js" <<'PY'
from pathlib import Path
import sys
p = Path(sys.argv[1])
count = 8200
lines = ["test('case %05d', () => { expect(true).toBe(true) })" % i for i in range(count)]
p.write_text("\n".join(lines) + "\n", encoding="utf-8")
PY
}

run_fixture() {
  local package_version="$1" producer_version="$2" jest_bin="$3" src="$4" install="$5" out="$6" fixture="$7" file="$8"
  local raw="$TMP/jest-$package_version-$fixture.raw.json"
  rm -f "$raw"
  set +e
  (
    cd "$src"
    env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$raw" "$file" >/dev/null 2>&1
  )
  local status=$?
  set -e
  [[ -s "$raw" ]] || {
    echo "jest $package_version fixture $fixture did not emit JSON (exit=$status)" >&2
    return 1
  }
  local final="$out/$fixture.json"
  normalize_json "$raw" "$final" "$src" "$install"
  local sha
  sha="$(shasum -a 256 "$final" | awk '{print $1}')"
  python3 - "$entries" "$package_version" "$producer_version" "$fixture" "$file" "$status" "$sha" <<'PY'
from pathlib import Path
import json, sys
entries, package_version, producer_version, fixture, file, status, sha = sys.argv[1:]
record = {
    "producer": "jest",
    "package_version": package_version,
    "producer_version": producer_version,
    "fixture": f"jest-{package_version}/{fixture}.json",
    "source_case": file,
    "generator_argv": ["<jest>", "--runInBand", "--json", "--outputFile=<raw.json>", file],
    "environment": {"JEST_JASMINE": "absent"},
    "exit_code": int(status),
    "sha256": sha,
    "normalization": "canonical JSON; zero dynamic timing/memory fields; replace ephemeral source/install roots; preserve key membership, statuses, counters, invocations, messages and semantic shape",
}
with Path(entries).open("a", encoding="utf-8") as f:
    f.write(json.dumps(record, sort_keys=True) + "\n")
PY
}

fixtures=(
  "pass:pass.test.js"
  "fail:fail.test.js"
  "test-skip:test_skip.test.js"
  "test-todo:test_todo.test.js"
  "describe-skip:describe_skip.test.js"
  "before-all-throw:before_all_throw.test.js"
  "before-each-throw:before_each_throw.test.js"
  "after-all-throw:after_all_throw.test.js"
  "module-throw:module_throw.test.js"
  "retry-failed:retry_failed.test.js"
  "retry-passed:retry_passed.test.js"
  "failing-expected:failing_expected.test.js"
  "failing-unexpected:failing_unexpected.test.js"
  "focused-trap:focused_trap.test.js"
  "over-cap:over_cap.test.js"
)

for package_version in "${VERSIONS[@]}"; do
  install="$TMP/install-$package_version"
  src="$TMP/src-$package_version"
  out="$OUT_ROOT/jest-$package_version"
  rm -rf "$out"
  mkdir -p "$install" "$src" "$out"
  (
    cd "$install"
    npm init -y >/dev/null 2>&1
    npm install --silent --no-audit --no-fund "jest@$package_version"
  )
  jest_bin="$install/node_modules/.bin/jest"
  installed_package="$(node -p "require('$install/node_modules/jest/package.json').version")"
  [[ "$installed_package" == "$package_version" ]] || { echo "package mismatch $installed_package != $package_version" >&2; exit 1; }
  producer_version="$("$jest_bin" --version | awk '{print $1}')"
  [[ -n "$producer_version" ]] || { echo "producer version unavailable for package $package_version" >&2; exit 1; }
  write_cases "$src"
  for pair in "${fixtures[@]}"; do
    fixture="${pair%%:*}"
    file="${pair#*:}"
    echo "generate jest $package_version $fixture" >&2
    run_fixture "$package_version" "$producer_version" "$jest_bin" "$src" "$install" "$out" "$fixture" "$file"
  done
done

python3 - "$entries" "$OUT_ROOT/manifest.json" <<'PY'
from pathlib import Path
import json, sys
entries, manifest = map(Path, sys.argv[1:])
fixtures = [json.loads(line) for line in entries.read_text(encoding="utf-8").splitlines() if line]
manifest.write_text(json.dumps({
    "schema_version": 1,
    "generator": "scripts/generate-jest-json-fixtures.sh",
    "fixtures": fixtures,
}, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY

echo "generated ${#fixtures[@]} semantic fixtures for each Jest release under $OUT_ROOT"
