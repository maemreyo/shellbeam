//go:build linux || darwin

package process

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func (Owner) BindExecution(spec operation.ExecutionSpec) operation.ExecutionSpec {
	if spec.Mode == "" {
		spec.Mode = operation.ExecutionModeShell
	}
	switch spec.Mode {
	case operation.ExecutionModeShell:
		spec.Executable = spec.Shell
	case operation.ExecutionModeArgv:
		spec = bindArgvExecutable(spec)
	}
	return spec
}

func bindArgvExecutable(spec operation.ExecutionSpec) operation.ExecutionSpec {
	if len(spec.Argv) == 0 || spec.Argv[0] == "" {
		spec.BindingErrorCode = "invalid_execution_spec"
		return spec
	}
	name := spec.Argv[0]
	if strings.ContainsRune(name, filepath.Separator) {
		path := name
		if !filepath.IsAbs(path) {
			path = filepath.Join(spec.CWD, path)
		}
		path = filepath.Clean(path)
		spec.Executable = path
		info, err := os.Stat(path)
		if err != nil {
			spec.BindingErrorCode = executableErrorCode(err)
			return spec
		}
		if info.IsDir() || info.Mode()&0111 == 0 {
			spec.BindingErrorCode = "permission_denied"
		}
		return spec
	}
	path, err := exec.LookPath(name)
	if err != nil {
		spec.Executable = name
		spec.BindingErrorCode = executableErrorCode(err)
		return spec
	}
	if !filepath.IsAbs(path) {
		if absolute, absErr := filepath.Abs(path); absErr == nil {
			path = absolute
		}
	}
	spec.Executable = filepath.Clean(path)
	return spec
}

func executableErrorCode(err error) string {
	if errors.Is(err, os.ErrPermission) {
		return "permission_denied"
	}
	return "executable_not_found"
}

func commandFor(spec operation.ExecutionSpec) (*exec.Cmd, string, error) {
	bound := (Owner{}).BindExecution(spec)
	if bound.BindingErrorCode != "" {
		return nil, bound.BindingErrorCode, errors.New(bound.BindingErrorCode)
	}
	switch bound.Mode {
	case operation.ExecutionModeShell:
		if bound.Shell == "" || bound.Command == "" {
			return nil, "invalid_execution_spec", fmt.Errorf("invalid execution spec")
		}
		return exec.Command(bound.Shell, "-lc", bound.Command), "", nil
	case operation.ExecutionModeArgv:
		if len(bound.Argv) == 0 || bound.Executable == "" {
			return nil, "invalid_execution_spec", fmt.Errorf("invalid execution spec")
		}
		cmd := exec.Command(bound.Executable, bound.Argv[1:]...)
		cmd.Args = append([]string(nil), bound.Argv...)
		return cmd, "", nil
	default:
		return nil, "invalid_execution_spec", fmt.Errorf("invalid execution mode")
	}
}

func commandForFrozen(spec operation.ExecutionSpec) (*exec.Cmd, string, error) {
	if spec.BindingErrorCode != "" || !filepath.IsAbs(spec.Executable) {
		return nil, "invalid_execution_spec", fmt.Errorf("invalid frozen execution spec")
	}
	switch spec.Mode {
	case operation.ExecutionModeShell:
		if spec.Shell == "" || spec.Command == "" || spec.Executable != spec.Shell || len(spec.Argv) != 0 {
			return nil, "invalid_execution_spec", fmt.Errorf("invalid frozen shell execution spec")
		}
		return exec.Command(spec.Executable, "-lc", spec.Command), "", nil
	case operation.ExecutionModeArgv:
		if len(spec.Argv) == 0 || spec.Argv[0] == "" || spec.Command != "" || spec.Shell != "" {
			return nil, "invalid_execution_spec", fmt.Errorf("invalid frozen argv execution spec")
		}
		cmd := exec.Command(spec.Executable, spec.Argv[1:]...)
		cmd.Args = append([]string(nil), spec.Argv...)
		return cmd, "", nil
	default:
		return nil, "invalid_execution_spec", fmt.Errorf("invalid frozen execution mode")
	}
}
