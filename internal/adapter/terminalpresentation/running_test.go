package terminalpresentation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestRunningSourceReturnsOnlyConfiguredRecognizedIdentities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lsappinfo-fixture")
	body := `#!/bin/sh
set -eu
if [ "$1" != find ]; then exit 9; fi
case "$2" in
bundleid=com.mitchellh.ghostty) echo 'ASN:0x0-0x123-"Ghostty":' ;;
bundleid=com.github.wez.wezterm) : ;;
esac
`
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	providers := []core.TerminalIdentity{
		testRunningIdentity("ghostty", "com.mitchellh.ghostty"),
		testRunningIdentity("wezterm", "com.github.wez.wezterm"),
	}
	source, err := NewRunningSource(RunningConfig{QueryPath: path, Providers: providers, CommandTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.Running(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ProviderID != "ghostty" || got[0].ExecutableName != "ghostty" {
		t.Fatalf("running=%+v", got)
	}
}

func TestRunningSourceRejectsInvalidProviderConfiguration(t *testing.T) {
	bad := core.TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "/Applications/Ghostty.app/ghostty"}
	if _, err := NewRunningSource(RunningConfig{QueryPath: "/usr/bin/lsappinfo", Providers: []core.TerminalIdentity{bad}, CommandTimeout: time.Second}); err == nil {
		t.Fatal("arbitrary executable path accepted in provider identity")
	}
}

func testRunningIdentity(id, bundle string) core.TerminalIdentity {
	return core.TerminalIdentity{ProviderID: id, ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: bundle, ExecutableName: id}
}

func TestRunningSourceFailsClosedOnUnexpectedQueryShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lsappinfo-fixture")
	body := "#!/bin/sh\nset -eu\necho '/private/arbitrary/app/path'\n"
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := NewRunningSource(RunningConfig{QueryPath: path, Providers: []core.TerminalIdentity{testRunningIdentity("ghostty", "com.mitchellh.ghostty")}, CommandTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Running(context.Background()); err == nil {
		t.Fatal("unexpected query output accepted as running identity")
	}
}
