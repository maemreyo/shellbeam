//go:build darwin && shellbeam_native_test

package main

// Native acceptance binaries must never launch host GUI applications. Pointing
// the running-terminal probe at a command that deterministically reports no
// running provider keeps terminal presentation unavailable while preserving the
// production composition path in ordinary builds.
const terminalLSAppInfoPath = "/usr/bin/false"
