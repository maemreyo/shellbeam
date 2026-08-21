package contextexec

import (
	"runtime"
	"testing"
)

func TestPlatformLauncherAdvertisesStrongExecutionOnlyWhereQualified(t *testing.T) {
	launcher := NewPlatformLauncher()
	if runtime.GOOS == "darwin" {
		if !launcher.Qualified() {
			t.Fatal("Darwin platform-equivalent execution did not advertise after native qualification")
		}
		return
	}
	if launcher.Qualified() {
		t.Fatalf("%s advertised strong context execution without native qualification", runtime.GOOS)
	}
}
