//go:build linux || darwin

package localfs

import (
	"fmt"
	"os"
	"path/filepath"
)

type privateLayout struct {
	captureDir string
	root       string
}

func createPrivateLayout(privateRoot, captureID string) (privateLayout, error) {
	if !validCaptureID(captureID) {
		return privateLayout{}, fmt.Errorf("invalid hermetic capture identity")
	}
	captureDir := filepath.Join(privateRoot, captureID)
	root := filepath.Join(captureDir, "root")
	if err := os.Mkdir(captureDir, 0o700); err != nil {
		return privateLayout{}, err
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		_ = os.Remove(captureDir)
		return privateLayout{}, err
	}
	return privateLayout{captureDir: captureDir, root: root}, nil
}

func writePrivateFile(root, rel string, data []byte, executable bool) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	inside, err := isPathWithin(root, path)
	if err != nil || !inside || path == root {
		return fmt.Errorf("invalid hermetic private path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	mode := os.FileMode(0o444)
	if executable {
		mode = 0o555
	}
	return os.Chmod(path, mode)
}

func freezePrivateTree(root string) error {
	var dirs []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unexpected private capture entry")
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i], 0o555); err != nil {
			return err
		}
	}
	return nil
}

func discardCaptureDir(captureDir string) error {
	if captureDir == "" {
		return nil
	}
	_ = filepath.Walk(captureDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			_ = os.Chmod(path, 0o700)
		}
		return nil
	})
	return os.RemoveAll(captureDir)
}
