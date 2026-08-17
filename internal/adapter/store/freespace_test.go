//go:build linux || darwin

package store

import (
	"path/filepath"
	"testing"
)

func TestAvailableBytesReportsRoomOnTheHostingFilesystem(t *testing.T) {
	available, err := AvailableBytes(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if available <= 0 {
		t.Fatalf("available = %d, want a positive figure", available)
	}
}

// A directory that is not there has no free space to report, and saying "zero"
// would read as a full disk to every caller that only compares against a
// minimum. The error is what distinguishes the two.
func TestAvailableBytesRejectsAMissingDirectory(t *testing.T) {
	if _, err := AvailableBytes(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing directory reported free space")
	}
}
