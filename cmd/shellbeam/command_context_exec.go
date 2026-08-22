package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	contextadapter "github.com/maemreyo/shellbeam/internal/adapter/contextexec"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
)

func runContextExecHelper(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: shellbeam __context_exec_helper <opaque-launch-id>")
	}
	launchID := args[0]
	if err := contextadapter.ValidateOpaqueLaunchID(launchID); err != nil {
		return err
	}
	runtimeDir := os.Getenv(contextcore.HelperRuntimeDirEnvironment)
	if runtimeDir == "" || !filepath.IsAbs(runtimeDir) || filepath.Clean(runtimeDir) != runtimeDir {
		return fmt.Errorf("context helper runtime locator unavailable")
	}
	conn, err := contextadapter.DialPrivate(runtimeDir, launchID)
	if err != nil {
		return err
	}
	defer conn.Close()
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	if err := contextadapter.VerifyDaemonPeer(ctx, conn, executable, processadapter.NewHostInspector().Observe); err != nil {
		return err
	}
	client := contextadapter.Client{Conn: conn, OpaqueLaunchID: launchID}
	request, err := client.Authenticate(ctx)
	if err != nil {
		return err
	}
	if !contextadapter.ExecutableMatches(request.Helper.ExecutablePath, executable) {
		return fmt.Errorf("context helper executable binding mismatch")
	}
	runtime := contextadapter.Runtime{Launcher: contextadapter.NewPlatformLauncher(executable), HelperExecutable: executable}
	terminal, err := runtime.Execute(ctx, request, &client)
	if err != nil {
		return err
	}
	return client.SendTerminal(terminal)
}

func runContextExecFDExec(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("context fdexec argv missing")
	}
	_ = closeContextFD(3)
	return contextadapter.ExecveatFD(4, args, os.Environ())
}
