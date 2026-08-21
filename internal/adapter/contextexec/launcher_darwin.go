//go:build darwin

package contextexec

import "fmt"

type darwinPlatformLauncher struct{}

func NewPlatformLauncher(_ ...string) ChildLauncher { return darwinPlatformLauncher{} }
func (darwinPlatformLauncher) Qualified() bool      { return false }
func (darwinPlatformLauncher) Launch(ChildSpec) (*ChildProcess, error) {
	return nil, fmt.Errorf("descriptor-bound context execution unavailable on darwin")
}
func ExecveatFD(int, []string, []string) error { return fmt.Errorf("execveat unavailable on darwin") }
