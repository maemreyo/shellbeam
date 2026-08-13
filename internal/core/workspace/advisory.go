package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxHintBytes = 128

type Hint struct {
	WorkspaceID WorkspaceID `json:"workspace_id,omitempty"`
	Branch      string      `json:"branch,omitempty"`
	GitProfile  string      `json:"git_profile,omitempty"`
}

type ContextEvent struct {
	Code                  string      `json:"code"`
	Message               string      `json:"message"`
	WorkspaceID           WorkspaceID `json:"workspace_id,omitempty"`
	Generation            string      `json:"generation,omitempty"`
	TransitionFingerprint string      `json:"transition_fingerprint"`
}

type Advisory struct {
	Code             string      `json:"code"`
	Severity         string      `json:"severity"`
	Message          string      `json:"message"`
	WorkspaceID      WorkspaceID `json:"workspace_id,omitempty"`
	CauseFingerprint string      `json:"cause_fingerprint"`
}

func (h Hint) Validate() error {
	if h.WorkspaceID == "" && h.Branch == "" && h.GitProfile == "" {
		return fmt.Errorf("workspace hint is empty")
	}
	if h.WorkspaceID != "" {
		if _, err := ParseWorkspaceID(string(h.WorkspaceID)); err != nil {
			return err
		}
	}
	if !validHintText(h.Branch) || !validHintText(h.GitProfile) {
		return fmt.Errorf("invalid workspace hint text")
	}
	return nil
}

func EvaluateHint(snapshot FastSnapshot, hint *Hint) []Advisory {
	if hint == nil {
		return nil
	}
	if err := hint.Validate(); err != nil {
		return nil
	}
	if snapshot.Quality == QualityUnavailable {
		return []Advisory{newAdvisory("workspace_observation_unavailable", snapshot.WorkspaceID, map[string]string{"diagnostic": snapshot.DiagnosticCode}, "workspace context unavailable; execution continued")}
	}
	mismatch := false
	facts := map[string]string{"expected_workspace": string(hint.WorkspaceID), "actual_workspace": string(snapshot.WorkspaceID), "expected_branch": hint.Branch, "actual_branch": shortBranch(snapshot.Ref)}
	if hint.WorkspaceID != "" && hint.WorkspaceID != snapshot.WorkspaceID {
		mismatch = true
	}
	if hint.Branch != "" && hint.Branch != shortBranch(snapshot.Ref) {
		mismatch = true
	}
	out := make([]Advisory, 0, 2)
	if mismatch {
		out = append(out, newAdvisory("workspace_hint_mismatch", snapshot.WorkspaceID, facts, "workspace context does not match hint; execution continued"))
	}
	if hint.GitProfile != "" {
		out = append(out, newAdvisory("git_profile_unknown", snapshot.WorkspaceID, map[string]string{"profile": hint.GitProfile}, "Git profile was not evaluated; execution continued"))
	}
	return out
}

func ContextEvents(previous, current FastSnapshot) []ContextEvent {
	if previous.Quality == QualityUnavailable || current.Quality == QualityUnavailable {
		return nil
	}
	if previous.WorkspaceID != current.WorkspaceID {
		return []ContextEvent{newContextEvent("workspace_changed", current, map[string]string{"from": string(previous.WorkspaceID), "to": string(current.WorkspaceID)}, "workspace changed")}
	}
	if previous.Ref != current.Ref {
		return []ContextEvent{newContextEvent("branch_changed", current, map[string]string{"from": previous.Ref, "to": current.Ref}, "branch changed")}
	}
	if previous.Head != current.Head {
		return []ContextEvent{newContextEvent("head_changed", current, map[string]string{"from": previous.Head, "to": current.Head}, "HEAD changed")}
	}
	return nil
}

func newAdvisory(code string, workspaceID WorkspaceID, facts map[string]string, message string) Advisory {
	return Advisory{Code: code, Severity: "warning", Message: message, WorkspaceID: workspaceID, CauseFingerprint: factFingerprint(code, facts)}
}

func newContextEvent(code string, snapshot FastSnapshot, facts map[string]string, message string) ContextEvent {
	return ContextEvent{Code: code, Message: message, WorkspaceID: snapshot.WorkspaceID, Generation: snapshot.Generation, TransitionFingerprint: factFingerprint(code, facts)}
}

func factFingerprint(code string, facts map[string]string) string {
	data, _ := json.Marshal(struct {
		Code  string            `json:"code"`
		Facts map[string]string `json:"facts"`
	}{code, facts})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func shortBranch(ref string) string { return strings.TrimPrefix(ref, "refs/heads/") }
func validHintText(value string) bool {
	if value == "" {
		return true
	}
	if len(value) > MaxHintBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
