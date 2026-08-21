#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
TMP="$(mktemp -d /tmp/shellbeam-jest-structured-results.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT
VERSIONS=("29.7.0" "30.4.2")
MAX_BLOB_BYTES=$((16 << 20))

run_gate() {
  local label="$1"
  shift
  printf '== %s ==\n' "$label"
  "$@"
}

run_gate frozen_fixture_semantics \
  go test ./internal/adapter/structured/jestjson ./cmd/shellbeam \
    -run 'JestQualificationFixtures|JestQualificationManifest|JestStructuredResultsPublicIPC|JestFrozenFixture' -count=1

run_gate qualification_negative_matrix \
  go test ./internal/app/structuredresult ./internal/app/daemon ./internal/adapter/localfs ./internal/adapter/store ./cmd/shellbeam \
    -run 'JestInvocation|JestCandidate|JestJasmine|JestCaptureRequest|ExplicitJestStructuredPrecondition|ArtifactBaselineRejects|ArtifactSourceHandlePins|ArtifactSourceOpenFails|ArtifactSourceIdentityChanges|ManagedPathCollision|ManagedPathClaimRelease|PreSpawnManagedCollision|TerminalCaptureDeadlineDoesNotResurrectLateSuccess|MaterializerRejectsPhaseAIdentityDrift|ArtifactBlobRejectsSourceMutation|BlobBudget|StructuredStoreCeilingsSupportJS|StructuredLimitsPermitJS|JestStructuredCaptureRuntimeRejectsJasmineEnvironment' \
    -count=1

run_gate crash_retention_concurrency \
  go test ./internal/app/structuredresult ./internal/adapter/store \
    -run 'StructuredWorkerArtifactDuplicateScheduleRunsParserOnceAndRestartDoesNotRerunTerminal|StructuredWorkerArtifactRecoveryResumesProcessingWithSameKey|StructuredWorkerArtifactIdentityBindsTerminalAndObservationCuts|SessionRetentionCannotDestroyCommittedUnboundArtifactRecoveryAuthority|RecoverStructuredArtifacts|ArtifactRefAcquireAndRetirementBarrierSerialize|CompactionReleasesOnlyOwnBlobRefAndLastDetailRetiresBytes|ArtifactCompactionCannotBypassDetailReferenceProtocol|ResolveArtifactBlobStateFailsClosedOnRetainedTombstoneConflict|OpenRunsStructuredRecoveryBeforeServingStore' \
    -count=1

probe_real_jest() {
  local package_version="$1" install="$2" src="$3" jest_bin="$4"
  local producer_version
  producer_version="$("$jest_bin" --version | awk '{print $1}')"
  printf '== producer_probe package=%s producer=%s ==\n' "$package_version" "$producer_version"

  cat > "$src/jest.config.cjs" <<'JS'
module.exports = {}
JS
  cat > "$src/pass.test.js" <<'JS'
test('real producer pass', () => { expect(2 + 2).toBe(4) })
JS
  cat > "$src/bigint.test.js" <<'JS'
test('bigint payload cannot be JSON serialized', () => { expect(1n).toBe(1n) })
JS
  cat > "$src/global-setup.cjs" <<'JS'
module.exports = async () => { throw new Error('global setup acceptance throw') }
JS
  cat > "$src/global-setup.config.cjs" <<'JS'
module.exports = { globalSetup: '<rootDir>/global-setup.cjs' }
JS
  printf '%s\n' '--bail' > "$src/args.txt"

  local zero="$TMP/zero-$package_version.json"
  rm -f "$zero"
  set +e
  (cd "$src" && env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$zero" __definitely_no_such_test_file__ >/dev/null 2>&1)
  local zero_status=$?
  set -e
  [[ "$zero_status" -ne 0 && -s "$zero" ]]
  python3 - "$zero" <<'PY'
import json,sys
v=json.load(open(sys.argv[1], encoding='utf-8'))
assert v['numTotalTests'] == 0
assert v['testResults'] == []
PY

  local argfile="$TMP/argfile-$package_version.json" explicit="$TMP/explicit-bail-$package_version.json"
  rm -f "$argfile" "$explicit"
  set +e
  (cd "$src" && env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$argfile" @args.txt >/dev/null 2>&1)
  local argfile_status=$?
  set -e
  [[ "$argfile_status" -ne 0 && -s "$argfile" ]]
  (cd "$src" && env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$explicit" --bail pass.test.js >/dev/null 2>&1)
  python3 - "$argfile" "$explicit" <<'PY'
import json,sys
arg=json.load(open(sys.argv[1], encoding='utf-8'))
ctl=json.load(open(sys.argv[2], encoding='utf-8'))
assert arg['numTotalTests'] == 0
assert ctl['numTotalTests'] == 1
PY

  local bigint="$TMP/bigint-$package_version.json"
  rm -f "$bigint"
  set +e
  (cd "$src" && env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$bigint" bigint.test.js >/dev/null 2>&1)
  local bigint_status=$?
  set -e
  [[ "$bigint_status" -ne 0 && ! -e "$bigint" ]]

  local setup="$TMP/global-setup-$package_version.json"
  rm -f "$setup"
  set +e
  (cd "$src" && env -u JEST_JASMINE "$jest_bin" --runInBand --config="$src/global-setup.config.cjs" --json --outputFile="$setup" pass.test.js >/dev/null 2>&1)
  local setup_status=$?
  set -e
  [[ "$setup_status" -ne 0 && ! -e "$setup" ]]

  # Deliberate release qualification also measures coverage payload size on the
  # frozen 8200-test source shape. Only the ceiling decision is asserted here;
  # exact measured bytes are recorded in the implementation plan.
  python3 - "$src/coverage.test.js" <<'PY'
from pathlib import Path
p=Path(__import__('sys').argv[1])
p.write_text("\n".join(f"test('case {i:05d}', () => expect({i}).toBe({i}))" for i in range(8200))+"\n", encoding='utf-8')
PY
  local off="$TMP/coverage-off-$package_version.json" on="$TMP/coverage-on-$package_version.json"
  (cd "$src" && env -u JEST_JASMINE "$jest_bin" --runInBand --json --outputFile="$off" coverage.test.js >/dev/null 2>&1)
  (cd "$src" && env -u JEST_JASMINE "$jest_bin" --runInBand --coverage --json --outputFile="$on" coverage.test.js >/dev/null 2>&1)
  local off_bytes on_bytes
  off_bytes="$(wc -c < "$off" | tr -d ' ')"
  on_bytes="$(wc -c < "$on" | tr -d ' ')"
  [[ "$off_bytes" -lt "$MAX_BLOB_BYTES" && "$on_bytes" -lt "$MAX_BLOB_BYTES" ]]
  printf 'coverage package=%s off_bytes=%s on_bytes=%s ceiling=%s decision=adequate\n' "$package_version" "$off_bytes" "$on_bytes" "$MAX_BLOB_BYTES"
}

for package_version in "${VERSIONS[@]}"; do
  install="$TMP/install-$package_version"
  src="$TMP/src-$package_version"
  mkdir -p "$install" "$src"
  printf '== real_jest_install_%s ==\n' "$package_version"
  (
    cd "$install"
    npm init -y >/dev/null 2>&1
    npm install --silent --no-audit --no-fund "jest@$package_version"
  )
  jest_bin="$install/node_modules/.bin/jest"
  installed_package="$(node -p "require('$install/node_modules/jest/package.json').version")"
  [[ "$installed_package" == "$package_version" ]] || {
    printf 'jest package version mismatch: got=%s want=%s\n' "$installed_package" "$package_version" >&2
    exit 1
  }

  probe_real_jest "$package_version" "$install" "$src" "$jest_bin"

  env -u JEST_JASMINE SHELLBEAM_JEST_REAL_BIN_DIR="$install/node_modules/.bin" \
    go test ./cmd/shellbeam -run '^TestJestStructuredResultsPublicIPCRealProducer$' -count=1
 done

printf 'jest_structured_results verdict=PASS package_versions=%s\n' "${VERSIONS[*]}"
