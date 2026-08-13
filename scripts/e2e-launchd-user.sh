#!/bin/sh
set -eu
[ "$(uname -s)" = Darwin ] || exit 3
command -v launchctl >/dev/null 2>&1 || exit 3
echo 'launchd available; run shellbeam install, status, doctor, then uninstall in a disposable user account.'
