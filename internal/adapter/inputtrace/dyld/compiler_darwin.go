//go:build darwin

package dyld

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/oklog/ulid/v2"
)

func defaultCompilerIdentity(ctx context.Context, clangPath string) (string, error) {
	if clangPath == "" {
		return "", fmt.Errorf("compiler missing")
	}
	cmd := exec.CommandContext(ctx, clangPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	if len(output) > 8192 {
		output = output[:8192]
	}
	return clangPath + "\n" + strings.TrimSpace(string(output)), nil
}

func defaultCompile(ctx context.Context, clangPath, sourcePath, outputPath string) error {
	cmd := exec.CommandContext(ctx, clangPath, "-dynamiclib", "-O2", "-o", outputPath, sourcePath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("compile trace shim: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (p *Provider) ensureArtifact(ctx context.Context) (string, string, error) {
	p.artifactMu.Lock()
	defer p.artifactMu.Unlock()
	identity, err := p.compilerIdentity(ctx)
	if err != nil {
		return "", "", err
	}
	fingerprint, err := instrumentationFingerprint(p.source, identity)
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(artifactRoot(p.stateDir), fingerprint+".dylib")
	if err := validatePrivateRegular(path, false); err == nil {
		return path, fingerprint, nil
	} else if !os.IsNotExist(err) {
		return "", "", err
	}

	token := ulid.Make().String()
	sourcePath := filepath.Join(artifactRoot(p.stateDir), ".shim-"+token+".c")
	tempOutput := filepath.Join(artifactRoot(p.stateDir), ".shim-"+token+".dylib")
	if err := os.WriteFile(sourcePath, []byte(p.source), 0600); err != nil {
		return "", "", err
	}
	_ = os.Chmod(sourcePath, 0600)
	defer os.Remove(sourcePath)
	defer os.Remove(tempOutput)
	if err := p.compile(ctx, p.clangPath, sourcePath, tempOutput); err != nil {
		return "", "", err
	}
	if err := os.Chmod(tempOutput, 0600); err != nil {
		return "", "", err
	}
	file, err := os.OpenFile(tempOutput, os.O_RDONLY, 0)
	if err != nil {
		return "", "", err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return "", "", err
	}
	if err := file.Close(); err != nil {
		return "", "", err
	}
	if err := validatePrivateRegular(tempOutput, false); err != nil {
		return "", "", err
	}
	if err := os.Rename(tempOutput, path); err != nil {
		if validatePrivateRegular(path, false) == nil {
			return path, fingerprint, nil
		}
		return "", "", err
	}
	if err := validatePrivateRegular(path, false); err != nil {
		return "", "", err
	}
	return path, fingerprint, nil
}

func instrumentationFingerprint(source, compilerIdentity string) (string, error) {
	encoded, err := json.Marshal(struct {
		Provider          string `json:"provider"`
		ProviderVersion   int    `json:"provider_version"`
		CapabilityVersion int    `json:"capability_version"`
		WireProtocol      int    `json:"wire_protocol"`
		Source            string `json:"source"`
		CompilerIdentity  string `json:"compiler_identity"`
	}{"dyld-interpose", 1, 1, wireProtocolVersion, source, compilerIdentity})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
