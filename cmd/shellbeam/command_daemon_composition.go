package main

import (
	"context"

	localfsadapter "github.com/maemreyo/shellbeam/internal/adapter/localfs"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	"github.com/maemreyo/shellbeam/internal/config"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
)

func daemonRuntimeCatalog(cfg config.Config, mutationScopesEnabled bool) capability.Catalog {
	catalog := daemonCatalog(capability.Limits{
		CommandBytes: cfg.MaxCommandBytes, ResponseBytes: cfg.MaxResponseOutputBytes,
		SessionOutputBytes: cfg.MaxSessionOutputBytes, RuntimeMS: cfg.MaxTimeoutMS,
		LiveSessions: cfg.MaxConcurrentSessions, ActivityHistory: activitycore.MaxOperationHistory,
	})
	catalog = persistentSessionCatalog(catalog, cfg.MaxConcurrentSessions, cfg.MaxSessionOutputBytes, cfg.MaxQueuedInputSessionBytes)
	if mutationScopesEnabled {
		catalog = mutationScopeCatalog(catalog)
	}
	return catalog
}

func bindDaemonOutputView(ctx context.Context, store *storeadapter.Repository, actions *daemonActions) error {
	outputKey, err := store.EventCursorKey(ctx)
	if err != nil {
		return err
	}
	outputCodec, err := outputview.NewCursorCodec(outputKey)
	if err != nil {
		return err
	}
	actions.output = outputview.NewWithCursor(store, outputCodec)
	return nil
}

func daemonMediaReader() daemonapp.MediaReader {
	return localfsadapter.Reader{}
}
