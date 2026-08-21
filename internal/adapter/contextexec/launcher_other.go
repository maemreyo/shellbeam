//go:build !darwin && !linux

package contextexec

import "fmt"

type unavailablePlatformLauncher struct{}

func NewPlatformLauncher(_ ...string) ChildLauncher { return unavailablePlatformLauncher{} }
func (unavailablePlatformLauncher) Qualified() bool { return false }
func (unavailablePlatformLauncher) Launch(ChildSpec) (*ChildProcess, error) {
	return nil, fmt.Errorf("descriptor-bound context execution unavailable")
}
func ExecveatFD(int, []string, []string) error { return fmt.Errorf("execveat unavailable") }
