package localfs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"golang.org/x/sys/unix"
)

const maxPrivateRestoreRecordBytes = 4 << 20

type restoreLayout struct {
	capture    *privateLayout
	restoresFD int
	restoreFD  int
	pathsFD    int
}

func (l *restoreLayout) close() {
	for _, fd := range []int{l.pathsFD, l.restoreFD, l.restoresFD} {
		if fd >= 0 {
			_ = unix.Close(fd)
		}
	}
	if l.capture != nil {
		l.capture.close()
	}
}

type privateRestoreClaim struct {
	SchemaVersion      int      `json:"schema_version"`
	RestoreID          string   `json:"restore_id"`
	CheckpointID       string   `json:"checkpoint_id"`
	WorkspaceID        string   `json:"workspace_id"`
	Root               string   `json:"root"`
	Paths              []string `json:"paths"`
	RequestFingerprint string   `json:"request_fingerprint"`
}

type privateRestoreObservations struct {
	SchemaVersion int                  `json:"schema_version"`
	Paths         []privateObservation `json:"paths"`
}

type privateRestorePathRecord struct {
	SchemaVersion int                    `json:"schema_version"`
	Result        core.RestorePathResult `json:"result"`
}

func (p *Provider) ensureRestoreLayout(checkpointID, restoreID string) (*restoreLayout, error) {
	if !safeComponent(restoreID) {
		return nil, fmt.Errorf("invalid restore id")
	}
	capture, err := p.openPrivateLayout(checkpointID)
	if err != nil {
		return nil, err
	}
	layout := &restoreLayout{capture: capture, restoresFD: -1, restoreFD: -1, pathsFD: -1}
	fail := func(err error) (*restoreLayout, error) {
		layout.close()
		return nil, err
	}
	if layout.restoresFD, err = ensurePrivateDirAt(capture.checkpointFD, "restores"); err != nil {
		return fail(err)
	}
	if layout.restoreFD, err = ensurePrivateDirAt(layout.restoresFD, restoreID); err != nil {
		return fail(err)
	}
	if layout.pathsFD, err = ensurePrivateDirAt(layout.restoreFD, "paths"); err != nil {
		return fail(err)
	}
	return layout, nil
}

func restoreRequestFingerprint(request checkpointapp.ProviderRestoreRequest) (string, error) {
	payload := struct {
		RestoreID    string   `json:"restore_id"`
		CheckpointID string   `json:"checkpoint_id"`
		WorkspaceID  string   `json:"workspace_id"`
		Root         string   `json:"root"`
		Paths        []string `json:"paths"`
	}{
		RestoreID: request.RestoreID, CheckpointID: request.CheckpointID,
		WorkspaceID: request.WorkspaceID, Root: filepath.Clean(request.Root),
		Paths: append([]string(nil), request.Paths...),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (p *Provider) loadOrCreateRestoreClaim(
	layout *restoreLayout,
	request checkpointapp.ProviderRestoreRequest,
) (privateRestoreClaim, error) {
	fingerprint, err := restoreRequestFingerprint(request)
	if err != nil {
		return privateRestoreClaim{}, err
	}
	raw, err := privateReadAt(layout.restoreFD, "claim.json", maxPrivateRestoreRecordBytes)
	if err == nil {
		var claim privateRestoreClaim
		if err := strictJSON(raw, &claim); err != nil {
			return privateRestoreClaim{}, err
		}
		if err := validateRestoreClaim(claim); err != nil {
			return privateRestoreClaim{}, err
		}
		if claim.RequestFingerprint != fingerprint {
			return privateRestoreClaim{}, restoreRequestConflict(request.RestoreID)
		}
		return claim, nil
	}
	if !errors.Is(err, unix.ENOENT) {
		return privateRestoreClaim{}, err
	}
	claim := privateRestoreClaim{
		SchemaVersion: providerSchemaVersion, RestoreID: request.RestoreID,
		CheckpointID: request.CheckpointID, WorkspaceID: request.WorkspaceID,
		Root: filepath.Clean(request.Root), Paths: append([]string(nil), request.Paths...),
		RequestFingerprint: fingerprint,
	}
	raw, err = marshalPrivate(claim)
	if err != nil {
		return privateRestoreClaim{}, err
	}
	if err := privateWriteNewAt(layout.restoreFD, "claim.json", raw); err != nil {
		return privateRestoreClaim{}, err
	}
	return claim, nil
}

func validateRestoreClaim(claim privateRestoreClaim) error {
	if claim.SchemaVersion != providerSchemaVersion ||
		!safeComponent(claim.RestoreID) ||
		!safeComponent(claim.CheckpointID) ||
		claim.WorkspaceID == "" ||
		!filepath.IsAbs(claim.Root) ||
		len(claim.RequestFingerprint) != 64 {
		return fmt.Errorf("invalid private restore claim")
	}
	normalized, err := (core.RestoreRequest{
		RestoreID: claim.RestoreID, CheckpointID: claim.CheckpointID, Paths: claim.Paths,
	}).Normalize()
	if err != nil || !reflect.DeepEqual(normalized.Paths, claim.Paths) {
		return fmt.Errorf("invalid private restore claim paths")
	}
	return nil
}

func (p *Provider) loadOrCreateRestoreObservations(
	layout *restoreLayout,
	rootFD int,
	paths []string,
) ([]privateObservation, error) {
	raw, err := privateReadAt(layout.restoreFD, "observations.json", maxPrivateRestoreRecordBytes)
	if err == nil {
		var record privateRestoreObservations
		if err := strictJSON(raw, &record); err != nil {
			return nil, err
		}
		if err := validateRestoreObservations(record, paths); err != nil {
			return nil, err
		}
		return record.Paths, nil
	}
	if !errors.Is(err, unix.ENOENT) {
		return nil, err
	}
	observations := make([]privateObservation, 0, len(paths))
	for _, path := range paths {
		observation, err := observePath(rootFD, path)
		if err != nil {
			return nil, err
		}
		observations = append(observations, observation)
	}
	record := privateRestoreObservations{SchemaVersion: providerSchemaVersion, Paths: observations}
	raw, err = marshalPrivate(record)
	if err != nil {
		return nil, err
	}
	if err := privateWriteNewAt(layout.restoreFD, "observations.json", raw); err != nil {
		return nil, err
	}
	return observations, nil
}

func validateRestoreObservations(record privateRestoreObservations, paths []string) error {
	if record.SchemaVersion != providerSchemaVersion || len(record.Paths) != len(paths) {
		return fmt.Errorf("invalid private restore observations")
	}
	for i, observation := range record.Paths {
		if observation.Path != paths[i] {
			return fmt.Errorf("private restore observation path mismatch")
		}
		if err := validateObservation(observation); err != nil {
			return err
		}
	}
	return nil
}

func loadRestorePathResult(layout *restoreLayout, ordinal int) (core.RestorePathResult, bool, error) {
	name := fmt.Sprintf("%06d.json", ordinal)
	raw, err := privateReadAt(layout.pathsFD, name, 16<<10)
	if errors.Is(err, unix.ENOENT) {
		return core.RestorePathResult{}, false, nil
	}
	if err != nil {
		return core.RestorePathResult{}, false, err
	}
	var record privateRestorePathRecord
	if err := strictJSON(raw, &record); err != nil {
		return core.RestorePathResult{}, false, err
	}
	if record.SchemaVersion != providerSchemaVersion {
		return core.RestorePathResult{}, false, fmt.Errorf("invalid private restore path record")
	}
	if err := record.Result.Validate(); err != nil {
		return core.RestorePathResult{}, false, err
	}
	return record.Result, true, nil
}

func storeRestorePathResult(
	layout *restoreLayout,
	ordinal int,
	result core.RestorePathResult,
) error {
	if err := result.Validate(); err != nil {
		return err
	}
	record := privateRestorePathRecord{SchemaVersion: providerSchemaVersion, Result: result}
	raw, err := marshalPrivate(record)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%06d.json", ordinal)
	if err := privateWriteNewAt(layout.pathsFD, name, raw); err != nil {
		if !errors.Is(err, unix.EEXIST) {
			return err
		}
		current, found, readErr := loadRestorePathResult(layout, ordinal)
		if readErr != nil {
			return readErr
		}
		if !found || !reflect.DeepEqual(current, result) {
			return fmt.Errorf("conflicting private restore path record")
		}
	}
	return nil
}

func restoreRequestConflict(restoreID string) error {
	return failure.New(
		failure.CheckpointRestoreRequestConflict,
		map[string]string{"restore_id": restoreID},
		nil,
	)
}
