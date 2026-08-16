//go:build linux || darwin

package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/maemreyo/shellbeam/internal/core/failure"
	"golang.org/x/sys/unix"
)

type OutputRange struct {
	Start int64
	End   int64
}

type outputAckState struct {
	SchemaVersion int   `json:"schema_version"`
	Offset        int64 `json:"offset"`
}

type Spool struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	ackPath string
	max     int64
	size    int64
	ack     int64
}

func OpenSpool(layout Layout, maxBytes int64) (*Spool, error) {
	if err := validateLayout(layout); err != nil {
		return nil, err
	}
	if maxBytes < 1 {
		return nil, outputFailure("invalid_limit")
	}
	path := filepath.Join(layout.SessionDir, "output.spool")
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_APPEND|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, outputFailure("open")
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, outputFailure("open")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ownedByCurrent(info) || info.Size() > maxBytes {
		_ = file.Close()
		return nil, outputFailure("unsafe_spool")
	}
	ackPath := filepath.Join(layout.SessionDir, "output-ack.json")
	ack, err := loadOutputAck(ackPath, info.Size())
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &Spool{file: file, path: path, ackPath: ackPath, max: maxBytes, size: info.Size(), ack: ack}, nil
}

func (s *Spool) Append(_ context.Context, data []byte) error {
	_, err := s.AppendRange(data)
	return err
}

func (s *Spool) AppendRange(data []byte) (OutputRange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil || len(data) == 0 {
		return OutputRange{}, outputFailure("invalid_append")
	}
	if s.size+int64(len(data)) > s.max {
		return OutputRange{Start: s.size, End: s.size}, outputFailure("output_limit")
	}
	start := s.size
	written := 0
	for written < len(data) {
		n, err := s.file.Write(data[written:])
		if err != nil {
			return OutputRange{Start: start, End: start + int64(written)}, outputFailure("write")
		}
		if n == 0 {
			return OutputRange{Start: start, End: start + int64(written)}, outputFailure("short_write")
		}
		written += n
	}
	if err := s.file.Sync(); err != nil {
		return OutputRange{Start: start, End: start + int64(written)}, outputFailure("sync")
	}
	s.size += int64(written)
	return OutputRange{Start: start, End: s.size}, nil
}

func (s *Spool) ReadRange(offset int64, maxBytes int) ([]byte, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil || offset < 0 || offset > s.size || maxBytes < 0 || maxBytes > MaxOutputBytes {
		return nil, s.size, outputFailure("range")
	}
	remaining := s.size - offset
	if int64(maxBytes) > remaining {
		maxBytes = int(remaining)
	}
	if maxBytes == 0 {
		return []byte{}, s.size, nil
	}
	data := make([]byte, maxBytes)
	n, err := s.file.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, s.size, outputFailure("read")
	}
	return data[:n], s.size, nil
}

func (s *Spool) Size() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.size
}

func (s *Spool) Acknowledged() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ack
}

func (s *Spool) Acknowledge(offset int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil || offset < s.ack || offset > s.size {
		return outputFailure("ack_range")
	}
	if offset == s.ack {
		return nil
	}
	state := outputAckState{SchemaVersion: 1, Offset: offset}
	if err := replacePrivateJSON(s.ackPath, state, 1024); err != nil {
		return outputFailure("ack_write")
	}
	s.ack = offset
	return nil
}

func loadOutputAck(path string, extent int64) (int64, error) {
	state := outputAckState{SchemaVersion: 1}
	err := loadPrivateJSON(path, 1024, &state)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, io.EOF) {
		if err := createPrivateJSON(path, state, 1024); err != nil {
			return 0, outputFailure("ack_create")
		}
		return 0, nil
	}
	if err != nil || state.SchemaVersion != 1 || state.Offset < 0 || state.Offset > extent {
		return 0, outputFailure("ack_state")
	}
	return state.Offset, nil
}

func (s *Spool) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

func outputFailure(reason string) error {
	return failure.New(failure.PersistentRecoveryOutputConflict, map[string]string{"reason": reason}, fmt.Errorf("persistent recovery output unavailable"))
}
