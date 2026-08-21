//go:build !darwin && !linux

package contextexec

import "fmt"

type unavailablePlatformLauncher struct{}

func NewPlatformLauncher(_ ...string) ChildLauncher { return unavailablePlatformLauncher{} }
func (unavailablePlatformLauncher) Qualified() bool { return false }
func (unavailablePlatformLauncher) Prepare(ChildSpec) (PreparedExecution, error) {
	return nil, fmt.Errorf("descriptor-bound context execution unavailable")
}
func ExecveatFD(int, []string, []string) error { return fmt.Errorf("execveat unavailable") }
