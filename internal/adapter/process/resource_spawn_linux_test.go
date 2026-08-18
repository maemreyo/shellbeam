//go:build linux

package process

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestResourceLinuxCgroupFDCoexistsWithNonTTYAndPTYAttributes(t *testing.T) {
	nonTTY := exec.Command("/bin/true")
	nonTTY.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := applyResourceCgroupFD(nonTTY, 42); err != nil {
		t.Fatal(err)
	}
	if !nonTTY.SysProcAttr.Setpgid || !nonTTY.SysProcAttr.UseCgroupFD || nonTTY.SysProcAttr.CgroupFD != 42 {
		t.Fatalf("non-TTY attrs=%#v", nonTTY.SysProcAttr)
	}

	ptyCmd := exec.Command("/bin/true")
	if err := applyResourceCgroupFD(ptyCmd, 43); err != nil {
		t.Fatal(err)
	}
	// creack/pty.Start mutates the existing SysProcAttr in place before Start.
	ptyCmd.SysProcAttr.Setsid = true
	ptyCmd.SysProcAttr.Setctty = true
	if !ptyCmd.SysProcAttr.Setsid || !ptyCmd.SysProcAttr.Setctty || !ptyCmd.SysProcAttr.UseCgroupFD || ptyCmd.SysProcAttr.CgroupFD != 43 {
		t.Fatalf("PTY attrs=%#v", ptyCmd.SysProcAttr)
	}
}
