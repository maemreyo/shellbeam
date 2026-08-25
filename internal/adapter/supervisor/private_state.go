//go:build linux || darwin

package supervisor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"golang.org/x/sys/unix"
)

const maxPrivateMetadataBytes = 4096

type Layout struct {
	RuntimeRoot    string
	SupervisorsDir string
	SessionDir     string
	CapabilityPath string
	MetadataPath   string
	SocketPath     string
	TerminalPath   string
}

type Metadata struct {
	SchemaVersion   int    `json:"schema_version"`
	ProtocolVersion int    `json:"protocol_version"`
	SessionID       string `json:"session_id"`
	GenerationID    string `json:"generation_id"`
}

func layoutFor(runtimeRoot, sessionID string) Layout {
	supervisors := filepath.Join(runtimeRoot, "supervisors")
	sessionDir := filepath.Join(supervisors, sessionID)
	return Layout{
		RuntimeRoot: runtimeRoot, SupervisorsDir: supervisors, SessionDir: sessionDir,
		CapabilityPath: filepath.Join(sessionDir, "capability.bin"), MetadataPath: filepath.Join(sessionDir, "metadata.json"),
		SocketPath: filepath.Join(sessionDir, "control.sock"), TerminalPath: filepath.Join(sessionDir, "terminal.json"),
	}
}

func OpenPrivateState(runtimeRoot, sessionID, generationID string) (Layout, error) {
	if !filepath.IsAbs(runtimeRoot) {
		return Layout{}, privateStateFailure("runtime_root")
	}
	if _, err := operation.ParseSessionID(sessionID); err != nil || !validOpaque(generationID) {
		return Layout{}, privateStateFailure("identity")
	}
	layout := layoutFor(runtimeRoot, sessionID)
	if err := validateLayout(layout); err != nil {
		return Layout{}, err
	}
	metadata, err := LoadMetadata(layout)
	if err != nil || metadata.SessionID != sessionID || metadata.GenerationID != generationID {
		return Layout{}, privateStateFailure("identity")
	}
	return layout, nil
}

func PreparePrivateState(runtimeRoot, sessionID, generationID string, capability Capability) (Layout, error) {
	if !filepath.IsAbs(runtimeRoot) {
		return Layout{}, privateStateFailure("runtime_root")
	}
	if _, err := operation.ParseSessionID(sessionID); err != nil || !validOpaque(generationID) {
		return Layout{}, privateStateFailure("identity")
	}
	layout := layoutFor(runtimeRoot, sessionID)
	if err := ensurePrivateDirectory(runtimeRoot); err != nil {
		return Layout{}, privateStateFailure("runtime_root")
	}
	supervisors := filepath.Join(runtimeRoot, "supervisors")
	if err := ensurePrivateDirectory(supervisors); err != nil {
		return Layout{}, privateStateFailure("supervisors_dir")
	}
	sessionDir := filepath.Join(supervisors, sessionID)
	if err := ensurePrivateDirectory(sessionDir); err != nil {
		return Layout{}, privateStateFailure("session_dir")
	}
	if err := createOrMatchPrivateFile(layout.CapabilityPath, capability.bytes()); err != nil {
		return Layout{}, privateStateFailure("capability_file")
	}
	metadata := Metadata{SchemaVersion: 1, ProtocolVersion: ProtocolVersion, SessionID: sessionID, GenerationID: generationID}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return Layout{}, privateStateFailure("metadata")
	}
	if err := createOrMatchPrivateFile(layout.MetadataPath, append(encoded, '\n')); err != nil {
		return Layout{}, privateStateFailure("metadata_file")
	}
	return layout, nil
}

func LoadCapability(layout Layout) (Capability, error) {
	raw, err := readPrivateFile(layout.CapabilityPath, CapabilityBytes, CapabilityBytes)
	if err != nil {
		return Capability{}, privateStateFailure("capability_file")
	}
	capability, err := capabilityFromBytes(raw)
	if err != nil {
		return Capability{}, privateStateFailure("capability_file")
	}
	return capability, nil
}

func LoadMetadata(layout Layout) (Metadata, error) {
	raw, err := readPrivateFile(layout.MetadataPath, 2, maxPrivateMetadataBytes)
	if err != nil {
		return Metadata{}, privateStateFailure("metadata_file")
	}
	var metadata Metadata
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&metadata); err != nil {
		return Metadata{}, privateStateFailure("metadata_file")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || metadata.SchemaVersion != 1 || metadata.ProtocolVersion != ProtocolVersion {
		return Metadata{}, privateStateFailure("metadata_file")
	}
	if _, err := operation.ParseSessionID(metadata.SessionID); err != nil || !validOpaque(metadata.GenerationID) {
		return Metadata{}, privateStateFailure("metadata_file")
	}
	return metadata, nil
}

func validateLayout(layout Layout) error {
	if !filepath.IsAbs(layout.RuntimeRoot) || filepath.Dir(layout.SupervisorsDir) != layout.RuntimeRoot || filepath.Dir(layout.SessionDir) != layout.SupervisorsDir {
		return privateStateFailure("layout")
	}
	for _, path := range []string{layout.RuntimeRoot, layout.SupervisorsDir, layout.SessionDir} {
		if err := validatePrivateDirectory(path); err != nil {
			return privateStateFailure("layout")
		}
	}
	metadata, err := LoadMetadata(layout)
	if err != nil {
		return err
	}
	if filepath.Base(layout.SessionDir) != metadata.SessionID || layout.CapabilityPath != filepath.Join(layout.SessionDir, "capability.bin") || layout.MetadataPath != filepath.Join(layout.SessionDir, "metadata.json") || layout.SocketPath != filepath.Join(layout.SessionDir, "control.sock") || layout.TerminalPath != filepath.Join(layout.SessionDir, "terminal.json") {
		return privateStateFailure("layout")
	}
	return nil
}

func validateControlSocketPath(path string) error {
	if !filepath.IsAbs(path) || len([]byte(path)) >= len(unix.RawSockaddrUnix{}.Path) {
		return privateStateFailure("socket_path")
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrExist) {
		return err
	}
	return validatePrivateDirectory(path)
}

func validatePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0700 || !ownedByCurrent(info) {
		return fmt.Errorf("unsafe private directory")
	}
	return nil
}

func createOrMatchPrivateFile(path string, data []byte) error {
	fd, err := unix.Open(path, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err == nil {
		file := os.NewFile(uintptr(fd), path)
		if file == nil {
			_ = unix.Close(fd)
			return fmt.Errorf("create private file")
		}
		writeErr := writeAndSyncPrivate(file, data)
		if writeErr != nil {
			_ = os.Remove(path)
		}
		return writeErr
	}
	if err != unix.EEXIST {
		return err
	}
	existing, readErr := readPrivateFile(path, len(data), len(data))
	if readErr != nil || !bytes.Equal(existing, data) {
		return fmt.Errorf("private file conflict")
	}
	return nil
}

func writeAndSyncPrivate(file *os.File, data []byte) error {
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(file.Name()))
}

func readPrivateFile(path string, minBytes, maxBytes int) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open private file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ownedByCurrent(info) || info.Size() < int64(minBytes) || info.Size() > int64(maxBytes) {
		return nil, fmt.Errorf("unsafe private file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil || len(raw) > maxBytes {
		return nil, fmt.Errorf("read private file")
	}
	return raw, nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func privateStateFailure(reason string) error {
	return failure.New(failure.SupervisorStateConflict, map[string]string{"reason": reason}, fmt.Errorf("invalid supervisor private state"))
}
