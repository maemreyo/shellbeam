package terminalpresentation

import (
	"context"
	"errors"
	"reflect"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestLauncherBuildsExactGhosttyArgvWithoutShell(t *testing.T) {
	identity := QualifiedProviders()[0].Identity
	attach, err := app.BuildAttachArgv("/opt/shellbeam/bin/shellbeam", "handoff-task5-safe")
	if err != nil {
		t.Fatal(err)
	}
	req, err := app.NewLaunchRequest(identity, attach)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeLaunchRunner{process: &fakeLaunchProcess{}}
	launcher := NewLauncher("darwin", runner)
	got, err := launcher.Launch(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Attempted || got.Outcome != core.LaunchOutcomeUnknown || got.ProviderID != "ghostty" {
		t.Fatalf("result=%+v", got)
	}
	want := []string{"/usr/bin/open", "-n", "-b", "com.mitchellh.ghostty", "--args", "-e", "/opt/shellbeam/bin/shellbeam", "session", "attach", "--handoff-id", "handoff-task5-safe"}
	if !reflect.DeepEqual(runner.argv, want) {
		t.Fatalf("argv=%q want=%q", runner.argv, want)
	}
	for _, value := range runner.argv {
		if value == "/bin/sh" || value == "-c" {
			t.Fatalf("shell launcher token present: %q", runner.argv)
		}
	}
}

func TestLauncherStartFailureIsKnownNotAttemptedFailure(t *testing.T) {
	identity := QualifiedProviders()[0].Identity
	attach, _ := app.BuildAttachArgv("/opt/shellbeam/bin/shellbeam", "handoff-safe")
	req, _ := app.NewLaunchRequest(identity, attach)
	launcher := NewLauncher("darwin", &fakeLaunchRunner{startErr: errors.New("spawn denied")})
	got, err := launcher.Launch(context.Background(), req)
	if !errors.Is(err, failure.TerminalLaunchFailed) {
		t.Fatalf("err=%v", err)
	}
	if got.Attempted || got.Outcome != core.LaunchOutcomeFailed || got.Reason != "launcher_start_failed" {
		t.Fatalf("result=%+v", got)
	}
}

func TestLauncherStartedProcessNeverClaimsClientProvenFromExitStatus(t *testing.T) {
	identity := QualifiedProviders()[0].Identity
	attach, _ := app.BuildAttachArgv("/opt/shellbeam/bin/shellbeam", "handoff-safe")
	req, _ := app.NewLaunchRequest(identity, attach)
	for _, waitErr := range []error{nil, errors.New("open exited nonzero"), context.Canceled} {
		runner := &fakeLaunchRunner{process: &fakeLaunchProcess{waitErr: waitErr}}
		got, err := NewLauncher("darwin", runner).Launch(context.Background(), req)
		if err != nil {
			t.Fatalf("waitErr=%v launch err=%v", waitErr, err)
		}
		if !got.Attempted || got.Outcome != core.LaunchOutcomeUnknown || got.Reason == "" {
			t.Fatalf("waitErr=%v result=%+v", waitErr, got)
		}
	}
}

func TestLauncherRejectsUnsupportedPlatformAndIdentityBeforeSpawn(t *testing.T) {
	identity := QualifiedProviders()[0].Identity
	attach, _ := app.BuildAttachArgv("/opt/shellbeam/bin/shellbeam", "handoff-safe")
	req, _ := app.NewLaunchRequest(identity, attach)
	runner := &fakeLaunchRunner{}
	if got, err := NewLauncher("linux", runner).Launch(context.Background(), req); !errors.Is(err, failure.TerminalLauncherUnavailable) || got.Attempted || len(runner.argv) != 0 {
		t.Fatalf("linux result=%+v err=%v argv=%q", got, err, runner.argv)
	}

	changed := identity
	changed.BundleID = "com.example.fake"
	badReq, err := app.NewLaunchRequest(changed, attach)
	if err != nil {
		t.Fatal(err)
	}
	runner = &fakeLaunchRunner{}
	if got, err := NewLauncher("darwin", runner).Launch(context.Background(), badReq); !errors.Is(err, failure.TerminalIdentityAmbiguous) || got.Attempted || len(runner.argv) != 0 {
		t.Fatalf("mismatch result=%+v err=%v argv=%q", got, err, runner.argv)
	}
}

func TestLauncherPropagatesPreCanceledContextWithoutSpawn(t *testing.T) {
	identity := QualifiedProviders()[0].Identity
	attach, _ := app.BuildAttachArgv("/opt/shellbeam/bin/shellbeam", "handoff-safe")
	req, _ := app.NewLaunchRequest(identity, attach)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &fakeLaunchRunner{}
	got, err := NewLauncher("darwin", runner).Launch(ctx, req)
	if !errors.Is(err, context.Canceled) || got.Attempted || len(runner.argv) != 0 {
		t.Fatalf("result=%+v err=%v argv=%q", got, err, runner.argv)
	}
}

type fakeLaunchRunner struct {
	argv     []string
	process  LaunchProcess
	startErr error
}

func (f *fakeLaunchRunner) Start(_ context.Context, argv []string) (LaunchProcess, error) {
	f.argv = append([]string(nil), argv...)
	return f.process, f.startErr
}

type fakeLaunchProcess struct{ waitErr error }

func (f *fakeLaunchProcess) Wait() error { return f.waitErr }
