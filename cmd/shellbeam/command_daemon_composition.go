package main

import (
	"context"
	"time"

	environmentadapter "github.com/maemreyo/shellbeam/internal/adapter/environment"
	localfsadapter "github.com/maemreyo/shellbeam/internal/adapter/localfs"
	projectadapter "github.com/maemreyo/shellbeam/internal/adapter/project"
	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	verificationadapter "github.com/maemreyo/shellbeam/internal/adapter/verification"
	activityapp "github.com/maemreyo/shellbeam/internal/app/activity"
	daemonapp "github.com/maemreyo/shellbeam/internal/app/daemon"
	environmentapp "github.com/maemreyo/shellbeam/internal/app/environment"
	"github.com/maemreyo/shellbeam/internal/app/outputview"
	projectapp "github.com/maemreyo/shellbeam/internal/app/project"
	workspaceapp "github.com/maemreyo/shellbeam/internal/app/workspace"
	"github.com/maemreyo/shellbeam/internal/buildinfo"
	"github.com/maemreyo/shellbeam/internal/config"
	activitycore "github.com/maemreyo/shellbeam/internal/core/activity"
	"github.com/maemreyo/shellbeam/internal/core/capability"
	environmentcore "github.com/maemreyo/shellbeam/internal/core/environment"
)

func withDaemonRuntimeIdentity(catalog capability.Catalog, incarnation string, startedAt time.Time, process buildinfo.ProcessIdentity) capability.Catalog {
	return catalog.WithRuntime(capability.RuntimeIdentity{
		SchemaVersion: capability.RuntimeIdentitySchemaVersion,
		Version:       process.Version, Revision: process.Revision, VCSModified: process.VCSModified, BinarySHA256: process.BinarySHA256,
		DaemonIncarnation: incarnation, DaemonStartedAt: startedAt.UTC().Format(time.RFC3339Nano),
	})
}

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

func composeDaemonProjectVerificationRuntime(
	store *storeadapter.Repository,
	cfg config.Config,
	workspaceSvc *workspaceapp.Service,
	workspaceObserver *workspaceapp.Observer,
	deltaSampler *workspaceapp.DeltaSampler,
	activitySvc *activityapp.Service,
) (*projectapp.Binder, *projectapp.Service, *environmentapp.Service, *daemonVerificationRuntime) {
	projectLoader := projectadapter.NewLoader()
	projectBinder := projectapp.NewBinder(store, projectLoader, projectadapter.NewRepoPathValidator(), projectadapter.NewGoPackageValidator())
	hostReadiness := projectadapter.NewHostReadiness()
	projectSvc := projectapp.NewWithReadiness(
		store, projectLoader, store,
		projectapp.ReadinessObservers{Executable: hostReadiness, Environment: hostReadiness, Toolchain: hostReadiness},
		projectapp.ReadinessOptions{},
	)
	environmentSvc := environmentapp.NewService(
		environmentadapter.NewHost(), projectEnvironmentManifestProvider{project: projectSvc}, environmentadapter.NewHostProber(),
		environmentapp.Options{DefaultExecution: environmentcore.ExecutionContext{Mode: "shell", Identity: cfg.Shell}},
	)
	verificationRuntime := composeVerificationRuntime(
		store, workspaceSvc, workspaceObserver, deltaSampler, activitySvc, projectSvc, projectBinder,
		verificationadapter.NewEnvironmentSource(environmentSvc),
	)
	return projectBinder, projectSvc, environmentSvc, verificationRuntime
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
