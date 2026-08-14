package git

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func (r *Repository) SampleDelta(ctx context.Context, workspace core.Workspace, limits core.DeltaLimits) core.DeltaSample {
	limits = limits.Normalize()
	now := time.Now().UTC()
	sample := core.DeltaSample{SchemaVersion: core.DeltaSampleSchemaVersion, RepositoryID: workspace.RepositoryID, WorkspaceID: workspace.ID, Freshness: core.SampleFreshlySampled, Completeness: core.SelectionUnavailable, ObservedAt: now}
	if workspace.Validate() != nil || limits.Validate() != nil {
		sample.DiagnosticCode = "delta_request_invalid"
		return sample
	}
	boundedCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.TimeoutMS)*time.Millisecond)
	defer cancel()
	stdout, _, err := runGitBounded(r.runner, boundedCtx, limits.MaxOutputBytes, "--no-optional-locks", "-C", workspace.Root, "status", "--porcelain=v2", "-z", "--branch", "--no-renames", "--untracked-files=all")
	if errors.Is(err, errOutputLimit) {
		sample = parseDeltaStatus(stdout, workspace, now, limits)
		sample.Completeness = core.SelectionPartial
		sample.DiagnosticCode = "output_limit_exceeded"
		return sample
	}
	if err != nil {
		if errors.Is(boundedCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			sample.DiagnosticCode = "git_status_timeout"
		} else {
			sample.DiagnosticCode = "git_status_unavailable"
		}
		return sample
	}
	sample = parseDeltaStatus(stdout, workspace, now, limits)
	return sample
}

func parseDeltaStatus(data []byte, workspace core.Workspace, observedAt time.Time, limits core.DeltaLimits) core.DeltaSample {
	sample := core.DeltaSample{SchemaVersion: core.DeltaSampleSchemaVersion, RepositoryID: workspace.RepositoryID, WorkspaceID: workspace.ID, Freshness: core.SampleFreshlySampled, Completeness: core.SelectionComplete, ObservedAt: observedAt, BytesObserved: int64(len(data))}
	for _, raw := range bytes.Split(data, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		record := string(raw)
		if strings.HasPrefix(record, "# ") {
			parseDeltaBranchHeader(&sample, record)
			continue
		}
		change, ok := parseDeltaChange(record)
		if !ok {
			sample.Completeness = core.SelectionPartial
			sample.DiagnosticCode = "git_status_record_unrecognized"
			continue
		}
		sample.RecordsObserved++
		if len(sample.Changes) < limits.MaxPaths {
			sample.Changes = append(sample.Changes, change)
		} else if sample.DiagnosticCode == "" {
			sample.Completeness = core.SelectionPartial
			sample.DiagnosticCode = "path_limit_exceeded"
		}
	}
	return sample
}

func parseDeltaBranchHeader(sample *core.DeltaSample, record string) {
	if strings.HasPrefix(record, "# branch.oid ") {
		oid := strings.TrimPrefix(record, "# branch.oid ")
		if oid == "(initial)" {
			sample.Unborn = true
			return
		}
		sample.Head = oid
		return
	}
	if strings.HasPrefix(record, "# branch.head ") {
		head := strings.TrimPrefix(record, "# branch.head ")
		if head == "(detached)" {
			sample.Detached = true
			return
		}
		sample.Ref = "refs/heads/" + head
	}
}

func parseDeltaChange(record string) (core.ChangeRecord, bool) {
	if strings.HasPrefix(record, "? ") {
		path := strings.TrimPrefix(record, "? ")
		change := core.ChangeRecord{PathTransition: core.PathAdded, NewPath: path, SourceTransition: core.SourceAvailabilityChanged, VCSTransition: core.VCSOther, Untracked: true}
		return change, change.Validate() == nil
	}
	if strings.HasPrefix(record, "u ") {
		fields := strings.SplitN(record, " ", 11)
		if len(fields) != 11 {
			return core.ChangeRecord{}, false
		}
		change := core.ChangeRecord{PathTransition: core.PathUnmerged, NewPath: fields[10], SourceTransition: core.SourceIdentityChanged, VCSTransition: core.VCSOther, Submodule: fields[2] != "N..."}
		return change, change.Validate() == nil
	}
	if !strings.HasPrefix(record, "1 ") {
		return core.ChangeRecord{}, false
	}
	fields := strings.SplitN(record, " ", 9)
	if len(fields) != 9 || len(fields[1]) != 2 {
		return core.ChangeRecord{}, false
	}
	xy, submodule, path := fields[1], fields[2] != "N...", fields[8]
	change := core.ChangeRecord{PathTransition: core.PathModified, NewPath: path, SourceTransition: core.SourceBytesChanged, VCSTransition: deltaVCSTransition(xy), Submodule: submodule}
	if xy[0] == "D"[0] || xy[1] == "D"[0] {
		change.PathTransition, change.OldPath, change.NewPath, change.SourceTransition = core.PathDeleted, path, "", core.SourceAvailabilityChanged
	}
	if xy[0] == "A"[0] || xy[1] == "A"[0] {
		change.PathTransition, change.SourceTransition = core.PathAdded, core.SourceAvailabilityChanged
	}
	if xy[0] == "T"[0] || xy[1] == "T"[0] || submodule {
		change.TypeChanged = xy[0] == "T"[0] || xy[1] == "T"[0]
		change.SourceTransition = core.SourceIdentityChanged
	}
	return change, change.Validate() == nil
}

func deltaVCSTransition(xy string) core.VCSTransition {
	if len(xy) == 2 && xy[0] != "."[0] {
		return core.VCSStaged
	}
	return core.VCSOther
}
