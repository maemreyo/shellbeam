package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"runtime"

	serviceadapter "github.com/maemreyo/shellbeam/internal/adapter/service"
)

func runInstall(ctx context.Context, args []string, uninstall bool) error {
	_, paths, err := loadCommon("service", args)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if uninstall {
		return serviceadapter.Uninstall(ctx, runtime.GOOS, home, serviceadapter.ExecRunner{})
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return serviceadapter.Install(ctx, runtime.GOOS, home, exe, paths.ConfigFile, serviceadapter.ExecRunner{})
}

func runStatus(args []string, out io.Writer) error {
	_, paths, err := loadCommon("status", args)
	if err != nil {
		return err
	}
	info, err := os.Lstat(paths.Socket)
	status := "stopped"
	if err == nil && info.Mode()&os.ModeSocket != 0 {
		status = "socket_present"
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return json.NewEncoder(out).Encode(map[string]any{"schema_version": 1, "status": status})
}
