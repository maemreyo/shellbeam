package shellintegration

import (
	"context"
	"fmt"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

func launchContextHelper(ctx context.Context, deps Dependencies, family core.ShellFamily, launch app.ContextHelperLaunch) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := launch.Validate(); err != nil {
		return err
	}
	if launch.Shell.Family != family {
		return fmt.Errorf("context helper shell family mismatch")
	}
	if !safeOpaque(launch.OpaqueLaunchID, 128) {
		return fmt.Errorf("invalid context helper launch identity")
	}
	if err := deps.validate(); err != nil {
		return err
	}
	return deps.Command.WriteShell(ctx, contextHelperInvocation(deps.Executable, launch.OpaqueLaunchID))
}

func contextHelperInvocation(executable, opaqueLaunchID string) string {
	return strings.Join([]string{
		shellQuote(executable),
		shellQuote("__context_exec_helper"),
		shellQuote(opaqueLaunchID),
	}, " ")
}
