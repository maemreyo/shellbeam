package workspace

import "testing"

func TestWorkspaceIDsAreOpaqueBoundedAndPrefixed(t *testing.T) {
	repo := NewRepositoryID()
	ws := NewWorkspaceID()
	if _, err := ParseRepositoryID(string(repo)); err != nil {
		t.Fatalf("repository id %q: %v", repo, err)
	}
	if _, err := ParseWorkspaceID(string(ws)); err != nil {
		t.Fatalf("workspace id %q: %v", ws, err)
	}
	for _, bad := range []string{"", "repo bad", "repo_", "repo_!"} {
		if _, err := ParseRepositoryID(bad); err == nil {
			t.Errorf("accepted repository id %q", bad)
		}
	}
	for _, bad := range []string{"", "ws bad", "ws_", "ws_!"} {
		if _, err := ParseWorkspaceID(bad); err == nil {
			t.Errorf("accepted workspace id %q", bad)
		}
	}
}
