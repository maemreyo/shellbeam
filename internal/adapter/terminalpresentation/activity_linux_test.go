//go:build linux

package terminalpresentation

import (
	"context"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/terminalpresentation"
)

func TestLinuxActivitySourceRemainsExplicitlyUnavailableUntilQualified(t *testing.T) {
	source := NewLinuxActivitySource()
	if _, err := source.Current(context.Background()); err == nil {
		t.Fatal("unqualified Linux activity source reported current evidence")
	}
	if err := source.Run(context.Background(), func(app.ForegroundObservation) error { return nil }); err == nil {
		t.Fatal("unqualified Linux activity stream reported available")
	}
}
