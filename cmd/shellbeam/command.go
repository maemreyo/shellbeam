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

	processadapter "github.com/maemreyo/shellbeam/internal/adapter/process"
	"github.com/maemreyo/shellbeam/internal/buildinfo"
	"github.com/maemreyo/shellbeam/internal/config"
)

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: shellbeam <daemon|mcp|workspace|project|session|install|uninstall|status|doctor|version>")
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	switch args[0] {
	case "version":
		return runVersion(args[1:], stdout, stderr)
	case "__supervisor":
		err = runSupervisor(ctx, args[1:])
	case "__handoff_notify":
		err = runHandoffNotify(ctx, args[1:])
	case "__context_exec_helper":
		err = runContextExecHelper(ctx, args[1:])
	case "__context_exec_fdexec":
		err = runContextExecFDExec(args[1:])
	case "daemon":
		err = runDaemon(ctx, args[1:])
	case "mcp":
		err = runMCP(ctx, args[1:])
	case "workspace":
		err = runWorkspace(ctx, args[1:], stdout)
	case "project":
		err = runProject(ctx, args[1:], stdout)
	case "session":
		err = runSession(ctx, args[1:], stdout, stderr)
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
