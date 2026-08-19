#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_ROOT="$ROOT/tests/fixtures/pytest-junit"
VERSIONS=("8.4.2" "9.1.1")
TMP="$(mktemp -d /tmp/shellbeam-pytest-junit-fixtures.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT

mkdir -p "$OUT_ROOT"
cat > "$TMP/outcomes_test.py" <<'PY'
import pytest


def test_pass():
    assert True


def test_fail():
    assert 1 == 2


@pytest.mark.skip(reason="mechanical skip reason")
def test_skip():
    assert False


@pytest.mark.xfail(reason="mechanical xfail reason")
def test_xfail():
    assert False


@pytest.mark.xfail(reason="non-strict xpass reason", strict=False)
def test_non_strict_xpass():
    assert True


@pytest.mark.xfail(reason="strict xpass reason", strict=True)
def test_strict_xpass():
    assert True


@pytest.fixture
def setup_error():
    raise RuntimeError("setup boom")


def test_error(setup_error):
    pass
PY

cat > "$TMP/duplicate_entry_test.py" <<'PY'
import pytest


@pytest.fixture
def teardown_error():
    yield
    raise RuntimeError("teardown boom")


def test_call_fail_and_teardown_error(teardown_error):
    assert False, "call boom"
PY

normalize_xml() {
  local src="$1" dst="$2" source_dir="$3"
  python3 - "$src" "$dst" "$source_dir" <<'PY'
from pathlib import Path
import re, sys
src, dst, source_dir = Path(sys.argv[1]), Path(sys.argv[2]), sys.argv[3]
text = src.read_text(encoding="utf-8")
text = re.sub(r'\btime="[^"]*"', 'time="0.000"', text)
text = re.sub(r'\btimestamp="[^"]*"', 'timestamp="1970-01-01T00:00:00+00:00"', text)
text = re.sub(r'\bhostname="[^"]*"', 'hostname="fixture-host"', text)
text = text.replace(source_dir, "/pytest-fixture")
dst.write_text(text, encoding="utf-8")
PY
}

manifest_tmp="$TMP/manifest.entries"
: > "$manifest_tmp"

for version in "${VERSIONS[@]}"; do
  venv="$TMP/venv-$version"
  src="$TMP/src-$version"
  out="$OUT_ROOT/pytest-$version"
  python3 -m venv "$venv"
  "$venv/bin/python" -m pip install --disable-pip-version-check --quiet "pytest==$version"
  installed="$($venv/bin/pytest --version | awk '{print $2}')"
  [[ "$installed" == "$version" ]] || { echo "pytest version mismatch: got $installed want $version" >&2; exit 1; }
  mkdir -p "$src" "$out"
  cp "$TMP/outcomes_test.py" "$src/outcomes_test.py"
  cp "$TMP/duplicate_entry_test.py" "$src/duplicate_entry_test.py"

  for fixture in outcomes duplicate-entry; do
    case "$fixture" in
      outcomes) test_file="outcomes_test.py" ;;
      duplicate-entry) test_file="duplicate_entry_test.py" ;;
    esac
    raw="$TMP/$version-$fixture.raw.xml"
    final="$out/$fixture.xml"
    set +e
    env -u PYTEST_ADDOPTS "$venv/bin/pytest" "$src/$test_file" \
      --junitxml="$raw" -o junit_family=xunit2 -o addopts= -q >/dev/null 2>&1
    status=$?
    set -e
    [[ -s "$raw" ]] || { echo "pytest $version did not create $fixture XML" >&2; exit 1; }
    [[ "$status" -ne 0 ]] || { echo "pytest $version $fixture unexpectedly succeeded" >&2; exit 1; }
    normalize_xml "$raw" "$final" "$src"
    sha="$(shasum -a 256 "$final" | awk '{print $1}')"
    printf '%s\t%s\t%s\n' "$version" "$fixture.xml" "$sha" >> "$manifest_tmp"
  done
done

python3 - "$manifest_tmp" "$OUT_ROOT/manifest.json" <<'PY'
from pathlib import Path
import json, sys
entries_path, manifest_path = map(Path, sys.argv[1:3])
fixtures = []
for line in entries_path.read_text().splitlines():
    version, filename, sha = line.split("\t")
    test_file = "outcomes_test.py" if filename == "outcomes.xml" else "duplicate_entry_test.py"
    fixtures.append({
        "producer": "pytest",
        "producer_version": version,
        "junit_family": "xunit2",
        "addopts_override": "addopts=",
        "pytest_addopts": "absent",
        "fixture": f"pytest-{version}/{filename}",
        "sha256": sha,
        "generator_command": f"env -u PYTEST_ADDOPTS <venv>/bin/pytest <src>/{test_file} --junitxml=<raw.xml> -o junit_family=xunit2 -o addopts= -q",
        "normalization": "canonicalize time attributes, timestamp, hostname, and ephemeral source-root paths; preserve all outcome/type/message structure",
    })
manifest = {"schema_version": 1, "generator": "scripts/generate-pytest-junit-fixtures.sh", "fixtures": fixtures}
manifest_path.write_text(json.dumps(manifest, indent=2, sort_keys=True) + "\n")
PY

echo "generated pytest JUnit fixtures under $OUT_ROOT"
