package main

import (
	"context"
	"fmt"
	"os"

	contextadapter "github.com/maemreyo/shellbeam/internal/adapter/contextexec"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
)

func runContextExecHelper(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: shellbeam __context_exec_helper <opaque-launch-id>")
	}
	launchID := args[0]
	if err := contextadapter.ValidateOpaqueLaunchID(launchID); err != nil {
		return err
	}
	_, paths, err := loadCommon("__context_exec_helper", nil)
	if err != nil {
		return err
	}
	conn, err := contextadapter.DialPrivate(paths.RuntimeDir, launchID)
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
	terminal, err := runtime.Execute(ctx, request, func(frame contextadapter.OutputFrame) error { return client.SendOutput(frame) })
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
