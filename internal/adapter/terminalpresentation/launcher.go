package terminalpresentation

import (
	"context"
	"errors"
	"os/exec"

	app "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

type LaunchProcess interface {
	Wait() error
}

type LaunchRunner interface {
	Start(context.Context, []string) (LaunchProcess, error)
}

type ExecLaunchRunner struct{}

func (ExecLaunchRunner) Start(ctx context.Context, argv []string) (LaunchProcess, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(argv) == 0 {
		return nil, errors.New("empty terminal launcher argv")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

type Launcher struct {
	platform string
	runner   LaunchRunner
}

func NewLauncher(platform string, runner LaunchRunner) *Launcher {
	return &Launcher{platform: platform, runner: runner}
}

func (l *Launcher) Launch(ctx context.Context, request app.LaunchRequest) (app.LaunchResult, error) {
	if err := ctx.Err(); err != nil {
		return app.LaunchResult{}, err
	}
	if err := request.Validate(); err != nil {
		return app.LaunchResult{}, err
	}
	provider, err := LookupQualifiedProvider(request.Identity)
	if err != nil {
		return app.LaunchResult{Attempted: false, Outcome: core.LaunchOutcomeFailed, ProviderID: request.Identity.ProviderID, Reason: "identity_not_qualified"}, err
	}
	if l == nil || l.runner == nil || l.platform != string(provider.Identity.Platform) {
		result := knownLaunchFailure(provider.Identity.ProviderID, "launcher_unavailable")
		return result, failure.New(failure.TerminalLauncherUnavailable, map[string]string{
			"provider_id": provider.Identity.ProviderID,
			"reason":      "platform_or_runner_unavailable",
		}, nil)
	}
	if err := provider.Validate(); err != nil {
		result := knownLaunchFailure(provider.Identity.ProviderID, "provider_invalid")
		return result, failure.New(failure.TerminalLauncherUnavailable, map[string]string{
			"provider_id": provider.Identity.ProviderID,
			"reason":      "provider_definition_invalid",
		}, err)
	}
	argv := make([]string, 0, 1+len(provider.ArgumentPrefix)+len(request.AttachArgv()))
	argv = append(argv, provider.LaunchExecutable)
	argv = append(argv, provider.ArgumentPrefix...)
	argv = append(argv, request.AttachArgv()...)
	process, err := l.runner.Start(ctx, argv)
	if err != nil {
		result := knownLaunchFailure(provider.Identity.ProviderID, "launcher_start_failed")
		return result, failure.New(failure.TerminalLaunchFailed, map[string]string{
			"provider_id": provider.Identity.ProviderID,
			"reason":      "launcher_start_failed",
		}, err)
	}
	if process == nil {
		result := knownLaunchFailure(provider.Identity.ProviderID, "launcher_start_failed")
		return result, failure.New(failure.TerminalLaunchFailed, map[string]string{
			"provider_id": provider.Identity.ProviderID,
			"reason":      "launcher_process_missing",
		}, nil)
	}
	waitErr := process.Wait()
	reason := "client_not_proven"
	if waitErr != nil {
		reason = "launcher_wait_failed"
	}
	result := app.LaunchResult{
		Attempted:  true,
		Outcome:    core.LaunchOutcomeUnknown,
		ProviderID: provider.Identity.ProviderID,
		Reason:     reason,
	}
	if err := result.Validate(); err != nil {
		return app.LaunchResult{}, err
	}
	return result, nil
}

func knownLaunchFailure(providerID, reason string) app.LaunchResult {
	return app.LaunchResult{
		Attempted:  false,
		Outcome:    core.LaunchOutcomeFailed,
		ProviderID: providerID,
		Reason:     reason,
	}
}
