package contextexec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func resolveChildExecutable(spec ChildSpec) (string, error) {
	if len(spec.Argv) == 0 || spec.Argv[0] == "" || spec.CWD == "" || !filepath.IsAbs(spec.CWD) {
		return "", fmt.Errorf("invalid context child spec")
	}
	requested := spec.Argv[0]
	if strings.ContainsRune(requested, '/') {
		candidate := requested
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(spec.CWD, candidate)
		}
		return validateExecutablePath(candidate)
	}
	pathValue := ""
	for _, entry := range spec.Env {
		if strings.HasPrefix(entry, "PATH=") {
			pathValue = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			dir = spec.CWD
		} else if !filepath.IsAbs(dir) {
			dir = filepath.Join(spec.CWD, dir)
		}
		candidate, err := validateExecutablePath(filepath.Join(dir, requested))
		if err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("context child executable not found")
}
func validateExecutablePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("context child executable is not executable")
	}
	return resolved, nil
}

func ValidateOpaqueLaunchID(value string) error {
	if !validOpaque(value, 128) {
		return fmt.Errorf("invalid context helper launch identity")
	}
	return nil
}
func ExecutableMatches(observed, expected string) bool { return sameExecutable(observed, expected) }
