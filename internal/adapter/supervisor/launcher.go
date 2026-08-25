//go:build linux || darwin

package supervisor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	persistentapp "github.com/maemreyo/shellbeam/internal/app/persistentsession"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	core "github.com/maemreyo/shellbeam/internal/core/persistentsession"
	"golang.org/x/sys/unix"
)

const (
	launchMarkerSchemaVersion = 1
	maxLaunchMarkerBytes      = 4096
)

type LauncherOptions struct {
	RuntimeRoot      string
	Executable       string
	HandshakeTimeout time.Duration
}

type Launcher struct {
	lockMu          sync.Mutex
	locks           map[string]*launcherSessionLock
	options         LauncherOptions
	spawnSupervisor func(Bootstrap, Capability) error
	attach          func(context.Context, Layout, Capability, string, string) (persistentapp.Attachment, persistentapp.Status, error)
}

type launcherSessionLock struct {
	mu   sync.Mutex
	refs int
}

type launchMarker struct {
	SchemaVersion int    `json:"schema_version"`
	SessionID     string `json:"session_id"`
	GenerationID  string `json:"generation_id"`
	BootstrapHash string `json:"bootstrap_hash"`
}

func NewLauncher(options LauncherOptions) (*Launcher, error) {
	if !filepath.IsAbs(options.RuntimeRoot) || !filepath.IsAbs(options.Executable) {
		return nil, failure.New(failure.SupervisorStateConflict, map[string]string{"reason": "launcher_options"}, nil)
	}
	if options.HandshakeTimeout <= 0 {
		options.HandshakeTimeout = 2 * time.Second
	}
	launcher := &Launcher{options: options, locks: map[string]*launcherSessionLock{}}
	launcher.spawnSupervisor = launcher.spawn
	launcher.attach = func(ctx context.Context, layout Layout, capability Capability, sessionID, generationID string) (persistentapp.Attachment, persistentapp.Status, error) {
		client, status, err := DialAttachment(ctx, layout, capability, sessionID, generationID)
		if client == nil {
			return nil, status, err
		}
		return client, status, err
	}
	return launcher, nil
}

func (l *Launcher) Ensure(ctx context.Context, request persistentapp.LaunchRequest) (persistentapp.Attachment, persistentapp.Status, error) {
	if err := ctx.Err(); err != nil {
		return nil, persistentapp.Status{}, err
	}
	bootstrap, err := l.bootstrapFor(request)
	if err != nil {
		return nil, persistentapp.Status{}, err
	}

	release := l.acquireSession(bootstrap.SessionID)
	layout, capability, err := l.ensurePrivateState(bootstrap.SessionID, bootstrap.GenerationID)
	if err != nil {
		release()
		return nil, persistentapp.Status{}, err
	}
	marker, err := markerFor(bootstrap)
	if err != nil {
		release()
		return nil, persistentapp.Status{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": bootstrap.SessionID, "reason": "bootstrap_fingerprint"}, nil)
	}
	created, err := claimLaunchMarker(layout, marker)
	if err != nil {
		release()
		return nil, persistentapp.Status{}, err
	}
	if created {
		if err := l.spawnSupervisor(bootstrap, capability); err != nil {
			release()
			return nil, persistentapp.Status{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": bootstrap.SessionID, "reason": "launch"}, nil)
		}
	}
	release()
	return l.attachUntilReady(ctx, layout, capability, bootstrap.SessionID, bootstrap.GenerationID)
}

func (l *Launcher) acquireSession(sessionID string) func() {
	l.lockMu.Lock()
	entry := l.locks[sessionID]
	if entry == nil {
		entry = &launcherSessionLock{}
		l.locks[sessionID] = entry
	}
	entry.refs++
	l.lockMu.Unlock()
	entry.mu.Lock()
	return func() {
		entry.mu.Unlock()
		l.lockMu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.locks, sessionID)
		}
		l.lockMu.Unlock()
	}
}

func (l *Launcher) bootstrapFor(request persistentapp.LaunchRequest) (Bootstrap, error) {
	binding := request.Binding
	if err := binding.Validate(); err != nil || binding.Lifecycle == core.LifecycleTerminal || binding.Lifecycle == core.LifecycleLost {
		return Bootstrap{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "binding"}, err)
	}
	if err := request.Limits.Validate(); err != nil {
		return Bootstrap{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "limits"}, nil)
	}
	bootstrap := Bootstrap{
		SchemaVersion: BootstrapSchemaVersion,
		RuntimeRoot:   l.options.RuntimeRoot,
		SessionID:     binding.SessionID,
		GenerationID:  binding.SupervisorGenerationID,
		Execution: BootstrapExecution{
			Mode: request.Spec.Mode, Shell: request.Spec.Shell, Executable: request.Spec.Executable,
			Command: request.Spec.Command, Argv: append([]string(nil), request.Spec.Argv...), CWD: request.Spec.CWD,
			TTY: request.Spec.TTY, TimeoutMS: request.Spec.TimeoutMS,
		},
		MaxOutputBytes:        request.Limits.MaxOutputBytes,
		MaxQueuedInputBytes:   request.Limits.MaxQueuedInputBytes,
		MaxInputRecords:       request.Limits.MaxInputRecords,
		MaxInputMetadataBytes: request.Limits.MaxInputMetadataBytes,
		MaxKillRecords:        request.Limits.MaxKillRecords,
		TerminationGraceMS:    request.Limits.TerminationGrace.Milliseconds(),
	}
	if err := bootstrap.Validate(); err != nil {
		return Bootstrap{}, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": binding.SessionID, "reason": "execution_binding"}, nil)
	}
	return bootstrap, nil
}

func (l *Launcher) ensurePrivateState(sessionID, generationID string) (Layout, Capability, error) {
	layout := layoutFor(l.options.RuntimeRoot, sessionID)
	if err := validateControlSocketPath(layout.SocketPath); err != nil {
		return Layout{}, Capability{}, err
	}
	if _, err := os.Lstat(layout.SessionDir); err == nil {
		opened, openErr := OpenPrivateState(l.options.RuntimeRoot, sessionID, generationID)
		if openErr != nil {
			return Layout{}, Capability{}, openErr
		}
		capability, loadErr := LoadCapability(opened)
		return opened, capability, loadErr
	} else if !errors.Is(err, os.ErrNotExist) {
		return Layout{}, Capability{}, privateStateFailure("session_dir")
	}

	capability, err := NewCapability()
	if err != nil {
		return Layout{}, Capability{}, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": sessionID, "reason": "capability"}, nil)
	}
	prepared, err := PreparePrivateState(l.options.RuntimeRoot, sessionID, generationID, capability)
	if err == nil {
		return prepared, capability, nil
	}
	// A concurrent launcher may have won private-state initialization. Reopen
	// only the exact same session/generation; corruption or mismatch remains a
	// fail-closed error.
	opened, openErr := OpenPrivateState(l.options.RuntimeRoot, sessionID, generationID)
	if openErr != nil {
		return Layout{}, Capability{}, err
	}
	loaded, loadErr := LoadCapability(opened)
	if loadErr != nil {
		return Layout{}, Capability{}, loadErr
	}
	return opened, loaded, nil
}

func (l *Launcher) attachUntilReady(ctx context.Context, layout Layout, capability Capability, sessionID, generationID string) (persistentapp.Attachment, persistentapp.Status, error) {
	deadline := time.Now().Add(l.options.HandshakeTimeout)
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return nil, persistentapp.Status{}, err
		}
		attemptCtx, cancel := context.WithDeadline(ctx, deadline)
		attachment, status, err := l.attach(attemptCtx, layout, capability, sessionID, generationID)
		cancel()
		if err == nil {
			return attachment, status, nil
		}
		if attachment != nil {
			_ = attachment.Close()
		}
		last = err
		if !errors.Is(err, failure.SupervisorUnavailable) {
			return nil, persistentapp.Status{}, err
		}
		if !time.Now().Before(deadline) {
			return nil, persistentapp.Status{}, last
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, persistentapp.Status{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (l *Launcher) spawn(bootstrap Bootstrap, capability Capability) error {
	bootstrapRead, bootstrapWrite, err := os.Pipe()
	if err != nil {
		return err
	}
	capabilityRead, capabilityWrite, err := os.Pipe()
	if err != nil {
		_ = bootstrapRead.Close()
		_ = bootstrapWrite.Close()
		return err
	}
	cmd := exec.Command(l.options.Executable, "__supervisor")
	cmd.ExtraFiles = []*os.File{bootstrapRead, capabilityRead}
	if err := cmd.Start(); err != nil {
		_ = bootstrapRead.Close()
		_ = bootstrapWrite.Close()
		_ = capabilityRead.Close()
		_ = capabilityWrite.Close()
		return err
	}
	_ = bootstrapRead.Close()
	_ = capabilityRead.Close()
	bootstrapErr := EncodeBootstrap(bootstrapWrite, bootstrap)
	bootstrapCloseErr := bootstrapWrite.Close()
	capabilityErr := EncodeCapability(capabilityWrite, capability)
	capabilityCloseErr := capabilityWrite.Close()
	go func() { _ = cmd.Wait() }()
	for _, candidate := range []error{bootstrapErr, bootstrapCloseErr, capabilityErr, capabilityCloseErr} {
		if candidate != nil {
			return candidate
		}
	}
	return nil
}

func markerFor(bootstrap Bootstrap) (launchMarker, error) {
	var encoded bytes.Buffer
	if err := EncodeBootstrap(&encoded, bootstrap); err != nil {
		return launchMarker{}, err
	}
	digest := sha256.Sum256(encoded.Bytes())
	return launchMarker{
		SchemaVersion: launchMarkerSchemaVersion,
		SessionID:     bootstrap.SessionID, GenerationID: bootstrap.GenerationID,
		BootstrapHash: hex.EncodeToString(digest[:]),
	}, nil
}

func claimLaunchMarker(layout Layout, expected launchMarker) (bool, error) {
	if err := validateLaunchMarker(expected); err != nil {
		return false, err
	}
	path := filepath.Join(layout.SessionDir, "launch.json")
	encoded, err := json.Marshal(expected)
	if err != nil {
		return false, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": expected.SessionID, "reason": "launch_marker"}, nil)
	}
	encoded = append(encoded, '\n')
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err == nil {
		file := os.NewFile(uintptr(fd), path)
		if file == nil {
			_ = unix.Close(fd)
			return false, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": expected.SessionID, "reason": "launch_marker"}, nil)
		}
		if err := writeAndSyncPrivate(file, encoded); err != nil {
			_ = os.Remove(path)
			return false, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": expected.SessionID, "reason": "launch_marker"}, nil)
		}
		return true, nil
	}
	if err != unix.EEXIST {
		return false, failure.New(failure.SupervisorUnavailable, map[string]string{"session_id": expected.SessionID, "reason": "launch_marker"}, nil)
	}
	stored, loadErr := loadLaunchMarker(layout)
	if loadErr != nil {
		return false, loadErr
	}
	if stored != expected {
		return false, failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": expected.SessionID, "reason": "launch_marker_conflict"}, nil)
	}
	return false, nil
}

func loadLaunchMarker(layout Layout) (launchMarker, error) {
	path := filepath.Join(layout.SessionDir, "launch.json")
	raw, err := readPrivateFile(path, 2, maxLaunchMarkerBytes)
	if err != nil {
		return launchMarker{}, failure.New(failure.SupervisorStateConflict, map[string]string{"reason": "launch_marker"}, nil)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var marker launchMarker
	if err := decoder.Decode(&marker); err != nil {
		return launchMarker{}, failure.New(failure.SupervisorStateConflict, map[string]string{"reason": "launch_marker"}, nil)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return launchMarker{}, failure.New(failure.SupervisorStateConflict, map[string]string{"reason": "launch_marker"}, nil)
	}
	if err := validateLaunchMarker(marker); err != nil {
		return launchMarker{}, err
	}
	return marker, nil
}

func validateLaunchMarker(marker launchMarker) error {
	if marker.SchemaVersion != launchMarkerSchemaVersion || !validOpaque(marker.SessionID) || !validOpaque(marker.GenerationID) || len(marker.BootstrapHash) != sha256.Size*2 {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": marker.SessionID, "reason": "launch_marker"}, nil)
	}
	if _, err := hex.DecodeString(marker.BootstrapHash); err != nil {
		return failure.New(failure.SupervisorStateConflict, map[string]string{"session_id": marker.SessionID, "reason": "launch_marker"}, nil)
	}
	return nil
}

var _ persistentapp.Launcher = (*Launcher)(nil)
