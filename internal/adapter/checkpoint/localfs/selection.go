package localfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	checkpointapp "github.com/maemreyo/shellbeam/internal/app/checkpoint"
	core "github.com/maemreyo/shellbeam/internal/core/checkpoint"
	"github.com/maemreyo/shellbeam/internal/core/failure"
	"golang.org/x/sys/unix"
)

type selectionLimits struct {
	MaxWalkEntries      int
	MaxCapturedEntries  int
	MaxRegularFileBytes int64
	MaxCheckpointBytes  int64
}

func defaultSelectionLimits() selectionLimits {
	return selectionLimits{
		MaxWalkEntries: core.MaxWalkEntries, MaxCapturedEntries: core.MaxCapturedEntries,
		MaxRegularFileBytes: core.MaxRegularFileBytes, MaxCheckpointBytes: core.MaxCheckpointBytes,
	}
}

func (p *Provider) selectCapture(ctx context.Context, request checkpointapp.CaptureRequest, limits selectionLimits) (selectedCapture, error) {
	selectors, err := canonicalSelectors(request.Paths)
	if err != nil {
		if typed, ok := err.(*failure.Failure); ok {
			return selectedCapture{}, typed
		}
		return selectedCapture{}, checkpointFailure(failure.CheckpointScopeInvalid, map[string]string{"field": "paths", "reason": "selector_invalid"}, err)
	}
	if !filepath.IsAbs(request.Root) {
		return selectedCapture{}, checkpointFailure(failure.CheckpointScopeInvalid, map[string]string{"field": "workspace_id", "reason": "root_invalid"}, nil)
	}
	rootFD, err := openDirNoFollow(request.Root)
	if err != nil {
		return selectedCapture{}, checkpointFailure(failure.CheckpointScopeInvalid, map[string]string{"field": "workspace_id", "reason": "root_unavailable"}, err)
	}
	defer unix.Close(rootFD)
	state := &selectionState{provider: p, request: request, limits: limits, rootFD: rootFD, seen: map[string]struct{}{}}
	for _, selector := range selectors {
		if err := ctx.Err(); err != nil {
			return selectedCapture{}, err
		}
		if strings.HasSuffix(selector, "/**") {
			base := strings.TrimSuffix(selector, "/**")
			if err := state.addSubtree(ctx, base); err != nil {
				return selectedCapture{}, err
			}
		} else if err := state.addExact(ctx, selector); err != nil {
			return selectedCapture{}, err
		}
	}
	sort.Slice(state.result.Entries, func(i, j int) bool { return state.result.Entries[i].Path < state.result.Entries[j].Path })
	sort.Slice(state.result.Excluded, func(i, j int) bool { return state.result.Excluded[i].Path < state.result.Excluded[j].Path })
	return state.result, nil
}

type selectionState struct {
	provider *Provider
	request  checkpointapp.CaptureRequest
	limits   selectionLimits
	rootFD   int
	walked   int
	seen     map[string]struct{}
	result   selectedCapture
}

func (s *selectionState) budget(field string, limit any) error {
	return checkpointFailure(failure.CheckpointBudgetExceeded, map[string]string{"field": field, "reason": "selection_limit", "limit": fmt.Sprint(limit)}, nil)
}

func (s *selectionState) visit() error {
	s.walked++
	if s.walked > s.limits.MaxWalkEntries {
		return s.budget("walk_entries", s.limits.MaxWalkEntries)
	}
	return nil
}

func (s *selectionState) addResult(path string, kind entryKind, bytes int64) error {
	if _, ok := s.seen[path]; ok {
		return nil
	}
	if len(s.result.Entries)+1 > s.limits.MaxCapturedEntries {
		return s.budget("captured_entries", s.limits.MaxCapturedEntries)
	}
	if bytes > s.limits.MaxRegularFileBytes && kind == entryFile {
		return s.budget("regular_file_bytes", s.limits.MaxRegularFileBytes)
	}
	if s.result.TotalBytes+bytes > s.limits.MaxCheckpointBytes {
		return s.budget("checkpoint_bytes", s.limits.MaxCheckpointBytes)
	}
	s.seen[path] = struct{}{}
	s.result.Entries = append(s.result.Entries, selectedEntry{Path: path, Kind: kind})
	s.result.TotalBytes += bytes
	return nil
}

func (s *selectionState) addExact(ctx context.Context, rel string) error {
	if err := s.rejectSubmoduleCrossing(rel); err != nil {
		return err
	}
	if err := s.visit(); err != nil {
		return err
	}
	if excluded, reason := s.excluded(rel); excluded {
		s.result.Excluded = append(s.result.Excluded, core.PathSummary{Path: rel, Reason: reason})
		return nil
	}
	parent, name, err := openParentAt(s.rootFD, rel)
	if err != nil {
		if isNotExist(err) {
			return s.addResult(rel, entryAbsent, 0)
		}
		return checkpointFailure(failure.CheckpointPathUnsupported, map[string]string{"path": rel, "reason": "parent_unavailable"}, err)
	}
	defer unix.Close(parent)
	st, err := statAtNoFollow(parent, name)
	if err != nil {
		if isNotExist(err) {
			return s.addResult(rel, entryAbsent, 0)
		}
		return checkpointFailure(failure.CheckpointPathUnsupported, map[string]string{"path": rel, "reason": "stat_failed"}, err)
	}
	return s.addClassified(ctx, parent, name, rel, st, false)
}

func (s *selectionState) addSubtree(ctx context.Context, rel string) error {
	if err := s.rejectSubmoduleCrossing(rel); err != nil {
		return err
	}
	if err := s.visit(); err != nil {
		return err
	}
	if excluded, reason := s.excluded(rel); excluded {
		s.result.Excluded = append(s.result.Excluded, core.PathSummary{Path: rel, Reason: reason})
		return nil
	}
	parent, name, err := openParentAt(s.rootFD, rel)
	if err != nil {
		return checkpointFailure(failure.CheckpointPathUnsupported, map[string]string{"path": rel, "reason": "subtree_unavailable"}, err)
	}
	defer unix.Close(parent)
	st, err := statAtNoFollow(parent, name)
	if err != nil {
		return checkpointFailure(failure.CheckpointPathUnsupported, map[string]string{"path": rel, "reason": "subtree_unavailable"}, err)
	}
	if fileType(st) != unix.S_IFDIR {
		return checkpointFailure(failure.CheckpointPathUnsupported, map[string]string{"path": rel, "reason": "subtree_not_directory"}, nil)
	}
	if err := s.addResult(rel, entryDirectory, 0); err != nil {
		return err
	}
	dirFD, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return checkpointFailure(failure.CheckpointPathUnsupported, map[string]string{"path": rel, "reason": "directory_open_failed"}, err)
	}
	defer unix.Close(dirFD)
	return s.walkDir(ctx, dirFD, rel)
}

func (s *selectionState) walkDir(ctx context.Context, dirFD int, relDir string) error {
	if err := s.rejectSubmodule(dirFD, relDir); err != nil {
		return err
	}
	dup, err := dupFD(dirFD)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(dup), relDir)
	if file == nil {
		_ = unix.Close(dup)
		return fmt.Errorf("open directory")
	}
	entries, err := file.ReadDir(-1)
	_ = file.Close()
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, de := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.visit(); err != nil {
			return err
		}
		name := de.Name()
		if !safeComponent(name) {
			return checkpointFailure(failure.CheckpointPathUnsupported, map[string]string{"path": relDir, "reason": "unsafe_name"}, nil)
		}
		rel := relDir + "/" + name
		if excluded, reason := s.excluded(rel); excluded {
			s.result.Excluded = append(s.result.Excluded, core.PathSummary{Path: rel, Reason: reason})
			continue
		}
		st, err := statAtNoFollow(dirFD, name)
		if err != nil {
			return err
		}
		if err := s.addClassified(ctx, dirFD, name, rel, st, true); err != nil {
			return err
		}
	}
	return nil
}

func (s *selectionState) addClassified(ctx context.Context, parent int, name, rel string, st unix.Stat_t, recurse bool) error {
	switch fileType(st) {
	case unix.S_IFREG:
		return s.addResult(rel, entryFile, st.Size)
	case unix.S_IFLNK:
		text, err := readlinkAt(parent, name)
		if err != nil {
			return err
		}
		return s.addResult(rel, entrySymlink, int64(len(text)))
	case unix.S_IFDIR:
		if err := s.addResult(rel, entryDirectory, 0); err != nil {
			return err
		}
		if !recurse {
			return nil
		}
		fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		return s.walkDir(ctx, fd, rel)
	default:
		return checkpointFailure(failure.CheckpointPathUnsupported, map[string]string{"path": rel, "reason": "special_file"}, nil)
	}
}

func readlinkAt(parent int, name string) (string, error) {
	buf := make([]byte, 256)
	for len(buf) <= 64<<10 {
		n, err := unix.Readlinkat(parent, name, buf)
		if err != nil {
			return "", err
		}
		if n < len(buf) {
			return string(buf[:n]), nil
		}
		buf = make([]byte, len(buf)*2)
	}
	return "", fmt.Errorf("symlink text exceeds limit")
}

func (s *selectionState) rejectSubmoduleCrossing(rel string) error {
	return rejectSubmoduleCrossing(s.rootFD, rel)
}

func (s *selectionState) rejectSubmodule(dirFD int, rel string) error {
	return rejectSubmoduleAt(dirFD, rel)
}

func (s *selectionState) excluded(rel string) (bool, string) {
	for _, part := range strings.Split(rel, "/") {
		if part == ".git" {
			return true, "git_metadata"
		}
	}
	candidate := filepath.Clean(filepath.Join(s.request.Root, filepath.FromSlash(rel)))
	if pathWithin(candidate, s.provider.stateDir) {
		return true, "provider_state"
	}
	if pathWithin(candidate, s.provider.runtimeDir) {
		return true, "provider_runtime"
	}
	return false, ""
}

func pathWithin(candidate, root string) bool {
	candidate = filepath.Clean(candidate)
	root = filepath.Clean(root)
	if candidate == root {
		return true
	}
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
