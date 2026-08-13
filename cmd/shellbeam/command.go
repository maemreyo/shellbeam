package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	mcpadapter "github.com/maemreyo/shellbeam/internal/adapter/mcp"
	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	serviceadapter "github.com/maemreyo/shellbeam/internal/adapter/service"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	bridgeapp "github.com/maemreyo/shellbeam/internal/app/bridge"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/buildinfo"
	"github.com/maemreyo/shellbeam/internal/config"
	"github.com/oklog/ulid/v2"
)

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: shellbeam <daemon|mcp|install|uninstall|status|doctor|version>")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	switch args[0] {
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "daemon":
		err = runDaemon(ctx, args[1:])
	case "mcp":
		err = runMCP(ctx, args[1:])
	case "install":
		err = runInstall(ctx, args[1:], false)
	case "uninstall":
		err = runInstall(ctx, args[1:], true)
	case "status":
		err = runStatus(args[1:], stdout)
	case "doctor":
		err = runDoctor(args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "shellbeam %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func runVersion(args []string, out, errOut io.Writer) int {
	if len(args) == 0 {
		i := buildinfo.Current()
		fmt.Fprintf(out, "shellbeam %s (%s, %s)\n", i.Version, i.Commit, i.BuiltAt)
		return 0
	}
	if len(args) == 1 && args[0] == "--json" {
		if json.NewEncoder(out).Encode(buildinfo.Current()) != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintln(errOut, "usage: shellbeam version [--json]")
	return 2
}

type common struct{ configPath, stateDir, runtimeDir, shell string }

func parseCommon(name string, args []string) (common, error) {
	var c common
	var jsonOutput bool
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&c.configPath, "config", "", "config file")
	fs.StringVar(&c.stateDir, "state-dir", "", "state directory")
	fs.StringVar(&c.runtimeDir, "runtime-dir", "", "runtime directory")
	fs.StringVar(&c.shell, "shell", "", "shell")
	fs.BoolVar(&jsonOutput, "json", false, "JSON output")
	if err := fs.Parse(args); err != nil {
		return c, err
	}
	if fs.NArg() != 0 {
		return c, fmt.Errorf("unexpected arguments")
	}
	return c, nil
}
func loadCommon(name string, args []string) (config.Config, config.Paths, error) {
	c, err := parseCommon(name, args)
	if err != nil {
		return config.Config{}, config.Paths{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return config.Config{}, config.Paths{}, err
	}
	paths, err := config.ResolvePaths(runtime.GOOS, os.Getuid(), home, map[string]string{"XDG_CONFIG_HOME": os.Getenv("XDG_CONFIG_HOME"), "XDG_STATE_HOME": os.Getenv("XDG_STATE_HOME"), "XDG_RUNTIME_DIR": os.Getenv("XDG_RUNTIME_DIR")})
	if err != nil {
		return config.Config{}, paths, err
	}
	if c.configPath != "" {
		paths.ConfigFile = c.configPath
	}
	over := config.Overrides{}
	if c.stateDir != "" {
		over.StateDir = &c.stateDir
	}
	if c.runtimeDir != "" {
		over.RuntimeDir = &c.runtimeDir
	}
	if c.shell != "" {
		over.Shell = &c.shell
	}
	cfg, err := config.Load(paths.ConfigFile, over)
	if err != nil {
		return cfg, paths, err
	}
	if cfg.StateDir != "" {
		paths.StateDir = cfg.StateDir
	}
	if cfg.RuntimeDir != "" {
		paths.RuntimeDir = cfg.RuntimeDir
		paths.Socket = cfg.RuntimeDir + "/daemon.sock"
	}
	shell, err := processadapter.ResolveShell(cfg.Shell, os.Getenv("SHELL"))
	if err != nil {
		return cfg, paths, err
	}
	cfg.Shell = shell
	return cfg, paths, nil
}

func runDaemon(ctx context.Context, args []string) error {
	cfg, paths, err := loadCommon("daemon", args)
	if err != nil {
		return err
	}
	limits := storeadapter.Limits{MaxSessions: cfg.MaxConcurrentSessions, MaxSessionOutput: cfg.MaxSessionOutputBytes, MaxTotalState: cfg.MaxTotalStateBytes, ControlReserve: cfg.ControlReserveSessionBytes}
	store, err := storeadapter.Open(paths.StateDir, limits)
	if err != nil {
		return err
	}
	incarnation := ulid.Make().String()
	svc := daemonapp.NewService(store, processadapter.Owner{}, daemonapp.Options{Incarnation: incarnation, Shell: cfg.Shell, MaxQueuedInputBytes: cfg.MaxQueuedInputSessionBytes, TerminationGrace: time.Duration(cfg.TerminationGraceMS) * time.Millisecond})
	server, err := ipcadapter.Listen(paths.RuntimeDir, svc)
	if err != nil {
		return err
	}
	defer server.Close()
	if err = store.AbandonUnresolved(ctx, incarnation); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Duration(cfg.TerminationGraceMS)*time.Millisecond)
		defer cancel()
		_ = svc.Shutdown(shutdownCtx)
		_ = server.Close()
	}()
	return server.Serve()
}
func runMCP(ctx context.Context, args []string) error {
	_, paths, err := loadCommon("mcp", args)
	if err != nil {
		return err
	}
	return mcpadapter.Run(ctx, bridgeapp.New(ipcadapter.NewClient(paths.Socket)))
}
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
