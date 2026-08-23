package contextexec

import (
	"context"
	"testing"

	shellapp "github.com/maemreyo/shellbeam/internal/app/shellintegration"
	core "github.com/maemreyo/shellbeam/internal/core/contextexec"
)

type callbackBindingHelper struct{ callbacks RuntimeCallbacks }

func (*callbackBindingHelper) Qualified() bool { return true }
func (*callbackBindingHelper) ArmContextHelper(context.Context, HelperArmRequest) (shellapp.ContextHelperArm, error) {
	return shellapp.ContextHelperArm{}, nil
}
func (h *callbackBindingHelper) BindContextExecCallbacks(callbacks RuntimeCallbacks) {
	h.callbacks = callbacks
}

func TestNewServiceBindsAllDurableRuntimeCallbacks(t *testing.T) {
	helper := &callbackBindingHelper{}
	svc := NewService(Options{Helper: helper})
	if svc == nil {
		t.Fatal("service missing")
	}
	if helper.callbacks.BindClaim == nil || helper.callbacks.AuthorizePrepared == nil || helper.callbacks.RecordSpawn == nil || helper.callbacks.RecordTerminal == nil || helper.callbacks.CanonicalizeNoChildFailure == nil {
		t.Fatalf("callbacks=%#v", helper.callbacks)
	}
	_ = core.Request{}
}
