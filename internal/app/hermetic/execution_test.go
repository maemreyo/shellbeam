package hermetic

import (
	"context"
	"testing"

	core "github.com/maemreyo/shellbeam/internal/core/hermetic"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestProviderCommandCloneDoesNotAliasPrivateArgumentsOrLimits(t *testing.T) {
	limits := &operation.ResourceLimits{MemoryBytes: 64 << 20}
	original := ProviderCommand{
		Executable:     "/private/bwrap",
		Argv:           []string{"/private/bwrap", "--unshare-all", "--", "/bin/true"},
		Dir:            "/",
		Env:            []string{},
		StdinMode:      operation.StdinModeClosed,
		ResourceLimits: limits,
	}
	cloned := original.Clone()
	cloned.Argv[1] = "changed"
	cloned.ResourceLimits.MemoryBytes = 1
	if original.Argv[1] == "changed" || original.ResourceLimits.MemoryBytes == 1 {
		t.Fatal("provider command clone aliased private execution state")
	}
}

func TestPreparedExecutionCarriesOnlyPrivateLaunchStateAndFrozenAuthorityInputs(t *testing.T) {
	provider := core.ProviderIdentity{Provider: core.ProviderBubblewrap, Version: core.BubblewrapVersionV1, BinarySHA256: repeatHex("a"), RuntimeManifestSHA256: repeatHex("b")}
	toolchain := core.ToolchainIdentity{ID: "go-1.26.6-linux-amd64", ManifestSHA256: repeatHex("c")}
	prepared := PreparedExecution{
		BoundaryID:            "hb_01K00000000000000000000000",
		Provider:              provider,
		Toolchain:             toolchain,
		CaptureManifestSHA256: repeatHex("d"),
		Command:               ProviderCommand{Executable: "/private/bwrap", Argv: []string{"/private/bwrap", "--", "/bin/true"}, Dir: "/", Env: []string{}, StdinMode: operation.StdinModeClosed, StatusFD: 3},
		PrivateStateRoot:      "/private/hb_01K00000000000000000000000",
		ScratchRoot:           "/private/hb_01K00000000000000000000000/scratch",
	}
	if err := prepared.ValidatePrivate(); err != nil {
		t.Fatalf("valid prepared execution rejected: %v", err)
	}
	broken := prepared
	broken.Command.StdinMode = operation.StdinModeStream
	if err := broken.ValidatePrivate(); err == nil {
		t.Fatal("prepared execution accepted streaming stdin")
	}
}

func TestExecutionProviderPortKeepsProviderMechanicsOutOfCoreRequest(t *testing.T) {
	var _ ExecutionProvider = fakeExecutionProvider{}
	req := PrepareExecutionRequest{Request: core.Request{Version: 1}}
	_, _ = fakeExecutionProvider{}.Prepare(context.Background(), req)
}

type fakeExecutionProvider struct{}

func (fakeExecutionProvider) Prepare(context.Context, PrepareExecutionRequest) (PreparedExecution, error) {
	return PreparedExecution{}, nil
}
func (fakeExecutionProvider) Discard(context.Context, PreparedExecution) error { return nil }

func repeatHex(s string) string {
	out := ""
	for len(out) < 64 {
		out += s
	}
	return out[:64]
}

func TestProviderCommandRequiresReservedStatusFDThree(t *testing.T) {
	base := ProviderCommand{Executable: "/provider/bwrap", Argv: []string{"/provider/bwrap", "--", "/bin/true"}, Dir: "/", Env: []string{}, StdinMode: operation.StdinModeClosed, StatusFD: 3}
	if err := base.ValidatePrivate(); err != nil {
		t.Fatalf("fd3 rejected: %v", err)
	}
	for _, fd := range []int{0, 1, 2, 4, 99} {
		bad := base
		bad.StatusFD = fd
		if err := bad.ValidatePrivate(); err == nil {
			t.Fatalf("status fd %d accepted", fd)
		}
	}
}
