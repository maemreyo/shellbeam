#!/bin/sh
set -eu
root=$(git rev-parse --show-toplevel)
cd "$root"
test -z "$(git status --porcelain)" || { echo 'working tree must be clean' >&2; exit 1; }
commit=$(git rev-parse HEAD)
short=$(git rev-parse --short=12 HEAD)
out=${1:-"$root/dist"}
mkdir -p "$out"
out=$(cd "$out" && pwd -P)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
git archive --format=tar --prefix=shellbeam/ "$commit" | tar -xf - -C "$stage"
cp "$root/.build/release/release-evidence.json" "$stage/shellbeam/release-evidence.json"
(cd "$stage/shellbeam" && find . -type f ! -name PACKAGE-MANIFEST.sha256 -print | LC_ALL=C sort | xargs sha256sum) > "$stage/shellbeam/PACKAGE-MANIFEST.sha256"
(cd "$stage" && zip -X -q -r "$out/shellbeam-v1-source-$short.zip" shellbeam)
(cd "$out" && sha256sum "shellbeam-v1-source-$short.zip" > "shellbeam-v1-source-$short.zip.sha256")
printf '%s\n' "$out/shellbeam-v1-source-$short.zip"
