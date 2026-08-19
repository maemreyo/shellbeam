package daemon_test

import (
	"context"
	"testing"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
	delegatedapp "github.com/maemreyo/shellbeam/internal/app/delegatedsession"
	delegated "github.com/maemreyo/shellbeam/internal/core/delegatedsession"
	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/receipt"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestDelegatedLifecycleReceiptNeverSchedulesOrdinaryEvidenceForVerificationIntent(t *testing.T) {
	for _, kind := range []operation.IntentKind{operation.IntentKindTest, operation.IntentKindBuild} {
		t.Run(string(kind), func(t *testing.T) {
			st := openDelegatedStartStore(t)
			runtime := newDelegatedStartRuntime()
			worker := &recordingEvidenceWorker{store: st}
			svc := app.NewService(st, &fakeOwner{}, app.Options{
				Incarnation: "d", Shell: "/bin/sh", MaxQueuedInputBytes: 100,
				DelegatedRuntime: runtime, EvidenceWorker: worker,
			})
			req := delegatedStartRequest()
			req.OperationID = "delegated-evidence-" + string(kind)
			req.Intent = &operation.DeclaredIntent{Kind: kind}
			started, err := svc.Start(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			zero := 0
			runtime.waitCh <- delegatedWaitResult{observation: delegatedapp.Observation{
				Provider: runtime.Identity(), ProviderCurrent: true, ProviderGeneration: "gen_test",
				Owner: delegated.OwnerNone, Terminal: true, ExitCode: &zero, OutputBytes: int64(len("delegated-ready\n")),
			}}
			terminal := waitForTerminal(t, svc, started.SessionID)
			if terminal.State != session.Completed || terminal.Outcome != session.Success || terminal.Receipt == nil {
				t.Fatalf("terminal=%#v", terminal)
			}
			if worker.count() != 0 {
				t.Fatalf("delegated lifecycle scheduled ordinary evidence: %d", worker.count())
			}
			rec := terminal.Receipt
			if rec.SchemaVersion != 5 || rec.EvidenceAuthority != receipt.EvidenceAuthoritySessionLifecycleOnly || rec.CaptureQuality != receipt.CaptureComplete || !rec.OutputComplete {
				t.Fatalf("delegated receipt=%#v", rec)
			}
			stored, found, err := st.FindOperation(context.Background(), operation.ID(req.OperationID))
			if err != nil || !found {
				t.Fatalf("reservation found=%v err=%v", found, err)
			}
			if stored.SchemaVersion != 5 || stored.SessionMode != delegated.ModeDelegatedInteractive || stored.EvidenceEligible() {
				t.Fatalf("reservation ordinary evidence eligibility leaked: %#v", stored)
			}
		})
	}
}
