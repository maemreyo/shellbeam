package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Evidence struct {
	SchemaVersion     int       `json:"schema_version"`
	Command           string    `json:"command"`
	Base              string    `json:"base"`
	SourceFingerprint string    `json:"source_fingerprint"`
	ChangedFiles      []string  `json:"changed_files"`
	SelectedPackages  []string  `json:"selected_packages"`
	Status            string    `json:"status"`
	ExitCode          int       `json:"exit_code"`
	Error             string    `json:"error,omitempty"`
	StartedAt         time.Time `json:"started_at"`
	FinishedAt        time.Time `json:"finished_at"`
}

func sourceFingerprint(root string) (string, error) {
	paths, err := sourcePaths(root)
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write([]byte{0})
		if err := hashSourcePath(h, root, p); err != nil {
			return "", err
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sourcePaths(root string) ([]string, error) {
	if _, err := os.Lstat(filepath.Join(root, ".git")); err == nil {
		return gitSourcePaths(root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return walkedSourcePaths(root)
}

func gitSourcePaths(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-co", "--exclude-standard", "-z")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" && !isIgnoredPath(filepath.ToSlash(p)) {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

func walkedSourcePaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		ignored := isIgnoredPath(filepath.ToSlash(rel))
		if d.IsDir() && ignored {
			return filepath.SkipDir
		}
		if !d.IsDir() && !ignored {
			paths = append(paths, rel)
		}
		return nil
	})
	return paths, err
}

type sourceHash interface {
	Write([]byte) (int, error)
}

func hashSourcePath(h sourceHash, root, path string) error {
	full := filepath.Join(root, path)
	info, err := os.Lstat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			_, _ = h.Write([]byte("missing"))
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(full)
		if err != nil {
			return err
		}
		_, _ = h.Write([]byte("symlink\x00" + target))
		return nil
	}
	if !info.Mode().IsRegular() {
		_, _ = h.Write([]byte("special\x00" + info.Mode().String()))
		return nil
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return err
	}
	_, _ = h.Write(b)
	return nil
}

func writeEvidence(e Evidence) (string, error) {
	if err := os.MkdirAll(".build/receipts", 0700); err != nil {
		return "", err
	}
	name := e.StartedAt.Format("20060102T150405.000000000Z") + "-" + e.Command + ".json"
	path := filepath.Join(".build/receipts", name)
	b, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(b, '\n'), 0600)
}

func isIgnoredPath(p string) bool {
	return p == ".git" || strings.HasPrefix(p, ".git/") ||
		p == ".build" || strings.HasPrefix(p, ".build/") ||
		p == "dist" || strings.HasPrefix(p, "dist/")
}
