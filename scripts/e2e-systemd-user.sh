#!/bin/sh
set -eu
command -v systemctl >/dev/null 2>&1 || exit 3
systemctl --user show-environment >/dev/null 2>&1 || exit 3
echo 'systemd user manager available; run shellbeam install, status, doctor, then uninstall in a disposable user environment.'
