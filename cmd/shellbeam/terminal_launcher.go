package main

import (
	"os"

	appterminal "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
)

type executableResolver func() (string, error)

func installedAttachArgv(handoffID string) ([]string, error) {
	return buildInstalledAttachArgv(handoffID, os.Executable)
}

func buildInstalledAttachArgv(handoffID string, executable executableResolver) ([]string, error) {
	path, err := executable()
	if err != nil {
		return nil, err
	}
	return appterminal.BuildAttachArgv(path, handoffID)
}
