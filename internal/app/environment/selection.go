package environment

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/environment"
	project "github.com/maemreyo/shellbeam/internal/core/project"
)

func selectedPresenceNames(extra []string) ([]string, error) {
	values := append(append([]string(nil), builtInPresenceNames...), extra...)
	if len(values) > core.MaxRelevantVariables+len(builtInPresenceNames) {
		return nil, fmt.Errorf("too many relevant environment variables")
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			return nil, fmt.Errorf("empty environment variable name")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) > core.MaxRelevantVariables {
		return nil, fmt.Errorf("too many relevant environment variables")
	}
	sort.Strings(out)
	return out, nil
}

func selectedToolchains(declared map[string]project.Toolchain) []ToolchainRequest {
	ids := core.SupportedToolchains()
	out := make([]ToolchainRequest, 0, len(ids))
	for _, id := range ids {
		declaration := declared[id]
		out = append(out, ToolchainRequest{Kind: id, RequestedIdentity: requestedIdentity(declaration), Declaration: declaration})
	}
	return out
}

func requestedIdentity(declaration project.Toolchain) string {
	parts := make([]string, 0, 3)
	if declaration.Version != "" {
		parts = append(parts, "version="+declaration.Version)
	}
	if declaration.VersionSource != "" {
		parts = append(parts, "source="+declaration.VersionSource)
	}
	if declaration.Manager != "" {
		parts = append(parts, "manager="+declaration.Manager)
	}
	if len(parts) == 0 {
		return "host"
	}
	return strings.Join(parts, ";")
}

func declaredManager(declared map[string]project.Toolchain) *core.ToolchainManager {
	values := make([]string, 0, len(declared))
	for kind, declaration := range declared {
		if declaration.Manager != "" {
			values = append(values, kind+"="+declaration.Manager)
		}
	}
	if len(values) == 0 {
		return nil
	}
	sort.Strings(values)
	return &core.ToolchainManager{Kind: "declared", Identity: strings.Join(values, ",")}
}

func snapshotCacheKey(workspaceID, manifestDigest string, execution core.ExecutionContext, names []string, toolchains []ToolchainRequest) (string, error) {
	encoded, err := json.Marshal(struct {
		WorkspaceID                 string
		ManifestDigest              string
		Execution                   core.ExecutionContext
		Names                       []string
		Toolchains                  []ToolchainRequest
		FingerprintVersion          int
		ToolchainFingerprintVersion int
	}{workspaceID, manifestDigest, execution, names, toolchains, core.FingerprintVersion, core.ToolchainFingerprintVersion})
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

func cachedBindingKey(request BindingRequest) (string, error) {
	if err := validateExecution(request.Execution); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(struct {
		WorkspaceID                 string
		ManifestDigest              string
		Execution                   core.ExecutionContext
		FingerprintVersion          int
		ToolchainFingerprintVersion int
	}{request.WorkspaceID, request.ManifestDigest, request.Execution, core.FingerprintVersion, core.ToolchainFingerprintVersion})
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

func validateExecution(execution core.ExecutionContext) error {
	if execution.Identity == "" || execution.Mode != "shell" && execution.Mode != "argv" {
		return fmt.Errorf("invalid execution context")
	}
	return nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneManager(manager *core.ToolchainManager) *core.ToolchainManager {
	if manager == nil {
		return nil
	}
	copy := *manager
	return &copy
}
