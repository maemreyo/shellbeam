package process

import "testing"

func TestCountProcessTreeIncludesDescendantsAcrossProcessGroups(t *testing.T) {
	parents := map[int]int{
		100: 1,
		101: 100,
		102: 100,
		103: 101,
		200: 1,
	}
	if got := countProcessTree(100, parents); got != 4 {
		t.Fatalf("tree count=%d want 4", got)
	}
	if got := countProcessTree(200, parents); got != 1 {
		t.Fatalf("independent tree count=%d want 1", got)
	}
}
