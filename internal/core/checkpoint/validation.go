package checkpoint

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

var (
	checkpointIDPattern = regexp.MustCompile(`^chk_[0-9A-HJKMNP-TV-Z]{26}$`)
	providerIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	safeTokenPattern    = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,127}$`)
	opaqueRefPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

func (p ProviderIdentity) Validate() error {
	if !providerIDPattern.MatchString(p.ID) || p.Version < 1 {
		return fmt.Errorf("invalid checkpoint provider identity")
	}
	return nil
}

func (c ConflictDetection) Validate() error {
	for _, guarantee := range []ConflictGuarantee{c.RegularFile, c.Symlink, c.AbsentToFile, c.DirectoryTree} {
		switch guarantee {
		case ConflictBestEffort, ConflictAtomicConditionalReplace, ConflictUnsupported:
		default:
			return fmt.Errorf("invalid checkpoint conflict guarantee")
		}
	}
	return nil
}

func (r CreateRequest) Validate() error {
	_, err := r.Normalize()
	return err
}

func (r CreateRequest) Normalize() (CreateRequest, error) {
	if _, err := operation.ParseID(r.CreateID); err != nil {
		return CreateRequest{}, fmt.Errorf("invalid checkpoint create id")
	}
	if _, err := workspace.ParseWorkspaceID(r.WorkspaceID); err != nil {
		return CreateRequest{}, err
	}
	if r.ActivityID != "" {
		if _, err := operation.ParseID(r.ActivityID); err != nil {
			return CreateRequest{}, fmt.Errorf("invalid checkpoint activity id")
		}
	}
	paths, err := normalizePaths(r.Paths, MaxCreateSelectors, true)
	if err != nil {
		return CreateRequest{}, err
	}
	total := 0
	for _, value := range paths {
		total += len(value)
	}
	if total > MaxTotalSelectorBytes {
		return CreateRequest{}, fmt.Errorf("checkpoint selector bytes exceed limit")
	}
	r.Paths = paths
	return r, nil
}

func (r RestoreRequest) Validate() error {
	_, err := r.Normalize()
	return err
}

func (r RestoreRequest) Normalize() (RestoreRequest, error) {
	if _, err := operation.ParseID(r.RestoreID); err != nil {
		return RestoreRequest{}, fmt.Errorf("invalid checkpoint restore id")
	}
	if !checkpointIDPattern.MatchString(r.CheckpointID) {
		return RestoreRequest{}, fmt.Errorf("invalid checkpoint id")
	}
	paths, err := normalizePaths(r.Paths, MaxRestorePaths, false)
	if err != nil {
		return RestoreRequest{}, err
	}
	r.Paths = paths
	return r, nil
}

func (c Checkpoint) Validate() error {
	if c.SchemaVersion != SchemaVersion || !checkpointIDPattern.MatchString(c.CheckpointID) {
		return fmt.Errorf("invalid checkpoint metadata")
	}
	if _, err := operation.ParseID(c.CreateID); err != nil {
		return fmt.Errorf("invalid checkpoint create id")
	}
	if err := c.Provider.Validate(); err != nil {
		return err
	}
	if _, err := workspace.ParseWorkspaceID(c.WorkspaceID); err != nil {
		return err
	}
	if c.ActivityID != "" {
		if _, err := operation.ParseID(c.ActivityID); err != nil {
			return fmt.Errorf("invalid checkpoint activity id")
		}
	}
	if !validGeneration(c.SourceGeneration) || c.CreatedAt.IsZero() {
		return fmt.Errorf("invalid checkpoint source metadata")
	}
	if c.CapturedPathCount < 0 || c.CapturedPathCount > MaxCapturedEntries || c.TotalBytes < 0 || c.TotalBytes > MaxCheckpointBytes {
		return fmt.Errorf("invalid checkpoint capture budget")
	}
	if c.CaptureQuality != CaptureComplete {
		return fmt.Errorf("invalid checkpoint capture quality")
	}
	switch c.RetentionState {
	case RetentionAvailable, RetentionPartiallyCompacted, RetentionExpired:
	default:
		return fmt.Errorf("invalid checkpoint retention state")
	}
	if len(c.OpaqueEntryRefs) > MaxPublicEntryRefs || len(c.OpaqueEntryRefs) != c.CapturedPathCount {
		return fmt.Errorf("invalid checkpoint entry references")
	}
	seenRefs := make(map[string]struct{}, len(c.OpaqueEntryRefs))
	for _, ref := range c.OpaqueEntryRefs {
		if len(ref) > MaxOpaqueRefBytes || !opaqueRefPattern.MatchString(ref) {
			return fmt.Errorf("invalid checkpoint entry reference")
		}
		if _, exists := seenRefs[ref]; exists {
			return fmt.Errorf("duplicate checkpoint entry reference")
		}
		seenRefs[ref] = struct{}{}
	}
	if len(c.Excluded)+len(c.Unsupported) > MaxPublicSummaries {
		return fmt.Errorf("too many checkpoint summaries")
	}
	summaries := append(append([]PathSummary(nil), c.Excluded...), c.Unsupported...)
	for _, summary := range summaries {
		if err := summary.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (s PathSummary) Validate() error {
	if err := validateExactPath(s.Path); err != nil {
		return err
	}
	if !safeTokenPattern.MatchString(s.Reason) {
		return fmt.Errorf("invalid checkpoint summary reason")
	}
	return nil
}

func (r RestorePathResult) Validate() error {
	if err := validateExactPath(r.Path); err != nil {
		return err
	}
	switch r.Outcome {
	case RestoreRestored, RestoreNoop, RestoreConflict, RestoreUnsupported, RestoreFailed:
	default:
		return fmt.Errorf("invalid checkpoint restore outcome")
	}
	if r.Reason != "" && !safeTokenPattern.MatchString(r.Reason) {
		return fmt.Errorf("invalid checkpoint restore reason")
	}
	return nil
}

func (r RestoreResult) Validate() error {
	if r.SchemaVersion != SchemaVersion || !checkpointIDPattern.MatchString(r.CheckpointID) {
		return fmt.Errorf("invalid checkpoint restore result")
	}
	if _, err := operation.ParseID(r.RestoreID); err != nil {
		return fmt.Errorf("invalid checkpoint restore id")
	}
	if len(r.Paths) < 1 || len(r.Paths) > MaxRestorePaths {
		return fmt.Errorf("invalid checkpoint restore result paths")
	}
	seen := make(map[string]struct{}, len(r.Paths))
	allSatisfied := true
	for _, result := range r.Paths {
		if err := result.Validate(); err != nil {
			return err
		}
		if _, exists := seen[result.Path]; exists {
			return fmt.Errorf("duplicate checkpoint restore result path")
		}
		seen[result.Path] = struct{}{}
		if result.Outcome != RestoreRestored && result.Outcome != RestoreNoop {
			allSatisfied = false
		}
	}
	if r.Complete && !allSatisfied {
		return fmt.Errorf("checkpoint restore marked complete with unsatisfied path")
	}
	return nil
}

func normalizePaths(input []string, max int, allowSubtree bool) ([]string, error) {
	if len(input) < 1 || len(input) > max {
		return nil, fmt.Errorf("invalid checkpoint path count")
	}
	out := append([]string(nil), input...)
	seen := make(map[string]struct{}, len(out))
	for _, value := range out {
		if len(value) > MaxSelectorBytes {
			return nil, fmt.Errorf("checkpoint path exceeds byte limit")
		}
		if allowSubtree && strings.HasSuffix(value, "/**") {
			base := strings.TrimSuffix(value, "/**")
			if base == "" {
				return nil, fmt.Errorf("whole workspace checkpoint selector unsupported")
			}
			if err := validateExactPath(base); err != nil {
				return nil, err
			}
		} else if err := validateExactPath(value); err != nil {
			return nil, err
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("duplicate checkpoint path")
		}
		seen[value] = struct{}{}
	}
	sort.Strings(out)
	return out, nil
}

func validateExactPath(value string) error {
	if value == "" || len(value) > MaxSelectorBytes || !utf8.ValidString(value) || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, `\`) || strings.ContainsAny(value, "*?[]{}") {
		return fmt.Errorf("invalid checkpoint path")
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("invalid checkpoint path")
		}
		for _, r := range part {
			if unicode.IsControl(r) {
				return fmt.Errorf("invalid checkpoint path")
			}
		}
	}
	return nil
}

func validGeneration(value string) bool {
	if !strings.HasPrefix(value, "gen_") || len(value) != 68 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "gen_"))
	return err == nil
}
