package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

const SchemaVersion = 1

type Repository struct {
	SchemaVersion int          `json:"schema_version"`
	ID            RepositoryID `json:"repository_id"`
	CommonDir     string       `json:"common_dir"`
	Bare          bool         `json:"bare"`
	CreatedAt     time.Time    `json:"created_at"`
	LastSeenAt    time.Time    `json:"last_seen_at"`
}

type Workspace struct {
	SchemaVersion int          `json:"schema_version"`
	ID            WorkspaceID  `json:"workspace_id"`
	RepositoryID  RepositoryID `json:"repository_id"`
	Label         string       `json:"label"`
	Root          string       `json:"root"`
	GitDir        string       `json:"git_dir"`
	Branch        string       `json:"branch,omitempty"`
	CreatedAt     time.Time    `json:"created_at"`
	LastSeenAt    time.Time    `json:"last_seen_at"`
}

func (r Repository) Validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported repository schema")
	}
	if _, err := ParseRepositoryID(string(r.ID)); err != nil {
		return err
	}
	if !filepath.IsAbs(r.CommonDir) || r.CreatedAt.IsZero() || r.LastSeenAt.IsZero() || r.LastSeenAt.Before(r.CreatedAt) {
		return fmt.Errorf("invalid repository record")
	}
	return nil
}

func (w Workspace) Validate() error {
	if w.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported workspace schema")
	}
	if _, err := ParseWorkspaceID(string(w.ID)); err != nil {
		return err
	}
	if _, err := ParseRepositoryID(string(w.RepositoryID)); err != nil {
		return err
	}
	if !safeLabel(w.Label) || !filepath.IsAbs(w.Root) || !filepath.IsAbs(w.GitDir) {
		return fmt.Errorf("invalid workspace record")
	}
	if w.CreatedAt.IsZero() || w.LastSeenAt.IsZero() || w.LastSeenAt.Before(w.CreatedAt) {
		return fmt.Errorf("invalid workspace timestamps")
	}
	return nil
}

func safeLabel(v string) bool {
	if strings.TrimSpace(v) == "" || len(v) > 128 {
		return false
	}
	for _, r := range v {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
