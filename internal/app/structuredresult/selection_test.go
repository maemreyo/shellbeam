package structuredresult

import "testing"

func TestGoAdapterSelectionIsExplicitOrExactDirectArgvOnly(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		argv     []string
		status   SelectionStatus
		adapter  string
	}{
		{"explicit test", "go-test-json", nil, SelectionSelected, "go-test-json"},
		{"explicit vet wins", "go-vet-json", []string{"go", "test", "-json", "./..."}, SelectionSelected, "go-vet-json"},
		{"unsupported explicit does not fallback", "junit", []string{"go", "test", "-json", "./..."}, SelectionUnsupported, "junit"},
		{"direct test", "", []string{"go", "test", "-json", "./..."}, SelectionSelected, "go-test-json"},
		{"direct vet", "", []string{"go", "vet", "-json", "./..."}, SelectionSelected, "go-vet-json"},
		{"flag not exact position", "", []string{"go", "test", "./...", "-json"}, SelectionNone, ""},
		{"shell pipeline is not direct argv", "", []string{"/bin/sh", "-lc", "go test -json ./... | tee out"}, SelectionNone, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectAdapter(tc.explicit, tc.argv)
			if got.Status != tc.status || got.AdapterID != tc.adapter {
				t.Fatalf("selection=%#v", got)
			}
			if tc.status == SelectionUnsupported && got.ObservationCode != "structured_adapter_unsupported" {
				t.Fatalf("unsupported observation=%#v", got)
			}
		})
	}
}
