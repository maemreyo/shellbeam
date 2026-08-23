package shellintegration

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	app "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	contextcore "github.com/maemreyo/shellbeam/internal/core/contextexec"
	core "github.com/maemreyo/shellbeam/internal/core/shellintegration"
)

type NushellAdapter struct{ deps Dependencies }

func NewNushellAdapter(deps Dependencies) (*NushellAdapter, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	if _, err := nushellLiteral(deps.Executable); err != nil {
		return nil, fmt.Errorf("nushell executable path: %w", err)
	}
	if _, err := nushellLiteral(deps.RuntimeDir); err != nil {
		return nil, fmt.Errorf("nushell runtime path: %w", err)
	}
	return &NushellAdapter{deps: deps}, nil
}

func (*NushellAdapter) Family() core.ShellFamily { return core.ShellNushell }

func (a *NushellAdapter) Install(_ context.Context, req app.WatchRequest) (app.RequirementWatcher, error) {
	if req.Shell.Family != core.ShellNushell {
		return nil, fmt.Errorf("nushell adapter shell mismatch")
	}
	watcher, _, err := newOneShotWatcherWithDialect(req, a.deps, nushellScripts, nushellNotifierInvocationForEvent, nushellWatcherDelivery)
	return watcher, err
}

func nushellScripts(req app.WatchRequest, eventID, trueNotify, _ string) (string, string) {
	marker := "__shellbeam_handoff_" + eventID
	armed := marker + "_armed"
	filter := nushellRemoveHook(marker)
	install := fmt.Sprintf(
		`$env.%s = false; let __shellbeam_hook = {|| let __shellbeam_marker = %s; if (not $env.%s) { $env.%s = true } else { let __shellbeam_satisfied = (($env.%s? | default "" | into string | str length) > 0); if $__shellbeam_satisfied { %s; %s; hide-env %s } } }; $env.config.hooks.pre_prompt = ($env.config.hooks.pre_prompt | append $__shellbeam_hook)`,
		armed, mustNushellLiteral(marker), armed, armed, req.Requirement.Name, trueNotify, filter, armed,
	)
	cleanup := fmt.Sprintf(`%s; if (%s in $env) { hide-env %s }`, filter, mustNushellLiteral(armed), armed)
	return install, cleanup
}

func nushellWatcherDelivery(install, installedNotify string) string {
	// Nushell has no eval command. A semicolon-separated top-level statement
	// keeps registration and acknowledgement in one REPL submission, so the
	// new pre_prompt hook cannot run between them.
	return install + "; " + installedNotify
}

func nushellNotifierInvocationForEvent(executable, socket string, req app.WatchRequest, eventID string, event NotificationEvent, satisfied bool) string {
	args := []string{
		"/usr/bin/env", "-i", executable, "__handoff_notify",
		"--socket", socket,
		"--handoff-id", req.HandoffID,
		"--epoch", fmt.Sprintf("%d", req.AuthorityEpoch),
		"--event-id", eventID,
		"--shell-runtime-id", req.Shell.RuntimeID,
		"--event", string(event),
		"--satisfied", fmt.Sprintf("%t", satisfied),
	}
	return nushellExternalInvocation(args...)
}

func nushellRemoveHook(marker string) string {
	return fmt.Sprintf(
		`$env.config.hooks.pre_prompt = ($env.config.hooks.pre_prompt | where {|h| if (($h | describe) == "closure") { not ((view source $h) | str contains %s) } else { true } })`,
		mustNushellLiteral(marker),
	)
}

func (a *NushellAdapter) ArmContextHelper(ctx context.Context, arm app.ContextHelperArmSpec) error {
	if a == nil {
		return fmt.Errorf("nushell context helper armer unavailable")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := arm.Validate(); err != nil {
		return err
	}
	if arm.Shell.Family != core.ShellNushell {
		return fmt.Errorf("context helper shell family mismatch")
	}
	if !safeOpaque(arm.OpaqueLaunchID, 128) {
		return fmt.Errorf("invalid context helper arm identity")
	}
	if err := a.deps.validate(); err != nil {
		return err
	}
	runtimeDir, err := nushellLiteral(a.deps.RuntimeDir)
	if err != nil {
		return err
	}
	executable, err := nushellLiteral(a.deps.Executable)
	if err != nil {
		return err
	}
	marker := contextHelperHookName(arm.OpaqueLaunchID)
	remove := nushellRemoveHook(marker)
	invocation := "^" + executable + " " + mustNushellLiteral("__context_exec_helper") + " " + mustNushellLiteral(arm.OpaqueLaunchID)
	script := fmt.Sprintf(
		`let __shellbeam_context_hook = {|| let __shellbeam_marker = %s; %s; with-env { %s: %s } { %s } }; $env.config.hooks.pre_prompt = ($env.config.hooks.pre_prompt | append $__shellbeam_context_hook)`,
		mustNushellLiteral(marker), remove, contextcore.HelperRuntimeDirEnvironment, runtimeDir, invocation,
	)
	return a.deps.Command.WriteShell(ctx, script)
}

func nushellExternalInvocation(args ...string) string {
	if len(args) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, mustNushellLiteral(arg))
	}
	return "^" + quoted[0] + " " + strings.Join(quoted[1:], " ")
}

func mustNushellLiteral(value string) string {
	literal, err := nushellLiteral(value)
	if err != nil {
		panic(err)
	}
	return literal
}

func nushellLiteral(value string) (string, error) {
	if !utf8.ValidString(value) || strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("value is not valid Nushell text")
	}
	var b strings.Builder
	b.Grow(len(value) + 2)
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				return "", fmt.Errorf("value contains unsupported control character")
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String(), nil
}
