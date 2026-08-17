package media

import (
	"testing"
	"time"
)

func TestV1ConstantsAndLimitsAreFrozen(t *testing.T) {
	if MaxImageBytes != 7<<20 || MaxWidth != 16384 || MaxHeight != 16384 || MaxPixels != 40_000_000 || MaxOuterResponseBytes != 9_852_248 {
		t.Fatalf("limits changed: bytes=%d width=%d height=%d pixels=%d outer=%d", MaxImageBytes, MaxWidth, MaxHeight, MaxPixels, MaxOuterResponseBytes)
	}
	if MaxPathBytes != 1024 || MaxCWDBytes != 1024 || MaxPathComponents != 64 || MaxConcurrentReads != 2 || AcquisitionBudget != 5*time.Second {
		t.Fatalf("operational constants changed")
	}
	if got := V1Limits(); got.MaxImageBytes != MaxImageBytes || got.MaxWidth != MaxWidth || got.MaxHeight != MaxHeight || got.MaxPixels != MaxPixels {
		t.Fatalf("V1Limits=%#v", got)
	}
}

func TestDisplayAddressValidatePreservesExactCallerIdentity(t *testing.T) {
	workspace := DisplayAddress{AddressKind: AddressWorkspace, WorkspaceID: "ws_01K00000000000000000000000", Path: "artifacts/settings.png"}
	if err := workspace.Validate(); err != nil {
		t.Fatal(err)
	}
	if workspace.Path != "artifacts/settings.png" || workspace.WorkspaceID != "ws_01K00000000000000000000000" {
		t.Fatalf("mutated=%#v", workspace)
	}

	cwd := DisplayAddress{AddressKind: AddressCWD, CWD: "/tmp/../tmp", Path: "settings.png"}
	if err := cwd.Validate(); err != nil {
		t.Fatal(err)
	}
	if cwd.CWD != "/tmp/../tmp" {
		t.Fatalf("cwd normalized: %#v", cwd)
	}
}

func TestDisplayAddressRequiresExactlyOneMatchingBase(t *testing.T) {
	cases := []DisplayAddress{
		{},
		{AddressKind: AddressWorkspace, Path: "a.png"},
		{AddressKind: AddressWorkspace, WorkspaceID: "ws_x", CWD: "/tmp", Path: "a.png"},
		{AddressKind: AddressWorkspace, CWD: "/tmp", Path: "a.png"},
		{AddressKind: AddressCWD, WorkspaceID: "ws_x", Path: "a.png"},
		{AddressKind: AddressCWD, CWD: "relative", Path: "a.png"},
		{AddressKind: "other", WorkspaceID: "ws_x", Path: "a.png"},
		{AddressKind: AddressWorkspace, WorkspaceID: "ws_x", Path: "../a.png"},
	}
	for i, in := range cases {
		if err := in.Validate(); err == nil {
			t.Fatalf("case %d accepted: %#v", i, in)
		}
	}
}
