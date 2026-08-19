package daemon

import (
	"testing"

	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
)

func TestDelegatedExecutionPolicyUsesPersistentLikeEntitlementWithoutLegacyPersistentField(t *testing.T) {
	svc := NewService(nil, nil, Options{DefaultTimeoutMS: 600000, MaxTimeoutMS: 3600000})
	req := StartRequest{ProtocolVersion: 2, SessionMode: delegated.ModeDelegatedInteractive}
	resolved, err := svc.resolveExecutionPolicy(req)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.StdinMode != operation.StdinModeStream || resolved.TimeoutMS != 0 || req.Persistent {
		t.Fatalf("resolved=%#v request=%#v", resolved, req)
	}

	req.TimeoutMode = operation.TimeoutModeUnlimited
	resolved, err = svc.resolveExecutionPolicy(req)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.StdinMode != operation.StdinModeStream || resolved.TimeoutMS != 0 {
		t.Fatalf("explicit unlimited=%#v", resolved)
	}
}
