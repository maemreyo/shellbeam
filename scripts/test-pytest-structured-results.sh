#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
TMP="$(mktemp -d /tmp/shellbeam-pytest-structured-results.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT
VERSIONS=("8.4.2" "9.1.1")

run_gate() {
  local label="$1"
  shift
  printf '== %s ==\n' "$label"
  "$@"
}

run_gate frozen_fixture_semantics \
  go test ./internal/adapter/structured/pytestjunit ./cmd/shellbeam \
    -run 'FrozenPytest|PytestStructuredResultsPublicIPC' -count=1

run_gate qualification_negative_matrix \
  go test ./internal/app/structuredresult ./internal/app/daemon ./internal/adapter/localfs ./internal/adapter/store \
    -run 'PytestInvocation|PytestAddopts|ExplicitPytestStructuredPrecondition|AutoPytestCandidate|ManagedPathCollision|ManagedPathClaimRelease|ArtifactBaselineRejects|ArtifactSourceHandlePins|ArtifactSourceOpenFails|ArtifactSourceIdentityChanges|MaterializerRejectsPhaseAIdentityDrift|ArtifactTerminalCaptureDeadline|ArtifactBlobRejectsSourceMutation|BlobBudget|ScanActiveSessionsDoesNotDecodeNonSessionMetadata' \
    -count=1

run_gate crash_retention_concurrency \
  go test ./internal/app/structuredresult ./internal/adapter/store \
    -run 'StructuredWorkerArtifactDuplicateScheduleRunsParserOnceAndRestartDoesNotRerunTerminal|StructuredWorkerArtifactRecoveryResumesProcessingWithSameKey|StructuredWorkerArtifactIdentityBindsTerminalAndObservationCuts|SessionRetentionCannotDestroyCommittedUnboundArtifactRecoveryAuthority|RecoverStructuredArtifacts|ArtifactRefAcquireAndRetirementBarrierSerialize|CompactionReleasesOnlyOwnBlobRefAndLastDetailRetiresBytes|ArtifactCompactionCannotBypassDetailReferenceProtocol|ResolveArtifactBlobStateFailsClosedOnRetainedTombstoneConflict|OpenRunsStructuredRecoveryBeforeServingStore' \
    -count=1

for version in "${VERSIONS[@]}"; do
  venv="$TMP/venv-$version"
  printf '== real_pytest_%s ==\n' "$version"
  python3 -m venv "$venv"
  "$venv/bin/python" -m pip install --disable-pip-version-check --quiet "pytest==$version"
  installed="$($venv/bin/pytest --version | awk '{print $2}')"
  [[ "$installed" == "$version" ]] || {
    printf 'pytest version mismatch: got=%s want=%s\n' "$installed" "$version" >&2
    exit 1
  }
  env -u PYTEST_ADDOPTS SHELLBEAM_PYTEST_REAL_BIN_DIR="$venv/bin" \
    go test ./cmd/shellbeam -run '^TestPytestStructuredResultsPublicIPC$' -count=1
done

printf 'pytest_structured_results verdict=PASS versions=%s\n' "${VERSIONS[*]}"
