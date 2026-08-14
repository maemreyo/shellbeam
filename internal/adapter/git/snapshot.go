package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (r *Repository) Snapshot(ctx context.Context, workspace core.Workspace) core.FastSnapshot {
	return r.snapshot(ctx, workspace, true)
}

func (r *Repository) SnapshotFresh(ctx context.Context, workspace core.Workspace) core.FastSnapshot {
	return r.snapshot(ctx, workspace, false)
}

func (r *Repository) snapshot(ctx context.Context, workspace core.Workspace, allowWarmCache bool) core.FastSnapshot {
	now := r.snapshotOptions.Now().UTC()
	if err := workspace.Validate(); err != nil {
		return unavailableSnapshot(workspace, now, "workspace_invalid")
	}
	key := snapshotCacheKey(workspace)
	cached, hasCached, current := r.snapshots.lookup(key, now, r.snapshotOptions.TTL)
	if allowWarmCache && current {
		return cached
	}

	flight, leader := r.snapshots.begin(key)
	budgetCtx, cancel := context.WithTimeout(ctx, r.snapshotOptions.Budget)
	defer cancel()
	if !leader {
		if got, ok := waitSnapshotFlight(budgetCtx, flight); ok {
			return got
		}
		return unavailableSnapshot(workspace, now, "observation_budget_exceeded")
	}

	fresh, diagnostic := r.readFreshSnapshot(budgetCtx, workspace, now)
	result := fresh
	if diagnostic != "" {
		if hasCached {
			result = cached
			result.Quality = core.QualityStale
			result.DiagnosticCode = diagnostic
		} else {
			result = unavailableSnapshot(workspace, now, diagnostic)
		}
	}
	r.snapshots.complete(key, flight, result)
	return result
}

func (r *Repository) readFreshSnapshot(ctx context.Context, workspace core.Workspace, now time.Time) (core.FastSnapshot, string) {
	stdout, _, err := r.runner.Run(ctx, "--no-optional-locks", "-C", workspace.Root, "status", "--porcelain=v2", "-z", "--branch", "--renames", "--untracked-files=normal")
	if err != nil {
		return core.FastSnapshot{}, observationErrorCode(ctx, err)
	}
	parsed, err := parseSnapshotStatus(stdout)
	if err != nil {
		return core.FastSnapshot{}, "git_status_malformed"
	}
	transient, err := transientState(workspace.GitDir)
	if err != nil {
		return core.FastSnapshot{}, "transient_state_unavailable"
	}

	snapshot := core.FastSnapshot{
		SchemaVersion:   core.SnapshotSchemaVersion,
		RepositoryID:    workspace.RepositoryID,
		WorkspaceID:     workspace.ID,
		Head:            parsed.head,
		Ref:             parsed.ref,
		Detached:        parsed.detached,
		Upstream:        parsed.upstream,
		Ahead:           parsed.ahead,
		Behind:          parsed.behind,
		UpstreamQuality: core.QualityUnavailable,
		Dirty:           parsed.dirty,
		Transient:       transient,
		Quality:         core.QualityFresh,
		ObservedAt:      now,
	}
	if parsed.upstream != "" {
		quality, code := r.upstreamQuality(ctx, workspace.Root)
		if code == "observation_budget_exceeded" {
			return core.FastSnapshot{}, code
		}
		snapshot.UpstreamQuality = quality
		if code != "" {
			snapshot.DiagnosticCode = code
		}
	}
	generated, err := core.WithGeneration(snapshot)
	if err != nil {
		return core.FastSnapshot{}, "snapshot_invalid"
	}
	return generated, ""
}

func (r *Repository) upstreamQuality(ctx context.Context, root string) (core.ObservationQuality, string) {
	stdout, _, err := r.runner.Run(ctx, "--no-optional-locks", "-C", root, "rev-parse", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return core.QualityUnavailable, "observation_budget_exceeded"
		}
		return core.QualityUnavailable, "upstream_ref_unavailable"
	}
	ref := strings.TrimSpace(string(stdout))
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		return core.QualityFresh, ""
	case strings.HasPrefix(ref, "refs/remotes/"):
		return core.QualityStale, ""
	default:
		return core.QualityStale, ""
	}
}

type parsedSnapshotStatus struct {
	head     string
	ref      string
	detached bool
	upstream string
	ahead    int
	behind   int
	dirty    core.DirtySummary
}

func parseSnapshotStatus(data []byte) (parsedSnapshotStatus, error) {
	var parsed parsedSnapshotStatus
	dirtyBytes := bytes.NewBuffer(nil)
	tokens := bytes.Split(data, []byte{0})
	for i := 0; i < len(tokens); i++ {
		token := string(tokens[i])
		if token == "" {
			continue
		}
		switch {
		case strings.HasPrefix(token, "# branch.oid "):
			parsed.head = strings.TrimPrefix(token, "# branch.oid ")
		case strings.HasPrefix(token, "# branch.head "):
			head := strings.TrimPrefix(token, "# branch.head ")
			if head == "(detached)" {
				parsed.detached = true
				parsed.ref = ""
			} else if head != "(unknown)" {
				parsed.ref = "refs/heads/" + head
			}
		case strings.HasPrefix(token, "# branch.upstream "):
			parsed.upstream = strings.TrimPrefix(token, "# branch.upstream ")
		case strings.HasPrefix(token, "# branch.ab "):
			if err := parseAheadBehind(strings.TrimPrefix(token, "# branch.ab "), &parsed); err != nil {
				return parsedSnapshotStatus{}, err
			}
		case strings.HasPrefix(token, "1 "):
			fields := strings.Fields(token)
			if len(fields) < 8 {
				return parsedSnapshotStatus{}, fmt.Errorf("short ordinary status")
			}
			countStatusXY(&parsed.dirty, fields[1])
			appendDirtyToken(dirtyBytes, tokens[i])
		case strings.HasPrefix(token, "2 "):
			fields := strings.Fields(token)
			if len(fields) < 9 || i+1 >= len(tokens) || len(tokens[i+1]) == 0 {
				return parsedSnapshotStatus{}, fmt.Errorf("short rename status")
			}
			parsed.dirty.Renamed++
			appendDirtyToken(dirtyBytes, tokens[i])
			i++
			appendDirtyToken(dirtyBytes, tokens[i])
		case strings.HasPrefix(token, "u "):
			fields := strings.Fields(token)
			if len(fields) < 10 {
				return parsedSnapshotStatus{}, fmt.Errorf("short unmerged status")
			}
			parsed.dirty.Conflicted++
			appendDirtyToken(dirtyBytes, tokens[i])
		case strings.HasPrefix(token, "? "):
			parsed.dirty.Untracked++
			appendDirtyToken(dirtyBytes, tokens[i])
		case strings.HasPrefix(token, "! "):
		default:
			return parsedSnapshotStatus{}, fmt.Errorf("unknown porcelain record")
		}
	}
	if parsed.head == "" || parsed.head == "(initial)" {
		return parsedSnapshotStatus{}, fmt.Errorf("head unavailable")
	}
	parsed.dirty.Dirty = dirtyBytes.Len() != 0
	sum := sha256.Sum256(dirtyBytes.Bytes())
	parsed.dirty.Digest = fmt.Sprintf("%x", sum[:])
	return parsed, nil
}

func parseAheadBehind(value string, parsed *parsedSnapshotStatus) error {
	fields := strings.Fields(value)
	if len(fields) != 2 || !strings.HasPrefix(fields[0], "+") || !strings.HasPrefix(fields[1], "-") {
		return fmt.Errorf("invalid ahead behind")
	}
	ahead, err := strconv.Atoi(strings.TrimPrefix(fields[0], "+"))
	if err != nil {
		return err
	}
	behind, err := strconv.Atoi(strings.TrimPrefix(fields[1], "-"))
	if err != nil {
		return err
	}
	parsed.ahead, parsed.behind = ahead, behind
	return nil
}

func countStatusXY(summary *core.DirtySummary, xy string) {
	if strings.ContainsAny(xy, "RC") {
		summary.Renamed++
	} else if strings.Contains(xy, "A") {
		summary.Added++
	} else if strings.Contains(xy, "D") {
		summary.Deleted++
	} else {
		summary.Modified++
	}
}

func appendDirtyToken(buffer *bytes.Buffer, token []byte) {
	_, _ = buffer.Write(token)
	_ = buffer.WriteByte(0)
}

func transientState(gitDir string) (core.TransientState, error) {
	merge, err := markerExists(gitDir, "MERGE_HEAD")
	if err != nil {
		return core.TransientState{}, err
	}
	rebaseMerge, err := markerExists(gitDir, "rebase-merge")
	if err != nil {
		return core.TransientState{}, err
	}
	rebaseApply, err := markerExists(gitDir, "rebase-apply")
	if err != nil {
		return core.TransientState{}, err
	}
	cherryPick, err := markerExists(gitDir, "CHERRY_PICK_HEAD")
	if err != nil {
		return core.TransientState{}, err
	}
	revert, err := markerExists(gitDir, "REVERT_HEAD")
	if err != nil {
		return core.TransientState{}, err
	}
	bisectLog, err := markerExists(gitDir, "BISECT_LOG")
	if err != nil {
		return core.TransientState{}, err
	}
	bisectStart, err := markerExists(gitDir, "BISECT_START")
	if err != nil {
		return core.TransientState{}, err
	}
	return core.TransientState{Merge: merge, Rebase: rebaseMerge || rebaseApply, CherryPick: cherryPick, Revert: revert, Bisect: bisectLog || bisectStart}, nil
}

func markerExists(gitDir, name string) (bool, error) {
	_, err := os.Lstat(filepath.Join(gitDir, name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func snapshotCacheKey(workspace core.Workspace) string {
	return string(workspace.ID) + "\x00" + workspace.Root + "\x00" + workspace.GitDir
}

func unavailableSnapshot(workspace core.Workspace, now time.Time, code string) core.FastSnapshot {
	return core.FastSnapshot{SchemaVersion: core.SnapshotSchemaVersion, RepositoryID: workspace.RepositoryID, WorkspaceID: workspace.ID, Quality: core.QualityUnavailable, UpstreamQuality: core.QualityUnavailable, ObservedAt: now, DiagnosticCode: code}
}

func observationErrorCode(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "observation_budget_exceeded"
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return "observation_cancelled"
	}
	return "git_status_unavailable"
}
