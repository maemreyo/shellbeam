package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	projectadapter "github.com/maemreyo/shellbeam/internal/adapter/project"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	"github.com/maemreyo/shellbeam/internal/buildinfo"
	coreproject "github.com/maemreyo/shellbeam/internal/core/project"
)

type projectCLIOptions struct {
	workspace   string
	fingerprint string
	jsonOutput  bool
	configPath  string
	stateDir    string
	runtimeDir  string
	shell       string
}

func runProject(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return projectUsage()
	}
	action := args[0]
	if action != "inspect" && action != "validate" && action != "review" {
		return projectUsage()
	}
	opts, err := parseProjectFlags("project "+action, args[1:])
	if err != nil {
		return err
	}
	if action == "review" && opts.fingerprint == "" {
		return fmt.Errorf("project review requires --fingerprint")
	}
	service, lookup, err := openProjectService(opts)
	if err != nil {
		return err
	}
	workspace, err := lookup.Inspect(ctx, opts.workspace)
	if err != nil {
		return err
	}

	var inspection coreproject.Inspection
	switch action {
	case "inspect":
		inspection, err = service.Inspect(ctx, string(workspace.ID))
	case "validate":
		inspection, err = service.Inspect(ctx, string(workspace.ID))
		if err == nil && (inspection.Status == coreproject.StatusAbsent || inspection.Status == coreproject.StatusInvalid) {
			err = fmt.Errorf("project manifest status is %s", inspection.Status)
		}
	case "review":
		inspection, err = service.Review(ctx, string(workspace.ID), projectapp.ReviewRequest{
			Fingerprint:   opts.fingerprint,
			ReviewedAt:    time.Now().UTC(),
			ToolVersion:   buildinfo.Current().Version,
			ReviewerClass: "user",
			SourceClass:   "cli",
		})
	}
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return json.NewEncoder(out).Encode(inspection)
	}
	_, err = fmt.Fprintf(out, "status=%s discovery_fingerprint=%s review_fingerprint=%s\n",
		inspection.Status, inspection.DiscoveryFingerprint, inspection.ReviewFingerprint)
	return err
}

func parseProjectFlags(name string, args []string) (projectCLIOptions, error) {
	var opts projectCLIOptions
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.workspace, "workspace", "", "workspace label or id")
	fs.StringVar(&opts.fingerprint, "fingerprint", "", "current discovery fingerprint")
	fs.BoolVar(&opts.jsonOutput, "json", false, "JSON output")
	fs.StringVar(&opts.configPath, "config", "", "config file")
	fs.StringVar(&opts.stateDir, "state-dir", "", "state directory")
	fs.StringVar(&opts.runtimeDir, "runtime-dir", "", "runtime directory")
	fs.StringVar(&opts.shell, "shell", "", "shell")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() != 0 {
		return opts, projectUsage()
	}
	return opts, nil
}

func openProjectService(opts projectCLIOptions) (*projectapp.Service, identityWorkspaceLookup, error) {
	commonArgs := make([]string, 0, 8)
	for _, pair := range [][2]string{
		{"--config", opts.configPath},
		{"--state-dir", opts.stateDir},
		{"--runtime-dir", opts.runtimeDir},
		{"--shell", opts.shell},
	} {
		if pair[1] != "" {
			commonArgs = append(commonArgs, pair[0], pair[1])
		}
	}
	cfg, paths, err := loadCommon("project", commonArgs)
	if err != nil {
		return nil, identityWorkspaceLookup{}, err
	}
	store, err := storeadapter.Open(paths.StateDir, storeadapter.Limits{
		MaxSessions:      cfg.MaxConcurrentSessions,
		MaxSessionOutput: cfg.MaxSessionOutputBytes,
		MaxTotalState:    cfg.MaxTotalStateBytes,
		ControlReserve:   cfg.ControlReserveSessionBytes,
	})
	if err != nil {
		return nil, identityWorkspaceLookup{}, err
	}
	lookup := identityWorkspaceLookup{store: store}
	return projectapp.New(store, projectadapter.NewLoader(), store), lookup, nil
}

func projectUsage() error {
	return fmt.Errorf("usage: shellbeam project <inspect|validate|review> [--workspace <label-or-id>] [--fingerprint <current-fingerprint>] [--json]")
}
