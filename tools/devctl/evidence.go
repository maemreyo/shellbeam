package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
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
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() && (rel == ".git" || rel == ".build" || rel == "dist") {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			paths = append(paths, rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			return "", err
		}
		h.Write([]byte(p))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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
	return strings.HasPrefix(p, ".git/") || strings.HasPrefix(p, ".build/") || strings.HasPrefix(p, "dist/")
}
