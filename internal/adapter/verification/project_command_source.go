package verification

import (
	"context"

	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

type projectCommandBinder interface {
	Bind(context.Context, projectapp.BindRequest) (project.CommandBinding, error)
}

type ProjectCommandSource struct{ binder projectCommandBinder }

func NewProjectCommandSource(binder projectCommandBinder) *ProjectCommandSource {
	return &ProjectCommandSource{binder: binder}
}
func (s *ProjectCommandSource) Resolve(ctx context.Context, workspaceID, commandID string, params map[string]string) (project.CommandBinding, error) {
	copyParams := make(map[string]string, len(params))
	for k, v := range params {
		copyParams[k] = v
	}
	return s.binder.Bind(ctx, projectapp.BindRequest{WorkspaceID: workspaceID, CommandID: commandID, Params: copyParams})
}
