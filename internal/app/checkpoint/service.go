package checkpoint

import (
	"context"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/oklog/ulid/v2"
)

type Service struct {
	repository      Repository
	workspace       WorkspaceSource
	provider        Provider
	now             func() time.Time
	newCheckpointID func() string
}

func New(repository Repository, workspace WorkspaceSource, provider Provider) *Service {
	return &Service{
		repository:      repository,
		workspace:       workspace,
		provider:        provider,
		now:             func() time.Time { return time.Now().UTC() },
		newCheckpointID: func() string { return "chk_" + ulid.Make().String() },
	}
}

func (s *Service) Inspect(ctx context.Context, checkpointID string) (core.Checkpoint, error) {
	if s == nil || s.repository == nil {
		return core.Checkpoint{}, checkpointProviderUnavailable("repository_unavailable", "")
	}
	return s.repository.LoadCheckpoint(ctx, checkpointID)
}
