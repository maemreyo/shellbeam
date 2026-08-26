//go:build darwin

package contextexec

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

type darwinSpawnedChild struct {
	pid     int
	process *os.Process
	pipes   *childOutputPipes
}

func spawnSuspendedDarwinChild(targetPath string, spec ChildSpec) (*darwinSpawnedChild, error) {
	pipes, err := newChildOutputPipes()
	if err != nil {
		return nil, err
	}
	stdin, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		_ = pipes.closeAll()
		return nil, err
	}
	defer stdin.Close()

	spawn, err := newDarwinSpawnState(spec.CWD)
	if err != nil {
		_ = pipes.closeAll()
		return nil, err
	}
	defer spawn.close()
	if err := spawn.redirect(stdin.Fd(), pipes); err != nil {
		_ = pipes.closeAll()
		return nil, err
	}
	pid, err := spawn.start(targetPath, spec.Argv, spec.Env)
	if err != nil {
		_ = pipes.closeAll()
		return nil, err
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = pipes.closeAll()
		return nil, err
	}
	if err := pipes.closeParentWriters(); err != nil {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_, _ = process.Wait()
		_ = pipes.closeAll()
		return nil, fmt.Errorf("close parent context child output writers: %w", err)
	}
	return &darwinSpawnedChild{pid: pid, process: process, pipes: pipes}, nil
}

func (c *darwinSpawnedChild) resume() error {
	if c == nil || c.process == nil {
		return fmt.Errorf("suspended context child unavailable")
	}
	if err := syscall.Kill(c.pid, syscall.SIGCONT); err != nil {
		return fmt.Errorf("resume suspended context child: %w", err)
	}
	return nil
}

func (c *darwinSpawnedChild) failClosed() {
	if c == nil {
		return
	}
	if c.process != nil {
		_ = syscall.Kill(-c.pid, syscall.SIGKILL)
		_, _ = c.process.Wait()
	}
	if c.pipes != nil {
		_ = c.pipes.closeAll()
	}
}

func (c *darwinSpawnedChild) publicProcess(resolved string) *ChildProcess {
	var once sync.Once
	var exit ChildExit
	var waitErr error
	wait := func() (ChildExit, error) {
		once.Do(func() {
			state, err := c.process.Wait()
			if err != nil {
				waitErr = err
				return
			}
			exit.Reaped = true
			if code := state.ExitCode(); code >= 0 {
				exit.Code = &code
			}
			if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				exit.Signal = status.Signal().String()
			}
		})
		return exit, waitErr
	}
	kill := func() error { return syscall.Kill(-c.pid, syscall.SIGKILL) }
	return &ChildProcess{
		ResolvedExecutable: resolved,
		Stdout:             c.pipes.stdoutR,
		Stderr:             c.pipes.stderrR,
		Wait:               wait,
		KillGroup:          kill,
	}
}

type darwinSpawnState struct {
	attr    darwinSpawnAttr
	actions darwinSpawnActions
	cwd     *byte
}

func newDarwinSpawnState(cwd string) (*darwinSpawnState, error) {
	state := &darwinSpawnState{}
	if err := darwinSpawnAttrInit(&state.attr); err != nil {
		return nil, err
	}
	if err := darwinSpawnActionsInit(&state.actions); err != nil {
		_ = darwinSpawnAttrDestroy(&state.attr)
		return nil, err
	}
	if err := darwinSpawnAttrSetFlags(&state.attr, darwinSpawnSetPGroup|darwinSpawnStartSuspended); err != nil {
		state.close()
		return nil, err
	}
	if err := darwinSpawnAttrSetPGroup(&state.attr, 0); err != nil {
		state.close()
		return nil, err
	}
	cwdp, err := syscall.BytePtrFromString(cwd)
	if err != nil {
		state.close()
		return nil, err
	}
	state.cwd = cwdp
	if err := darwinSpawnActionsAddChdir(&state.actions, cwdp); err != nil {
		state.close()
		return nil, err
	}
	return state, nil
}

func (s *darwinSpawnState) redirect(stdin uintptr, pipes *childOutputPipes) error {
	if s == nil || pipes == nil {
		return fmt.Errorf("darwin spawn state unavailable")
	}
	for _, mapping := range []struct {
		from uintptr
		to   int
	}{{stdin, 0}, {pipes.stdoutW.Fd(), 1}, {pipes.stderrW.Fd(), 2}} {
		if err := darwinSpawnActionsAddDup2(&s.actions, mapping.from, mapping.to); err != nil {
			return err
		}
	}
	for _, fd := range []uintptr{pipes.stdoutR.Fd(), pipes.stdoutW.Fd(), pipes.stderrR.Fd(), pipes.stderrW.Fd(), stdin} {
		if err := darwinSpawnActionsAddClose(&s.actions, fd); err != nil {
			return err
		}
	}
	return nil
}

func (s *darwinSpawnState) start(targetPath string, argv, env []string) (int, error) {
	if len(argv) == 0 {
		return 0, fmt.Errorf("context child argv unavailable")
	}
	path, err := syscall.BytePtrFromString(targetPath)
	if err != nil {
		return 0, err
	}
	args, err := darwinCStringVector(argv)
	if err != nil {
		return 0, err
	}
	environ, err := darwinCStringVector(env)
	if err != nil {
		return 0, err
	}
	pid, err := darwinPosixSpawn(path, &s.actions, &s.attr, args, environ)
	runtime.KeepAlive(path)
	runtime.KeepAlive(args)
	runtime.KeepAlive(environ)
	runtime.KeepAlive(s.cwd)
	return pid, err
}

func (s *darwinSpawnState) close() error {
	if s == nil {
		return nil
	}
	return errors.Join(darwinSpawnActionsDestroy(&s.actions), darwinSpawnAttrDestroy(&s.attr))
}

func darwinCStringVector(values []string) ([]*byte, error) {
	result := make([]*byte, 0, len(values)+1)
	for _, value := range values {
		ptr, err := syscall.BytePtrFromString(value)
		if err != nil {
			return nil, err
		}
		result = append(result, ptr)
	}
	return append(result, nil), nil
}

func darwinCStringVectorPointer(values []*byte) uintptr {
	if len(values) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&values[0]))
}
