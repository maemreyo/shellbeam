package main

import (
	"errors"
	"reflect"
	"testing"

	terminaladapter "github.com/maemreyo/shellbeam/internal/adapter/terminalpresentation"
)

func TestBuildInstalledAttachArgvUsesExactRuntimeExecutable(t *testing.T) {
	got, err := buildInstalledAttachArgv("handoff-task5-cmd", func() (string, error) {
		return "/Applications/ShellBeam.app/Contents/MacOS/shellbeam", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/Applications/ShellBeam.app/Contents/MacOS/shellbeam", "session", "attach", "--handoff-id", "handoff-task5-cmd"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv=%q want=%q", got, want)
	}
}

func TestBuildInstalledAttachArgvFailsClosedOnExecutableResolutionFailure(t *testing.T) {
	boom := errors.New("executable unavailable")
	if _, err := buildInstalledAttachArgv("handoff-safe", func() (string, error) { return "", boom }); !errors.Is(err, boom) {
		t.Fatalf("err=%v want=%v", err, boom)
	}
	if _, err := buildInstalledAttachArgv("handoff-safe", func() (string, error) { return "shellbeam", nil }); err == nil {
		t.Fatal("relative runtime executable accepted")
	}
}

func TestMCPBridgeAffinityUsesQualifiedLauncherRegistry(t *testing.T) {
	got := mcpBridgeTerminalProviders()
	want := terminaladapter.QualifiedIdentities()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bridge providers=%+v launcher providers=%+v", got, want)
	}
}
