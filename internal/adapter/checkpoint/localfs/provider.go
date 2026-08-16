package localfs

import (
	"context"
	"errors"
	"path/filepath"
	"sync"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/oklog/ulid/v2"
)

const providerSchemaVersion = 1

type Provider struct {
	stateDir              string
	runtimeDir            string
	mu                    sync.Mutex
	newEntryRef           func() string
	afterEntry            func(int) error
	beforeRestoreMutation func(string)
	afterRestorePath      func(int) error
}

func New(stateDir, runtimeDir string) *Provider {
	return &Provider{
		stateDir:    filepath.Clean(stateDir),
		runtimeDir:  filepath.Clean(runtimeDir),
		newEntryRef: func() string { return "entry_" + ulid.Make().String() },
	}
}

func (p *Provider) Identity() core.ProviderIdentity {
	return core.ProviderIdentity{ID: "localfs", Version: 1}
}

func (p *Provider) ConflictDetection() core.ConflictDetection {
	return core.ConflictDetection{
		RegularFile:   core.ConflictBestEffort,
		Symlink:       core.ConflictBestEffort,
		AbsentToFile:  core.ConflictBestEffort,
		DirectoryTree: core.ConflictUnsupported,
	}
}

func (p *Provider) Capture(ctx context.Context, request checkpointapp.CaptureRequest) (checkpointapp.CaptureResult, error) {
	return p.capture(ctx, request)
}

func (p *Provider) contentRoot() string     { return filepath.Join(p.stateDir, "checkpoint-content") }
func (p *Provider) versionRoot() string     { return filepath.Join(p.contentRoot(), "v1") }
func (p *Provider) checkpointsRoot() string { return filepath.Join(p.versionRoot(), "checkpoints") }
func (p *Provider) checkpointDir(checkpointID string) string {
	return filepath.Join(p.checkpointsRoot(), checkpointID)
}
func (p *Provider) entriesDir(checkpointID string) string {
	return filepath.Join(p.checkpointDir(checkpointID), "entries")
}
func (p *Provider) manifestPath(checkpointID string) string {
	return filepath.Join(p.checkpointDir(checkpointID), "manifest.json")
}
func (p *Provider) completePath(checkpointID string) string {
	return filepath.Join(p.checkpointDir(checkpointID), "complete")
}
func (p *Provider) entryDataPath(checkpointID, ref string) string {
	return filepath.Join(p.entriesDir(checkpointID), ref+".bin")
}

func (p *Provider) Restore(ctx context.Context, request checkpointapp.ProviderRestoreRequest) (checkpointapp.ProviderRestoreResult, error) {
	return p.restore(ctx, request)
}

func (p *Provider) Inspect(context.Context, string) (checkpointapp.ProviderCheckpointStatus, error) {
	return checkpointapp.ProviderCheckpointStatus{}, errors.New("checkpoint inspect not available")
}

func (p *Provider) Sweep(context.Context, checkpointapp.SweepRequest) (checkpointapp.SweepResult, error) {
	return checkpointapp.SweepResult{}, errors.New("checkpoint sweep not available")
}

var _ checkpointapp.Provider = (*Provider)(nil)
