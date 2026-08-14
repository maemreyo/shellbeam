package workspace

import (
	"strings"
	"testing"
	"time"
)

func TestWorkspaceHintMatchingMismatchChangedAndOmitted(t *testing.T) {
	now := time.Now().UTC()
	base := FastSnapshot{SchemaVersion: SnapshotSchemaVersion, RepositoryID: RepositoryID("repo_01K00000000000000000000000"), WorkspaceID: WorkspaceID("ws_01K00000000000000000000000"), Head: strings.Repeat("a", 40), Ref: "refs/heads/main", Dirty: DirtySummary{Digest: strings.Repeat("b", 64)}, Quality: QualityFresh, ObservedAt: now}
	base, err := WithGeneration(base)
	if err != nil {
		t.Fatal(err)
	}
	matching := Hint{WorkspaceID: base.WorkspaceID, Branch: "main"}
	if advisories := EvaluateHint(base, &matching); len(advisories) != 0 {
		t.Fatalf("matching advisories=%#v", advisories)
	}
	mismatch := Hint{WorkspaceID: base.WorkspaceID, Branch: "topic"}
	first := EvaluateHint(base, &mismatch)
	if len(first) != 1 || first[0].Code != "workspace_hint_mismatch" || first[0].CauseFingerprint == "" {
		t.Fatalf("mismatch=%#v", first)
	}
	changed := base
	changed.Ref = "refs/heads/develop"
	changed, err = WithGeneration(changed)
	if err != nil {
		t.Fatal(err)
	}
	second := EvaluateHint(changed, &mismatch)
	if len(second) != 1 || second[0].CauseFingerprint == first[0].CauseFingerprint {
		t.Fatalf("changed=%#v first=%#v", second, first)
	}
	if advisories := EvaluateHint(base, nil); len(advisories) != 0 {
		t.Fatalf("omitted=%#v", advisories)
	}
}

func TestContextEventsUseSafeTransitionFingerprintAndSuppressDirtyOnlyChange(t *testing.T) {
	now := time.Now().UTC()
	previous := FastSnapshot{SchemaVersion: SnapshotSchemaVersion, RepositoryID: RepositoryID("repo_01K00000000000000000000000"), WorkspaceID: WorkspaceID("ws_01K00000000000000000000000"), Head: strings.Repeat("a", 40), Ref: "refs/heads/main", Dirty: DirtySummary{Digest: strings.Repeat("b", 64)}, Quality: QualityFresh, ObservedAt: now}
	previous, _ = WithGeneration(previous)
	dirty := previous
	dirty.Dirty = DirtySummary{Dirty: true, Modified: 1, Digest: strings.Repeat("c", 64)}
	dirty.ObservedAt = now.Add(time.Second)
	dirty, _ = WithGeneration(dirty)
	if events := ContextEvents(previous, dirty); len(events) != 0 {
		t.Fatalf("dirty-only events=%#v", events)
	}
	branch := dirty
	branch.Ref = "refs/heads/topic"
	branch.ObservedAt = now.Add(2 * time.Second)
	branch, _ = WithGeneration(branch)
	events := ContextEvents(dirty, branch)
	if len(events) != 1 || events[0].Code != "branch_changed" || events[0].TransitionFingerprint == "" {
		t.Fatalf("events=%#v", events)
	}
	if strings.Contains(events[0].TransitionFingerprint, "/") || strings.Contains(events[0].TransitionFingerprint, "topic") {
		t.Fatalf("unsafe fingerprint=%q", events[0].TransitionFingerprint)
	}
}

func TestWorkspaceHintValidationIsBoundedAndControlFree(t *testing.T) {
	if err := (Hint{Branch: "main"}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, hint := range []Hint{{}, {Branch: "bad\x00branch"}, {GitProfile: strings.Repeat("x", 129)}} {
		if err := hint.Validate(); err == nil {
			t.Fatalf("hint %#v accepted", hint)
		}
	}
}
