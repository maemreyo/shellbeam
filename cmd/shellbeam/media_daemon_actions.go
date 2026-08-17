package main

import (
	"context"

	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/media"
)

func (a daemonActions) ReadMedia(ctx context.Context, req daemonapp.MediaRequest) (media.Result, error) {
	if a.observation == nil {
		return media.Result{}, failure.New(failure.FeatureUnavailable, map[string]string{"feature": "rich_local_media"}, nil)
	}
	return a.observation.ReadMedia(ctx, req)
}

func (a daemonActions) MediaSupport() capability.MediaSupport {
	return capability.V1MediaSupport()
}
