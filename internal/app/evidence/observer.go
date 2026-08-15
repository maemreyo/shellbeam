package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/evidence"
	"github.com/maemreyo/shellbeam/internal/core/project"
)

type Limits struct {
	MaxDigestBytes   int64
	MaxTreeEntries   int
	MaxMetadataBytes int
}

type Observer struct {
	limits Limits
	now    func() time.Time
}

func DefaultLimits() Limits {
	return Limits{MaxDigestBytes: core.MaxArtifactDigestBytes, MaxTreeEntries: core.MaxTreeEntries, MaxMetadataBytes: core.MaxArtifactMetadataBytes}
}

func NewObserver(limits Limits) *Observer {
	defaults := DefaultLimits()
	if limits.MaxDigestBytes <= 0 {
		limits.MaxDigestBytes = defaults.MaxDigestBytes
	}
	if limits.MaxTreeEntries <= 0 {
		limits.MaxTreeEntries = defaults.MaxTreeEntries
	}
	if limits.MaxMetadataBytes <= 0 {
		limits.MaxMetadataBytes = defaults.MaxMetadataBytes
	}
	return &Observer{limits: limits, now: func() time.Time { return time.Now().UTC() }}
}

func (o *Observer) Observe(ctx context.Context, root string, outputs []project.Output) ([]core.ArtifactObservation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalized, err := project.ValidateExpectedOutputs(outputs)
	if err != nil {
		return nil, err
	}
	canonicalRoot, err := canonicalWorkspaceRoot(root)
	if err != nil {
		return nil, err
	}
	observations := make([]core.ArtifactObservation, 0, len(normalized))
	for _, output := range normalized {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		observations = append(observations, o.observeOne(ctx, canonicalRoot, output))
	}
	return observations, nil
}

func (o *Observer) observeOne(ctx context.Context, root string, output project.Output) core.ArtifactObservation {
	observation := core.ArtifactObservation{SchemaVersion: core.ArtifactSchemaVersion, Path: output.Path, DeclaredKind: output.Kind, Required: output.Required, DigestMode: output.Digest, ObservedAt: o.now()}
	target, err := containedArtifactPath(root, output.Path)
	if errors.Is(err, errArtifactMissing) {
		return missingObservation(observation)
	}
	if err != nil {
		return unavailableObservation(observation)
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return missingObservation(observation)
	}
	if err != nil {
		return unavailableObservation(observation)
	}
	observation.Exists = true
	observation.ObservedKind = observedKind(info)
	observation.Size = info.Size()
	observation.MTime = info.ModTime().UTC()
	if observation.ObservedKind != output.Kind {
		observation.Status, observation.Quality = core.ArtifactKindMismatch, core.ObservationComplete
		return observation
	}
	switch output.Kind {
	case "file":
		if output.Digest == "sha256" {
			return o.observeFileDigest(ctx, target, info, observation)
		}
	case "directory":
		if output.Digest == "tree-sha256" {
			return o.observeTreeDigest(ctx, target, info, observation)
		}
	case "symlink":
		return o.observeSymlink(target, observation)
	}
	observation.Status, observation.Quality = core.ArtifactCurrent, core.ObservationComplete
	return observation
}

func (o *Observer) observeFileDigest(ctx context.Context, path string, before os.FileInfo, observation core.ArtifactObservation) core.ArtifactObservation {
	digest, readBytes, after, err := completeFileDigest(ctx, path, o.limits.MaxDigestBytes)
	if err != nil || readBytes != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return unavailableObservation(observation)
	}
	observation.Digest = digest
	observation.Status, observation.Quality = core.ArtifactCurrent, core.ObservationComplete
	return observation
}

func (o *Observer) observeSymlink(path string, observation core.ArtifactObservation) core.ArtifactObservation {
	text, err := os.Readlink(path)
	if err != nil || len(text) > o.limits.MaxMetadataBytes {
		return unavailableObservation(observation)
	}
	observation.LinkText = text
	observation.Status, observation.Quality = core.ArtifactCurrent, core.ObservationComplete
	return observation
}

func completeFileDigest(ctx context.Context, path string, limit int64) (string, int64, os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, nil, err
	}
	defer file.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(&contextReader{ctx: ctx, reader: file}, limit+1))
	if err != nil {
		return "", n, nil, err
	}
	if n > limit {
		return "", n, nil, errors.New("digest budget exceeded")
	}
	after, err := file.Stat()
	if err != nil {
		return "", n, nil, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, after, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func missingObservation(value core.ArtifactObservation) core.ArtifactObservation {
	value.Status, value.Quality = core.ArtifactMissing, core.ObservationComplete
	return value
}

func unavailableObservation(value core.ArtifactObservation) core.ArtifactObservation {
	value.Status, value.Quality, value.Digest, value.LinkText = core.ArtifactUnavailable, core.ObservationUnavailable, "", ""
	return value
}

func observedKind(info os.FileInfo) string {
	if info.Mode()&os.ModeSymlink != 0 {
		return "symlink"
	}
	if info.IsDir() {
		return "directory"
	}
	if info.Mode().IsRegular() {
		return "file"
	}
	return "other"
}

func (o *Observer) observeTreeDigest(ctx context.Context, root string, before os.FileInfo, observation core.ArtifactObservation) core.ArtifactObservation {
	digest, err := o.treeDigest(ctx, root)
	if err != nil {
		return unavailableObservation(observation)
	}
	after, err := os.Lstat(root)
	if err != nil || !after.ModTime().Equal(before.ModTime()) {
		return unavailableObservation(observation)
	}
	observation.Digest = digest
	observation.Status, observation.Quality = core.ArtifactCurrent, core.ObservationComplete
	return observation
}

type treeEntry struct {
	path, kind, payload string
	size                int64
}

func (o *Observer) treeDigest(ctx context.Context, root string) (string, error) {
	entries, err := o.collectTree(ctx, root)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	h := sha256.New()
	writeTreeField(h, "shellbeam-tree-sha256-v1")
	for _, entry := range entries {
		writeTreeField(h, entry.kind)
		writeTreeField(h, entry.path)
		writeTreeField(h, entry.payload)
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(entry.size))
		_, _ = h.Write(size[:])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (o *Observer) collectTree(ctx context.Context, root string) ([]treeEntry, error) {
	entries := make([]treeEntry, 0)
	var totalBytes int64
	metadata := 0
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." {
			return err
		}
		rel = filepath.ToSlash(rel)
		if len(entries) >= o.limits.MaxTreeEntries {
			return errors.New("tree entry budget exceeded")
		}
		metadata += len(rel) + 16
		if metadata > o.limits.MaxMetadataBytes {
			return errors.New("tree metadata budget exceeded")
		}
		item, bytesRead, err := o.treeEntry(ctx, path, rel, entry, o.limits.MaxDigestBytes-totalBytes)
		if err != nil {
			return err
		}
		if item.kind == "symlink" {
			metadata += len(item.payload)
			if metadata > o.limits.MaxMetadataBytes {
				return errors.New("tree metadata budget exceeded")
			}
		}
		totalBytes += bytesRead
		if totalBytes > o.limits.MaxDigestBytes {
			return errors.New("tree digest budget exceeded")
		}
		entries = append(entries, item)
		return nil
	})
	return entries, err
}

func (o *Observer) treeEntry(ctx context.Context, path, rel string, entry os.DirEntry, remaining int64) (treeEntry, int64, error) {
	info, err := entry.Info()
	if err != nil {
		return treeEntry{}, 0, err
	}
	if entry.Type()&os.ModeSymlink != 0 {
		text, err := os.Readlink(path)
		if err != nil || len(text)+len(rel) > o.limits.MaxMetadataBytes {
			return treeEntry{}, 0, errors.New("symlink metadata unavailable")
		}
		return treeEntry{path: rel, kind: "symlink", payload: text, size: info.Size()}, 0, nil
	}
	if entry.IsDir() {
		return treeEntry{path: rel, kind: "directory"}, 0, nil
	}
	if !info.Mode().IsRegular() {
		return treeEntry{}, 0, errors.New("unsupported tree entry")
	}
	if remaining < 0 {
		return treeEntry{}, 0, errors.New("tree digest budget exceeded")
	}
	digest, n, after, err := completeFileDigest(ctx, path, remaining)
	if err != nil || n != info.Size() || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return treeEntry{}, n, errors.New("tree file unavailable")
	}
	return treeEntry{path: rel, kind: "file", payload: digest, size: info.Size()}, n, nil
}

func writeTreeField(h hash.Hash, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = h.Write(size[:])
	_, _ = h.Write([]byte(value))
}
