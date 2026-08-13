package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Address struct {
	WorkspaceID WorkspaceID `json:"workspace_id,omitempty"`
	CWD         string      `json:"cwd,omitempty"`
}

type ResolvedAddress struct {
	WorkspaceID WorkspaceID `json:"workspace_id,omitempty"`
	LogicalCWD  string      `json:"logical_cwd"`
	CWD         string      `json:"cwd"`
}

func (a Address) LogicalCWD() string {
	if a.WorkspaceID != "" && a.CWD == "" {
		return "."
	}
	return a.CWD
}

func (a Address) Validate() error {
	if a.WorkspaceID == "" {
		if a.CWD == "" || !filepath.IsAbs(a.CWD) || strings.IndexByte(a.CWD, 0) >= 0 {
			return fmt.Errorf("absolute cwd required without workspace")
		}
		return nil
	}
	if _, err := ParseWorkspaceID(string(a.WorkspaceID)); err != nil {
		return err
	}
	cwd := a.LogicalCWD()
	if filepath.IsAbs(cwd) || strings.IndexByte(cwd, 0) >= 0 {
		return fmt.Errorf("workspace cwd must be relative")
	}
	for _, part := range strings.FieldsFunc(filepath.ToSlash(cwd), func(r rune) bool { return r == 47 }) {
		if part == ".." {
			return fmt.Errorf("workspace cwd escapes root")
		}
	}
	return nil
}
