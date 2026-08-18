package project

import "testing"

func TestProjectStatusAbsentValidInvalidAndReviewDue(t *testing.T) {
	cases := []struct {
		name string
		in   StatusInput
		want Status
	}{
		{"absent", StatusInput{LoadState: LoadAbsent}, StatusAbsent},
		{"invalid", StatusInput{LoadState: LoadInvalid}, StatusInvalid},
		{"valid no review", StatusInput{LoadState: LoadValid, DiscoveryFingerprint: "current"}, StatusReviewDue},
		{"valid reviewed", StatusInput{LoadState: LoadValid, DiscoveryFingerprint: "current", ReviewFingerprint: "current"}, StatusValid},
		{"review due", StatusInput{LoadState: LoadValid, DiscoveryFingerprint: "current", ReviewFingerprint: "old"}, StatusReviewDue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateStatus(tc.in); got != tc.want {
				t.Fatalf("status=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestProjectInspectionCarriesFingerprintAndProvenance(t *testing.T) {
	got := NewInspection(StatusInput{
		LoadState:            LoadValid,
		SchemaVersion:        1,
		ManifestDigest:       "digest",
		DiscoveryFingerprint: "discovery",
		ReviewFingerprint:    "review",
	}, &Manifest{SchemaVersion: 1})
	if got.Status != StatusReviewDue || got.ManifestDigest != "digest" || got.DiscoveryFingerprint != "discovery" || got.ReviewFingerprint != "review" {
		t.Fatalf("inspection=%#v", got)
	}
	if got.Confidence == "" || got.Provenance == "" {
		t.Fatalf("missing confidence/provenance: %#v", got)
	}
}

func TestAbsentProjectInspectionSurfacesBoundedDiscovery(t *testing.T) {
	got := NewInspection(StatusInput{
		LoadState: LoadAbsent, DiscoveryFingerprint: "fingerprint", Code: CodeManifestAbsent,
		DetectedFamilies: []string{"go", "node"}, DiscoveryEvidence: []string{"go:go.mod", "node:package.json"},
	}, nil)
	if got.Status != StatusAbsent || got.Confidence != "medium" || got.Provenance != "workspace_discovery" || got.Code != CodeManifestAbsent {
		t.Fatalf("inspection=%#v", got)
	}
	if len(got.DetectedFamilies) != 2 || len(got.DiscoveryEvidence) != 2 {
		t.Fatalf("discovery fields=%#v", got)
	}
}
