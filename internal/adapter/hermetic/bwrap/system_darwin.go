//go:build darwin

package bwrap

import (
	"context"
	"fmt"

	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
)

type darwinQualificationOps struct{}

func defaultQualificationOps() qualificationOps { return darwinQualificationOps{} }
func (darwinQualificationOps) Qualify(context.Context, Config) (core.ProviderIdentity, core.ToolchainIdentity, error) {
	return core.ProviderIdentity{}, core.ToolchainIdentity{}, fmt.Errorf("hermetic bubblewrap provider unsupported on darwin")
}
func (darwinQualificationOps) ToolchainExecutable(string, string) bool { return false }
