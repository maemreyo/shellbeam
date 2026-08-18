package bwrap

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func toolchainManifestDigest(root string) (string, error) {
	if !cleanAbsolute(root) {
		return "", fmt.Errorf("invalid hermetic toolchain root")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || writable(info.Mode()) {
		return "", fmt.Errorf("invalid hermetic toolchain root")
	}
	required := []string{"dev", "tmp", "work", "work/input", "work/scratch"}
	for _, rel := range required {
		got, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(rel)))
		if statErr != nil || !got.IsDir() || got.Mode()&os.ModeSymlink != 0 || writable(got.Mode()) {
			return "", fmt.Errorf("invalid hermetic toolchain mount layout")
		}
	}

	lines := make([]string, 0, 128)
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			rel = ""
		}
		mode := info.Mode()
		switch {
		case mode.IsDir():
			if writable(mode) {
				return fmt.Errorf("writable hermetic toolchain directory")
			}
			lines = append(lines, fmt.Sprintf("D %04o %s\n", mode.Perm(), rel))
		case mode.IsRegular():
			if writable(mode) {
				return fmt.Errorf("writable hermetic toolchain file")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			lines = append(lines, fmt.Sprintf("F %04o %s %s\n", mode.Perm(), rel, hex.EncodeToString(sum[:])))
		case mode&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := validateToolchainLink(root, path, target); err != nil {
				return err
			}
			lines = append(lines, fmt.Sprintf("L %s -> %s\n", rel, target))
		default:
			return fmt.Errorf("unsupported hermetic toolchain entry")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "")))
	return hex.EncodeToString(sum[:]), nil
}

func runtimeManifestDigest(paths []string) (string, error) {
	unique := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if !cleanAbsolute(path) {
			return "", fmt.Errorf("invalid hermetic provider runtime path")
		}
		unique[path] = struct{}{}
	}
	ordered := make([]string, 0, len(unique))
	for path := range unique {
		ordered = append(ordered, path)
	}
	sort.Strings(ordered)
	var manifest strings.Builder
	for _, path := range ordered {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&manifest, "%s  %s\n", hex.EncodeToString(sum[:]), path)
	}
	sum := sha256.Sum256([]byte(manifest.String()))
	return hex.EncodeToString(sum[:]), nil
}

func toolchainExecutable(root, sandboxPath string) bool {
	if !cleanAbsolute(root) || !filepath.IsAbs(sandboxPath) || filepath.Clean(sandboxPath) != sandboxPath || strings.Contains(sandboxPath, "..") {
		return false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	hostPath := filepath.Join(resolvedRoot, strings.TrimPrefix(filepath.Clean(sandboxPath), string(filepath.Separator)))
	resolved, err := filepath.EvalSymlinks(hostPath)
	if err != nil {
		return false
	}
	inside, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || filepath.IsAbs(inside) || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return false
	}
	info, err := os.Stat(resolved)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 && !writable(info.Mode())
}

func validateToolchainLink(root, path, target string) error {
	if target == "" || filepath.IsAbs(target) || strings.IndexByte(target, 0) >= 0 {
		return fmt.Errorf("invalid hermetic toolchain symlink")
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(path), target))
	rel, err := filepath.Rel(root, resolved)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("escaping hermetic toolchain symlink")
	}
	return nil
}

func writable(mode os.FileMode) bool { return mode.Perm()&0o222 != 0 }

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
