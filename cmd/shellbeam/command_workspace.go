package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type workspaceCLIOptions struct {
	jsonOutput bool
	configPath string
	stateDir   string
	runtimeDir string
	shell      string
	label      string
	ref        string
	path       string
	force      bool
}

func runWorkspace(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return workspaceUsage()
	}
	switch args[0] {
	case "list":
		opts, err := parseWorkspaceFlags("workspace list", args[1:], nil)
		if err != nil {
			return err
		}
		service, err := openWorkspaceService(opts)
		if err != nil {
			return err
		}
		workspaces, err := service.List(ctx)
		if err != nil {
			return err
		}
		return writeWorkspaceList(out, opts.jsonOutput, workspaces)
	case "preflight":
		return runWorkspacePreflight(ctx, args[1:], out)
	case "inspect":
		if len(args) < 2 {
			return workspaceUsage()
		}
		return runWorkspaceInspect(ctx, args[1], args[2:], out)
	case "attach":
		if len(args) < 2 {
			return workspaceUsage()
		}
		return runWorkspaceAttach(ctx, args[1], args[2:], out)
	case "create":
		if len(args) < 2 {
			return workspaceUsage()
		}
		return runWorkspaceCreate(ctx, args[1], args[2:], out)
	case "rename":
		if len(args) < 3 {
			return workspaceUsage()
		}
		return runWorkspaceRename(ctx, args[1], args[2], args[3:], out)
	case "forget":
		if len(args) < 2 {
			return workspaceUsage()
		}
		return runWorkspaceForget(ctx, args[1], args[2:], out)
	case "remove":
		if len(args) < 2 {
			return workspaceUsage()
		}
		return runWorkspaceRemove(ctx, args[1], args[2:], out)
	default:
		return workspaceUsage()
	}
}

func runWorkspaceInspect(ctx context.Context, key string, args []string, out io.Writer) error {
	opts, err := parseWorkspaceFlags("workspace inspect", args, nil)
	if err != nil {
		return err
	}
	service, err := openWorkspaceService(opts)
	if err != nil {
		return err
	}
	record, err := service.Inspect(ctx, key)
	if err != nil {
		return err
	}
	return writeWorkspace(out, opts.jsonOutput, record)
}

func runWorkspaceAttach(ctx context.Context, path string, args []string, out io.Writer) error {
	opts, err := parseWorkspaceFlags("workspace attach", args, func(fs *flag.FlagSet, o *workspaceCLIOptions) {
		fs.StringVar(&o.label, "label", "", "workspace label")
	})
	if err != nil {
		return err
	}
	service, err := openWorkspaceService(opts)
	if err != nil {
		return err
	}
	record, err := service.Attach(ctx, path, opts.label)
	if err != nil {
		return err
	}
	return writeWorkspace(out, opts.jsonOutput, record)
}

func runWorkspaceCreate(ctx context.Context, repository string, args []string, out io.Writer) error {
	opts, err := parseWorkspaceFlags("workspace create", args, func(fs *flag.FlagSet, o *workspaceCLIOptions) {
		fs.StringVar(&o.ref, "ref", "", "Git ref")
		fs.StringVar(&o.path, "path", "", "worktree path")
		fs.StringVar(&o.label, "label", "", "workspace label")
	})
	if err != nil {
		return err
	}
	service, err := openWorkspaceService(opts)
	if err != nil {
		return err
	}
	record, err := service.Create(ctx, workspaceapp.CreateRequest{
		Repository: repository, Ref: opts.ref, Path: opts.path, Label: opts.label,
	})
	if err != nil {
		return err
	}
	return writeWorkspace(out, opts.jsonOutput, record)
}

func runWorkspaceRename(ctx context.Context, key, label string, args []string, out io.Writer) error {
	opts, err := parseWorkspaceFlags("workspace rename", args, nil)
	if err != nil {
		return err
	}
	service, err := openWorkspaceService(opts)
	if err != nil {
		return err
	}
	record, err := service.Rename(ctx, key, label)
	if err != nil {
		return err
	}
	return writeWorkspace(out, opts.jsonOutput, record)
}

func runWorkspaceForget(ctx context.Context, key string, args []string, out io.Writer) error {
	opts, err := parseWorkspaceFlags("workspace forget", args, nil)
	if err != nil {
		return err
	}
	service, err := openWorkspaceService(opts)
	if err != nil {
		return err
	}
	record, err := service.Forget(ctx, key)
	if err != nil {
		return err
	}
	return writeWorkspace(out, opts.jsonOutput, record)
}

func runWorkspaceRemove(ctx context.Context, key string, args []string, out io.Writer) error {
	opts, err := parseWorkspaceFlags("workspace remove", args, func(fs *flag.FlagSet, o *workspaceCLIOptions) {
		fs.BoolVar(&o.force, "force", false, "remove dirty worktree")
	})
	if err != nil {
		return err
	}
	service, err := openWorkspaceService(opts)
	if err != nil {
		return err
	}
	record, err := service.Remove(ctx, key, opts.force)
	if err != nil {
		return err
	}
	return writeWorkspace(out, opts.jsonOutput, record)
}

func parseWorkspaceFlags(name string, args []string, configure func(*flag.FlagSet, *workspaceCLIOptions)) (workspaceCLIOptions, error) {
	var opts workspaceCLIOptions
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&opts.jsonOutput, "json", false, "JSON output")
	fs.StringVar(&opts.configPath, "config", "", "config file")
	fs.StringVar(&opts.stateDir, "state-dir", "", "state directory")
	fs.StringVar(&opts.runtimeDir, "runtime-dir", "", "runtime directory")
	fs.StringVar(&opts.shell, "shell", "", "shell")
	if configure != nil {
		configure(fs, &opts)
	}
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() != 0 {
		return opts, workspaceUsage()
	}
	return opts, nil
}

func openWorkspaceService(opts workspaceCLIOptions) (*workspaceapp.Service, error) {
	commonArgs := make([]string, 0, 8)
	for _, pair := range [][2]string{{"--config", opts.configPath}, {"--state-dir", opts.stateDir}, {"--runtime-dir", opts.runtimeDir}, {"--shell", opts.shell}} {
		if pair[1] != "" {
			commonArgs = append(commonArgs, pair[0], pair[1])
		}
	}
	cfg, paths, err := loadCommon("workspace", commonArgs)
	if err != nil {
		return nil, err
	}
	registry, err := storeadapter.Open(paths.StateDir, storeadapter.Limits{
		MaxSessions: cfg.MaxConcurrentSessions, MaxSessionOutput: cfg.MaxSessionOutputBytes,
		MaxTotalState: cfg.MaxTotalStateBytes, ControlReserve: cfg.ControlReserveSessionBytes,
	})
	if err != nil {
		return nil, err
	}
	return workspaceapp.New(registry, gitadapter.New()), nil
}

func writeWorkspace(out io.Writer, jsonOutput bool, record core.Workspace) error {
	if jsonOutput {
		return json.NewEncoder(out).Encode(record)
	}
	_, err := fmt.Fprintf(out, "%s\t%s\t%s\n", record.Label, record.ID, record.Root)
	return err
}

func writeWorkspaceList(out io.Writer, jsonOutput bool, records []core.Workspace) error {
	if jsonOutput {
		return json.NewEncoder(out).Encode(records)
	}
	for _, record := range records {
		if err := writeWorkspace(out, false, record); err != nil {
			return err
		}
	}
	return nil
}

func workspaceUsage() error {
	return fmt.Errorf("usage: shellbeam workspace <list|inspect|preflight|attach|create|rename|forget|remove>")
}
