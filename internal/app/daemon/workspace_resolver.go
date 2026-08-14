package daemon

import (
	"context"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

type WorkspaceResolver interface {
	ResolveAddress(context.Context, workspace.Address) (workspace.ResolvedAddress, error)
}

func NewServiceWithWorkspaceResolver(store Store, owner ProcessOwner, resolver WorkspaceResolver, options Options) *Service {
	service := NewService(store, owner, options)
	service.resolver = resolver
	return service
}
