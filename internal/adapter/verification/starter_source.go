package verification

import (
	"context"

	app "github.com/maemreyo/shellbeam/internal/app/verification"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

type StarterSource struct{}

func NewStarterSource() *StarterSource { return &StarterSource{} }

func (*StarterSource) Preview(ctx context.Context, profile, repositoryID string, manifest *project.Manifest) (app.PolicyPreview, error) {
	if err := ctx.Err(); err != nil {
		return app.PolicyPreview{}, err
	}
	proposal, advisories, err := PreviewStarter(profile, repositoryID, manifest)
	if err != nil {
		return app.PolicyPreview{}, err
	}
	rendered, err := RenderPolicyTOML(proposal)
	if err != nil {
		return app.PolicyPreview{}, err
	}
	copyProposal := proposal
	return app.PolicyPreview{State: app.PolicyLoadValid, Proposal: &copyProposal, RenderedTOML: string(rendered), Advisories: advisories}, nil
}
