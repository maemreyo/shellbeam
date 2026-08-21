package shellintegration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type contextHelperArmBuilder func(name, invocation string) string

func armContextHelper(ctx context.Context, deps Dependencies, family core.ShellFamily, arm app.ContextHelperArmSpec, build contextHelperArmBuilder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := arm.Validate(); err != nil {
		return err
	}
	if arm.Shell.Family != family {
		return fmt.Errorf("context helper shell family mismatch")
	}
	if !safeOpaque(arm.OpaqueLaunchID, 128) {
		return fmt.Errorf("invalid context helper arm identity")
	}
	if build == nil {
		return fmt.Errorf("context helper arm builder unavailable")
	}
	if err := deps.validate(); err != nil {
		return err
	}
	invocation := contextHelperInvocation(deps.Executable, arm.OpaqueLaunchID)
	return deps.Command.WriteShell(ctx, build(contextHelperHookName(arm.OpaqueLaunchID), invocation))
}

func contextHelperInvocation(executable, opaqueLaunchID string) string {
	return strings.Join([]string{
		shellQuote(executable),
		shellQuote("__context_exec_helper"),
		shellQuote(opaqueLaunchID),
	}, " ")
}

func contextHelperHookName(opaqueLaunchID string) string {
	sum := sha256.Sum256([]byte(opaqueLaunchID))
	return "__shellbeam_context_" + hex.EncodeToString(sum[:12])
}
