package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type BuildEvidence struct {
	SourceDigest  string `json:"source_digest"`
	BuildID       string `json:"build_id"`
	StagingDir    string `json:"staging_dir"`
	PublishedPath string `json:"published_path"`
	CacheMode     string `json:"cache_mode"`
}

type buildFunc func(output string) error

func runIncrementalBuild(root, sourceDigest string) (BuildEvidence, error) {
	buildID, err := newBuildID()
	if err != nil {
		return BuildEvidence{}, err
	}
	return buildPublication(root, sourceDigest, buildID, runGoBuild)
}

func buildPublication(root, sourceDigest, buildID string, build buildFunc) (BuildEvidence, error) {
	stagingDir := filepath.Join(root, ".build", "workspaces", sourceDigest, buildID)
	published := filepath.Join(root, ".build", "shellbeam")
	evidence := BuildEvidence{
		SourceDigest: sourceDigest, BuildID: buildID, StagingDir: stagingDir,
		PublishedPath: published, CacheMode: "go-native",
	}
	if err := os.MkdirAll(stagingDir, 0o700); err != nil {
		return evidence, err
	}
	staged := filepath.Join(stagingDir, "shellbeam")
	if err := build(staged); err != nil {
		return evidence, err
	}
	if err := atomicPublishFile(staged, published); err != nil {
		return evidence, err
	}
	return evidence, nil
}

func goBuildArgs(output string) []string {
	return []string{"build", "-trimpath", "-buildvcs=false", "-o", output, "./cmd/shellbeam"}
}

func runGoBuild(output string) error {
	cmd := exec.Command("go", goBuildArgs(output)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	return nil
}

func newBuildID() (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("build id: %w", err)
	}
	return hex.EncodeToString(random[:]), nil
}

func atomicPublishFile(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("build artifact is not a regular file: %s", source)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".shellbeam-publish-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := copyBuildFile(tmp, source, info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, destination); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func copyBuildFile(destination *os.File, source string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if _, err := io.Copy(destination, input); err != nil {
		return err
	}
	if err := destination.Chmod(mode); err != nil {
		return err
	}
	return destination.Sync()
}
