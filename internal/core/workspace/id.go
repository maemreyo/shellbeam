// Package workspace defines stable local repository and workspace identity.
package workspace

import (
	"fmt"
	"regexp"

	"github.com/oklog/ulid/v2"
)

type RepositoryID string
type WorkspaceID string

var (
	repositoryIDPattern = regexp.MustCompile(`^repo_[0-9A-HJKMNP-TV-Z]{26}$`)
	workspaceIDPattern  = regexp.MustCompile(`^ws_[0-9A-HJKMNP-TV-Z]{26}$`)
)

func NewRepositoryID() RepositoryID { return RepositoryID("repo_" + ulid.Make().String()) }
func NewWorkspaceID() WorkspaceID   { return WorkspaceID("ws_" + ulid.Make().String()) }

func ParseRepositoryID(v string) (RepositoryID, error) {
	if !repositoryIDPattern.MatchString(v) {
		return "", fmt.Errorf("invalid repository id")
	}
	return RepositoryID(v), nil
}

func ParseWorkspaceID(v string) (WorkspaceID, error) {
	if !workspaceIDPattern.MatchString(v) {
		return "", fmt.Errorf("invalid workspace id")
	}
	return WorkspaceID(v), nil
}
