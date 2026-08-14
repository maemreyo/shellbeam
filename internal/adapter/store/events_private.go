package store

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/maemreyo/shellbeam/internal/core/observation"
)

func newEventCursorKeyRecord() (eventCursorKeyRecord, error) {
	secret := make([]byte, 32)
	epoch := make([]byte, 16)
	generation := make([]byte, 16)
	if _, err := rand.Read(secret); err != nil {
		return eventCursorKeyRecord{}, err
	}
	if _, err := rand.Read(epoch); err != nil {
		return eventCursorKeyRecord{}, err
	}
	if _, err := rand.Read(generation); err != nil {
		return eventCursorKeyRecord{}, err
	}
	return eventCursorKeyRecord{
		SchemaVersion:  observation.SchemaVersion,
		StateRootEpoch: "epoch_" + hex.EncodeToString(epoch),
		Generation:     "key_" + hex.EncodeToString(generation),
		Secret:         base64.RawURLEncoding.EncodeToString(secret),
	}, nil
}

func cursorKeyMaterial(record eventCursorKeyRecord) (observation.CursorKeyMaterial, error) {
	if record.SchemaVersion != observation.SchemaVersion {
		return observation.CursorKeyMaterial{}, fmt.Errorf("invalid cursor key schema")
	}
	secret, err := base64.RawURLEncoding.DecodeString(record.Secret)
	if err != nil {
		return observation.CursorKeyMaterial{}, fmt.Errorf("invalid cursor key secret")
	}
	material := observation.CursorKeyMaterial{StateRootEpoch: record.StateRootEpoch, Generation: record.Generation, Secret: secret}
	return material, material.Validate()
}

func readPrivateJSON(path string, maxBytes int64, out any) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err == unix.ENOENT {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return fmt.Errorf("open private state file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || !ownedByCurrent(info) || info.Size() < 1 || info.Size() > maxBytes {
		return fmt.Errorf("unsafe private state file")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fmt.Errorf("trailing json")
	}
	return nil
}

func eventSequences(dir string) ([]observation.ChangeSeq, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	if len(entries) > maxEventScanRecords {
		return nil, fmt.Errorf("event scan limit exceeded")
	}
	sequences := make([]observation.ChangeSeq, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".shellbeam-") {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			return nil, fmt.Errorf("unsafe event entry")
		}
		seq, ok := parseEventFilename(entry.Name())
		if !ok {
			return nil, fmt.Errorf("invalid event filename")
		}
		sequences = append(sequences, seq)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	return sequences, nil
}

func parseEventFilename(name string) (observation.ChangeSeq, bool) {
	if len(name) != len("00000000000000000000.json") || !strings.HasSuffix(name, ".json") {
		return 0, false
	}
	seq, err := strconv.ParseUint(strings.TrimSuffix(name, ".json"), 10, 64)
	return observation.ChangeSeq(seq), err == nil && seq > 0
}
