//go:build darwin && shellbeam_native_test

package main

import "testing"

func TestNativeAcceptanceBuildDisablesHostTerminalPresentationProbe(t *testing.T) {
	if terminalLSAppInfoPath != "/usr/bin/false" {
		t.Fatalf("native acceptance terminal probe=%q", terminalLSAppInfoPath)
	}
}
