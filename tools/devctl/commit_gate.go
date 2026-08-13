package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func stagedFiles() ([]string, error) {
	return stagedFilesIn(".")
}

func stagedFilesIn(root string) ([]string, error) {
	data, err := commandIn(root, "git", "diff", "--cached", "--name-status", "-z")
	if err != nil {
		return nil, err
	}
	paths, err := parseNameStatusZ(data)
	if err != nil {
		return nil, err
	}
	return normalizedPaths(paths), nil
}

func cleanGitChildEnv(env []string) []string {
	blocked := map[string]bool{
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true,
		"GIT_CONFIG":                       true,
		"GIT_CONFIG_PARAMETERS":            true,
		"GIT_CONFIG_COUNT":                 true,
		"GIT_OBJECT_DIRECTORY":             true,
		"GIT_DIR":                          true,
		"GIT_WORK_TREE":                    true,
		"GIT_IMPLICIT_WORK_TREE":           true,
		"GIT_GRAFT_FILE":                   true,
		"GIT_INDEX_FILE":                   true,
		"GIT_NO_REPLACE_OBJECTS":           true,
		"GIT_REPLACE_REF_BASE":             true,
		"GIT_PREFIX":                       true,
		"GIT_SHALLOW_FILE":                 true,
		"GIT_COMMON_DIR":                   true,
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && blocked[key] {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func runCommitGate(receipt *Evidence) error {
	if err := checkCachedDiff(); err != nil {
		return err
	}
	if err := checkChangedPaths(".", receipt.ChangedFiles); err != nil {
		return err
	}
	if err := checkGoFormat(receipt.ChangedFiles); err != nil {
		return err
	}
	cfg, err := loadImpactConfig("dev/test-impact.toml")
	if err != nil {
		return err
	}
	selection := selectImpact(cfg, receipt.ChangedFiles)
	setSelectionEvidence(receipt, selection)
	if selection.Mode == "empty" {
		return nil
	}
	if err := runGoTest(receipt.SelectedPackages, false); err != nil {
		return err
	}
	return runGoVet(receipt.SelectedPackages)
}

func setSelectionEvidence(receipt *Evidence, selection impactSelection) error {
	receipt.Selection = selection.Mode
	receipt.SelectedSuites = selection.Suites
	receipt.SelectionReasons = selection.Reasons
	packages, err := goSuites(selection.Suites)
	if err != nil {
		return err
	}
	receipt.SelectedPackages = packages
	return nil
}

func checkCachedDiff() error {
	cmd := exec.Command("git", "diff", "--cached", "--check")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git diff --cached --check: %w\n%s", err, output.String())
	}
	return nil
}

func checkChangedPaths(root string, paths []string) error {
	for _, path := range paths {
		if forbiddenPath(path) {
			return fmt.Errorf("forbidden package path %s", path)
		}
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if info.Mode().IsRegular() {
			if err := checkFile(full); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkGoFormat(paths []string) error {
	var files []string
	for _, path := range paths {
		if !strings.HasSuffix(path, ".go") {
			continue
		}
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			files = append(files, path)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if len(files) == 0 {
		return nil
	}
	args := append([]string{"-l"}, files...)
	output, err := command("gofmt", args...)
	if err != nil {
		return fmt.Errorf("gofmt check: %w", err)
	}
	if names := strings.TrimSpace(string(output)); names != "" {
		return fmt.Errorf("gofmt required:\n%s", names)
	}
	return nil
}
