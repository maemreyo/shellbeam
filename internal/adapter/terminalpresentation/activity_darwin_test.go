//go:build darwin

package terminalpresentation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
	core "github.com/maemreyo/shellbeam/internal/core/terminalpresentation"
)

func TestDarwinActivitySourceCurrentMapsOnlyKnownBundleIdentity(t *testing.T) {
	ghostty := testGhosttyIdentity()
	script := writeLSAppInfoFixture(t, `
case "$1" in
front) echo 'ASN:0x0-0x123:' ;;
info) echo '"CFBundleIdentifier"="com.mitchellh.ghostty"' ;;
*) exit 9 ;;
esac
`)
	source, err := NewDarwinActivitySource(DarwinConfig{LSAppInfoPath: script, Providers: []core.TerminalIdentity{ghostty}, CommandTimeout: time.Second, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity == nil || got.Identity.ProviderID != "ghostty" || got.Quality != core.QualityNative {
		t.Fatalf("current=%+v", got)
	}
}

func TestDarwinActivitySourceCurrentTreatsKnownNonterminalAsNoTerminalCandidate(t *testing.T) {
	script := writeLSAppInfoFixture(t, `
case "$1" in
front) echo 'ASN:0x0-0x456:' ;;
info) echo '"CFBundleIdentifier"="org.mozilla.firefox"' ;;
*) exit 9 ;;
esac
`)
	source, err := NewDarwinActivitySource(DarwinConfig{LSAppInfoPath: script, Providers: []core.TerminalIdentity{testGhosttyIdentity()}, CommandTimeout: time.Second, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.Current(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity != nil {
		t.Fatalf("nonterminal became authority candidate: %+v", got)
	}
}

func TestDarwinActivitySourceRunEmitsTerminalThenNonterminal(t *testing.T) {
	script := writeLSAppInfoFixture(t, `
if [ "$1" = listen ]; then
  echo 'Notification: kLSNotifyBecameFrontmost dataRef={ "CFBundleIdentifier"="com.mitchellh.ghostty" } affectedASN="Ghostty" ASN:0x1:'
  echo 'Notification: kLSNotifyBecameFrontmost dataRef={ "CFBundleIdentifier"="org.mozilla.firefox" } affectedASN="Firefox" ASN:0x2:'
  exit 0
fi
exit 9
`)
	now := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	source, err := NewDarwinActivitySource(DarwinConfig{LSAppInfoPath: script, Providers: []core.TerminalIdentity{testGhosttyIdentity()}, CommandTimeout: time.Second, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	var got []app.ForegroundObservation
	if err := source.Run(context.Background(), func(event app.ForegroundObservation) error { got = append(got, event); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Identity == nil || got[0].Identity.ProviderID != "ghostty" || got[1].Identity != nil {
		t.Fatalf("events=%+v", got)
	}
}

func TestDarwinActivitySourceRunFailsClosedOnMalformedNotification(t *testing.T) {
	script := writeLSAppInfoFixture(t, `
if [ "$1" = listen ]; then echo 'Notification: kLSNotifyBecameFrontmost malformed'; exit 0; fi
exit 9
`)
	source, err := NewDarwinActivitySource(DarwinConfig{LSAppInfoPath: script, Providers: []core.TerminalIdentity{testGhosttyIdentity()}, CommandTimeout: time.Second, Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Run(context.Background(), func(app.ForegroundObservation) error { return nil }); err == nil {
		t.Fatal("malformed LaunchServices notification accepted")
	}
}

func writeLSAppInfoFixture(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lsappinfo-fixture")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func testGhosttyIdentity() core.TerminalIdentity {
	return core.TerminalIdentity{ProviderID: "ghostty", ProviderVersion: 1, Platform: core.PlatformDarwin, BundleID: "com.mitchellh.ghostty", ExecutableName: "ghostty"}
}
