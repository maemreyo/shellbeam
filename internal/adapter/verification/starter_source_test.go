package verification

import (
	"context"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/verification"
	core "github.com/maemreyo/shellbeam/internal/core/verification"
)

func TestStarterSourceReturnsReadOnlyRenderedPreview(t *testing.T) {
	got, err := NewStarterSource().Preview(context.Background(), "team", "repo_01K00000000000000000000000", manifestV2ForStarter())
	if err != nil {
		t.Fatal(err)
	}
	if got.State != app.PolicyLoadValid || got.Proposal == nil || got.Proposal.Origin != core.ProposalStarterProfile || got.RenderedTOML == "" {
		t.Fatalf("got=%#v", got)
	}
}
