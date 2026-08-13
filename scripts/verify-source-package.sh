#!/bin/sh
set -eu
archive=${1:?usage: verify-source-package.sh archive.zip}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
unzip -q "$archive" -d "$tmp"
cd "$tmp/shellbeam"
sha256sum -c PACKAGE-MANIFEST.sha256
go mod verify
go test -count=1 ./...
go build -trimpath -buildvcs=false -o ./shellbeam ./cmd/shellbeam
./shellbeam version --json
./shellbeam doctor --json
test ! -e .git
echo 'source package clean-room verification: PASS'
