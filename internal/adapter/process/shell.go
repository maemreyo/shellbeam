package process

import (
	"fmt"
	"os"
	"path/filepath"
)

func ResolveShell(configured, inherited string) (string, error) {
	p := configured
	if p == "" {
		p = inherited
	}
	if p == "" {
		p = "/bin/sh"
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("shell must be absolute")
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0111 == 0 {
		return "", fmt.Errorf("shell is not executable")
	}
	return p, nil
}
