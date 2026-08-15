package ipc

import (
	"context"

	mutationapp "github.com/maemreyo/shellbeam/internal/app/mutationscope"
	core "github.com/maemreyo/shellbeam/internal/core/mutationscope"
)

type MutationScopeActions interface {
	SetMutationScope(context.Context, mutationapp.SetRequest) (mutationapp.MutationResult, error)
	ReleaseMutationScope(context.Context, mutationapp.ReleaseRequest) (mutationapp.MutationResult, error)
	InspectMutationScopes(context.Context, mutationapp.InspectRequest) (core.InspectResult, error)
}

type ActivityMutationScopeActions interface {
	InspectActivityMutationScopes(context.Context, string) (core.InspectResult, error)
}
