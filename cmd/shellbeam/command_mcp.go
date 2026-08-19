package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	ipcadapter "github.com/maemreyo/shellbeam/internal/adapter/ipc"
	mcpadapter "github.com/maemreyo/shellbeam/internal/adapter/mcp"
	bridgeapp "github.com/maemreyo/shellbeam/internal/app/bridge"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	terminalpresentation "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

const (
	defaultMCPBridgeAffinityFreshness = 15 * time.Minute
	mcpAncestorMaxDepth               = 16
	mcpAncestorProbeTimeout           = 2 * time.Second
)

type mcpTerminalAffinityDeps struct {
	platform  string
	now       func() time.Time
	getenv    func(string) string
	ancestors func(context.Context) ([]string, error)
}

func runMCP(ctx context.Context, args []string) error {
	_, paths, err := loadCommon("mcp", args)
	if err != nil {
		return err
	}
	handler, err := bridgeapp.NewNegotiated(ctx, ipcadapter.NewClient(paths.Socket), capability.V1MediaSupport())
	if err != nil {
		return err
	}
	hint, err := captureMCPBridgeAffinity(ctx, productionMCPAffinityDeps())
	if err != nil {
		return err
	}
	if hint != nil {
		if err := handler.SetTerminalAffinity(*hint); err != nil {
			return err
		}
	}
	return mcpadapter.Run(ctx, handler)
}

func productionMCPAffinityDeps() mcpTerminalAffinityDeps {
	return mcpTerminalAffinityDeps{
		platform:  runtime.GOOS,
		now:       time.Now,
		getenv:    os.Getenv,
		ancestors: readMCPAncestorExecutables,
	}
}

func captureMCPBridgeAffinity(ctx context.Context, deps mcpTerminalAffinityDeps) (*terminalpresentation.BridgeAffinityHint, error) {
	if deps.platform != "darwin" {
		return nil, nil
	}
	if deps.now == nil || deps.getenv == nil || deps.ancestors == nil {
		return nil, errors.New("invalid MCP terminal affinity dependencies")
	}
	ancestors, err := deps.ancestors(ctx)
	if err != nil {
		return nil, nil
	}
	launchContext := bridgeapp.TerminalLaunchContext{
		ObservedAt: deps.now(),
		Environment: map[string]string{
			"TERM_PROGRAM": deps.getenv("TERM_PROGRAM"),
		},
		AncestorExecutables: ancestors,
	}
	return bridgeapp.CaptureTerminalAffinity(launchContext, mcpBridgeTerminalProviders(), defaultMCPBridgeAffinityFreshness)
}

func mcpBridgeTerminalProviders() []terminalpresentation.TerminalIdentity {
	return []terminalpresentation.TerminalIdentity{{
		ProviderID:      "ghostty",
		ProviderVersion: 1,
		Platform:        terminalpresentation.PlatformDarwin,
		BundleID:        "com.mitchellh.ghostty",
		ExecutableName:  "ghostty",
	}}
}

func readMCPAncestorExecutables(ctx context.Context) ([]string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, mcpAncestorProbeTimeout)
	defer cancel()
	pid := os.Getppid()
	result := make([]string, 0, mcpAncestorMaxDepth)
	seen := make(map[int]struct{}, mcpAncestorMaxDepth)
	for depth := 0; depth < mcpAncestorMaxDepth && pid > 1; depth++ {
		if _, exists := seen[pid]; exists {
			return nil, errors.New("process ancestry cycle")
		}
		seen[pid] = struct{}{}
		parent, executable, err := readMCPProcessIdentity(probeCtx, pid)
		if err != nil {
			return nil, err
		}
		if executable != "" {
			result = append(result, executable)
		}
		if parent <= 1 || parent == pid {
			break
		}
		pid = parent
	}
	return result, nil
}

func readMCPProcessIdentity(ctx context.Context, pid int) (int, string, error) {
	output, err := exec.CommandContext(ctx, "/bin/ps", "-p", strconv.Itoa(pid), "-o", "ppid=", "-o", "comm=").Output()
	if err != nil {
		return 0, "", err
	}
	line := strings.TrimSpace(string(output))
	if line == "" || strings.ContainsRune(line, '\n') {
		return 0, "", errors.New("unexpected process ancestry shape")
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, "", errors.New("incomplete process ancestry shape")
	}
	parent, err := strconv.Atoi(fields[0])
	if err != nil || parent < 0 {
		return 0, "", errors.New("invalid process ancestry parent")
	}
	index := strings.Index(line, fields[0]) + len(fields[0])
	executable := strings.TrimSpace(line[index:])
	if executable == "" || strings.ContainsRune(executable, 0) {
		return 0, "", errors.New("invalid process ancestry executable")
	}
	return parent, executable, nil
}
