package contextexec

import (
	"runtime"
	"testing"
)

func TestPlatformLauncherAdvertisesStrongExecutionOnlyWhereQualified(t *testing.T) {
	launcher := NewPlatformLauncher()
	if runtime.GOOS == "darwin" && launcher.Qualified() {
		t.Fatal("Darwin advertised descriptor-bound execution after qualification probe failed")
	}
}
