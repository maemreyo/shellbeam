package hermetic

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

// PrepareExecutionRequest is the provider-private handoff. Target remains the
// canonical child contract; provider wrapper mechanics live only in the
// PreparedExecution returned by ExecutionProvider.
type PrepareExecutionRequest struct {
	Request    core.Request
	Capture    CapturedView
	LogicalCWD string
	Target     operation.ExecutionSpec
}

type ProviderCommand struct {
	Executable     string
	Argv           []string
	Dir            string
	Env            []string
	StdinMode      operation.StdinMode
	ResourceLimits *operation.ResourceLimits
	// StatusFD is a provider-private inherited descriptor used only for
	// setup/continuity proof. V1 reserves fd 3 for this channel.
	StatusFD int
}

func (c ProviderCommand) Clone() ProviderCommand {
	out := c
	out.Argv = append([]string(nil), c.Argv...)
	if c.Env != nil {
		out.Env = append([]string{}, c.Env...)
	}
	out.ResourceLimits = c.ResourceLimits.Clone()
	return out
}

func (c ProviderCommand) ValidatePrivate() error {
	if !cleanAbsolute(c.Executable) || !cleanAbsolute(c.Dir) || len(c.Argv) == 0 || c.Argv[0] != c.Executable {
		return fmt.Errorf("invalid hermetic provider command")
	}
	if c.Env == nil || len(c.Env) != 0 {
		return fmt.Errorf("hermetic provider command must clear ambient environment")
	}
	if c.StdinMode != operation.StdinModeClosed {
		return fmt.Errorf("hermetic provider command requires closed stdin")
	}
	if c.StatusFD != 3 {
		return fmt.Errorf("hermetic provider command requires status fd 3")
	}
	for _, value := range c.Argv {
		if value == "" || strings.IndexByte(value, 0) >= 0 {
			return fmt.Errorf("invalid hermetic provider argument")
		}
	}
	if c.ResourceLimits != nil {
		if err := c.ResourceLimits.Validate(); err != nil {
			return err
		}
	}
	return nil
}

type PreparedExecution struct {
	BoundaryID            string
	Provider              core.ProviderIdentity
	Toolchain             core.ToolchainIdentity
	CaptureManifestSHA256 string
	CaptureContentSHA256  string
	Command               ProviderCommand
	PrivateStateRoot      string
	ScratchRoot           string
}

func (p PreparedExecution) ValidatePrivate() error {
	boundary := core.BoundaryResult{
		SchemaVersion: core.BoundaryResultSchemaV1,
		BoundaryID:    p.BoundaryID,
		Provider:      p.Provider,
		Toolchain:     p.Toolchain,
		Continuity:    core.ContinuityLost,
	}
	if err := boundary.Validate(); err != nil {
		return err
	}
	if !validSHA256(p.CaptureManifestSHA256) || !validSHA256(p.CaptureContentSHA256) {
		return fmt.Errorf("invalid hermetic capture digest")
	}
	if err := p.Command.ValidatePrivate(); err != nil {
		return err
	}
	if !cleanAbsolute(p.PrivateStateRoot) || !cleanAbsolute(p.ScratchRoot) {
		return fmt.Errorf("invalid hermetic private execution path")
	}
	inside, err := filepath.Rel(p.PrivateStateRoot, p.ScratchRoot)
	if err != nil || filepath.IsAbs(inside) || inside == "." || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return fmt.Errorf("hermetic scratch root is outside private execution state")
	}
	return nil
}

type ExecutionProvider interface {
	Prepare(context.Context, PrepareExecutionRequest) (PreparedExecution, error)
	Discard(context.Context, PreparedExecution) error
}

func cleanAbsolute(value string) bool {
	return value != "" && filepath.IsAbs(value) && filepath.Clean(value) == value
}

func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
