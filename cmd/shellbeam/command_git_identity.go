package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gitadapter "github.com/maemreyo/shellbeam/internal/adapter/git"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	identityapp "github.com/maemreyo/shellbeam/internal/app/gitidentity"
	coreworkspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type gitIdentityArgs struct {
	target string
	effect string
	deep   bool
	json   bool
	common []string
}

type identityWorkspaceStore interface {
	ListWorkspaces(context.Context) ([]coreworkspace.Workspace, error)
}

type identityWorkspaceLookup struct {
	store identityWorkspaceStore
}

func (l identityWorkspaceLookup) Inspect(ctx context.Context, key string) (coreworkspace.Workspace, error) {
	records, err := l.store.ListWorkspaces(ctx)
	if err != nil {
		return coreworkspace.Workspace{}, err
	}
	if key == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return coreworkspace.Workspace{}, err
		}
		if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
			cwd = resolved
		}
		return workspaceContaining(records, cwd)
	}
	var found coreworkspace.Workspace
	matches := 0
	for _, record := range records {
		if string(record.ID) == key {
			return record, nil
		}
		if record.Label == key {
			found = record
			matches++
		}
	}
	if matches == 1 {
		return found, nil
	}
	if matches > 1 {
		return coreworkspace.Workspace{}, fmt.Errorf("workspace label %q is ambiguous", key)
	}
	return coreworkspace.Workspace{}, fmt.Errorf("workspace %q not found", key)
}

func runWorkspacePreflight(ctx context.Context, args []string, out io.Writer) error {
	parsed, err := parseGitIdentityArgs(args)
	if err != nil {
		return err
	}
	cfg, paths, err := loadCommon("workspace preflight", parsed.common)
	if err != nil {
		return err
	}
	limits := storeadapter.Limits{
		MaxSessions:      cfg.MaxConcurrentSessions,
		MaxSessionOutput: cfg.MaxSessionOutputBytes,
		MaxTotalState:    cfg.MaxTotalStateBytes,
		ControlReserve:   cfg.ControlReserveSessionBytes,
	}
	store, err := storeadapter.Open(paths.StateDir, limits)
	if err != nil {
		return err
	}
	lookup := identityWorkspaceLookup{store: store}
	probe := gitadapter.NewIdentityProbe(nil, nil)
	service := identityapp.New(lookup, probe, identityapp.Profiles{Values: cfg.GitProfiles, RepositoryBindings: cfg.GitRepositoryProfiles, WorkspaceBindings: cfg.GitWorkspaceProfiles})
	result, err := service.Preflight(ctx, parsed.target, parsed.effect, parsed.deep)
	if err != nil {
		return err
	}
	if parsed.json {
		return json.NewEncoder(out).Encode(result)
	}
	return writeIdentityResult(out, result)
}

func parseGitIdentityArgs(args []string) (gitIdentityArgs, error) {
	var out gitIdentityArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--effect":
			i++
			if i >= len(args) {
				return out, fmt.Errorf("--effect requires a value")
			}
			out.effect = args[i]
		case "--deep":
			out.deep = true
		case "--json":
			out.json = true
		case "--config":
			i++
			if i >= len(args) {
				return out, fmt.Errorf("--config requires a value")
			}
			out.common = append(out.common, "--config", args[i])
		default:
			if strings.HasPrefix(arg, "-") {
				return out, fmt.Errorf("unknown workspace preflight option %q", arg)
			}
			if out.target != "" {
				return out, fmt.Errorf("multiple workspace targets")
			}
			out.target = arg
		}
	}
	switch out.effect {
	case "push", "pr", "tag", "release", "publish", "verify":
	default:
		return out, fmt.Errorf("--effect must be push, pr, tag, release, publish, or verify")
	}
	return out, nil
}

func workspaceContaining(records []coreworkspace.Workspace, cwd string) (coreworkspace.Workspace, error) {
	cwd = filepath.Clean(cwd)
	best := -1
	var found coreworkspace.Workspace
	for _, record := range records {
		root := filepath.Clean(record.Root)
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		if cwd != root && !strings.HasPrefix(cwd, root+string(os.PathSeparator)) {
			continue
		}
		if len(root) > best {
			best = len(root)
			found = record
		}
	}
	if best < 0 {
		return coreworkspace.Workspace{}, fmt.Errorf("current directory is not in a registered workspace")
	}
	return found, nil
}

func writeIdentityResult(out io.Writer, result identityapp.Result) error {
	profile := result.Resolution.ProfileName
	if profile == "" {
		profile = "unknown"
	}
	if _, err := fmt.Fprintf(out, "workspace=%s profile=%s source=%s effect=%s deep=%t findings=%d\n", result.WorkspaceID, profile, result.Resolution.Source, result.Effect, result.Deep, len(result.Findings)); err != nil {
		return err
	}
	for _, finding := range result.Findings {
		if _, err := fmt.Fprintf(out, "warning[%s]: %s\n", finding.Code, finding.Message); err != nil {
			return err
		}
	}
	return nil
}
