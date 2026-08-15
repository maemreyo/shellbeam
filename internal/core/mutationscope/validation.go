package mutationscope

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	activity "github.com/maemreyo/shellbeam/internal/core/activity"
	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func ValidateScopeID(v string) error {
	if !safeID(v, MaxScopeIDBytes) {
		return fmt.Errorf("invalid scope id")
	}
	return nil
}

func ValidateMutationID(v string) error {
	if !safeID(v, MaxMutationIDBytes) {
		return fmt.Errorf("invalid mutation id")
	}
	return nil
}

func (s Scope) Validate() error {
	if s.SchemaVersion != SchemaVersion || !safeID(s.ScopeID, MaxScopeIDBytes) || !safeID(s.RevisionID, MaxMutationIDBytes) {
		return fmt.Errorf("invalid scope identity")
	}
	if _, err := activity.ParseID(s.ActivityID); err != nil {
		return err
	}
	if _, err := workspace.ParseWorkspaceID(string(s.WorkspaceID)); err != nil {
		return err
	}
	if s.Mode != ModeRead && s.Mode != ModeMutate {
		return fmt.Errorf("invalid scope mode")
	}
	normalized, err := NormalizeSelectors(s.Paths)
	if err != nil {
		return err
	}
	if len(normalized) != len(s.Paths) {
		return fmt.Errorf("invalid selectors")
	}
	for i := range normalized {
		if normalized[i] != s.Paths[i] {
			return fmt.Errorf("selectors not canonical")
		}
	}
	if s.DeclaredAt.IsZero() || s.ExpiresAt.IsZero() || !s.ExpiresAt.After(s.DeclaredAt) {
		return fmt.Errorf("invalid scope time")
	}
	ttl := s.ExpiresAt.Sub(s.DeclaredAt)
	if ttl < MinTTL || ttl > MaxTTL {
		return fmt.Errorf("invalid scope ttl")
	}
	return nil
}

func (i ScopeIdentity) Validate() error {
	if i.SchemaVersion != SchemaVersion || !safeID(i.ScopeID, MaxScopeIDBytes) || i.BoundAt.IsZero() {
		return fmt.Errorf("invalid scope identity")
	}
	if _, err := activity.ParseID(i.ActivityID); err != nil {
		return err
	}
	_, err := workspace.ParseWorkspaceID(string(i.WorkspaceID))
	return err
}

func (r MutationReceipt) Validate() error {
	if r.SchemaVersion != SchemaVersion || !safeID(r.MutationID, MaxMutationIDBytes) || !safeID(r.ScopeID, MaxScopeIDBytes) || r.CommittedAt.IsZero() || !validDigest(r.RequestFingerprint) {
		return fmt.Errorf("invalid mutation receipt")
	}
	switch r.Result {
	case ResultSet:
		if r.ExpiresAt.IsZero() || !r.ExpiresAt.After(r.CommittedAt) || r.ExpiresAt.Sub(r.CommittedAt) > MaxTTL {
			return fmt.Errorf("invalid set receipt")
		}
	case ResultReleased, ResultAlreadyAbsent:
		if !r.ExpiresAt.IsZero() {
			return fmt.Errorf("release receipt has expiry")
		}
	default:
		return fmt.Errorf("invalid mutation result")
	}
	return nil
}

func BuildAdvisory(a, b Scope, maxExamples int) (Advisory, bool) {
	if a.WorkspaceID == "" || a.WorkspaceID != b.WorkspaceID || a.ScopeID == b.ScopeID || (a.Mode == ModeRead && b.Mode == ModeRead) || !SelectorsOverlap(a.Paths, b.Paths) {
		return Advisory{}, false
	}
	left, right := a, b
	if right.ScopeID < left.ScopeID {
		left, right = right, left
	}
	kind := ConflictMutateMutate
	if left.Mode == ModeRead || right.Mode == ModeRead {
		kind = ConflictReadMutate
	}
	if maxExamples < 0 {
		maxExamples = 0
	}
	examples, truncated := overlapExamples(left.Paths, right.Paths, maxExamples)
	activities := []string{left.ActivityID}
	if right.ActivityID != left.ActivityID {
		activities = append(activities, right.ActivityID)
		sort.Strings(activities)
	}
	advisory := Advisory{Code: "mutation_scope_overlap", WorkspaceID: left.WorkspaceID, ScopeIDs: [2]string{left.ScopeID, right.ScopeID}, ActivityIDs: activities, Modes: [2]Mode{left.Mode, right.Mode}, ConflictKind: kind, OverlapExamples: examples, OverlapExamplesTruncated: truncated}
	advisory.CauseFingerprint = advisoryFingerprint(left, right, kind)
	return advisory, true
}

func overlapExamples(a, b []string, max int) ([]OverlapExample, bool) {
	out := make([]OverlapExample, 0, max)
	total := 0
	for _, left := range a {
		for _, right := range b {
			if selectorOverlap(left, right) {
				total++
				if len(out) < max {
					out = append(out, OverlapExample{Left: left, Right: right})
				}
			}
		}
	}
	return out, total > len(out)
}

func advisoryFingerprint(a, b Scope, kind ConflictKind) string {
	payload := strings.Join([]string{"mutation_scope_overlap", string(a.WorkspaceID), a.ScopeID, a.RevisionID, string(a.Mode), strings.Join(a.Paths, "\x00"), b.ScopeID, b.RevisionID, string(b.Mode), strings.Join(b.Paths, "\x00"), string(kind)}, "\x1f")
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func safeID(v string, max int) bool {
	if v == "" || v == "." || v == ".." || len(v) > max || !utf8.ValidString(v) {
		return false
	}
	for _, r := range v {
		if r == '/' || r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validDigest(v string) bool {
	if len(v) != 64 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}
