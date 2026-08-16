//go:build linux || darwin

package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/operation"
	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestDialAttachmentAuthenticatesExactIdentityAndProxiesStatusIO(t *testing.T) {
	layout, capability := shortRuntimePrivateState(t, "client-main", "generation-client")
	owner := newRuntimeFakeOwner()
	runtime := mustRuntime(t, layout, capability, owner, operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "cat", CWD: "/tmp"}, 64)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	listener, err := ListenControl(layout)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(runtime, capability)
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(serveCtx, listener) }()
	defer func() {
		cancelServe()
		_ = listener.Close()
		_ = runtime.Close()
		<-serveDone
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client, status, err := DialAttachment(ctx, layout, capability, "client-main", "generation-client")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if status.State != session.Running || status.PID != 4242 || client.PID() != 4242 || status.SessionID != "client-main" || status.GenerationID != "generation-client" {
		t.Fatalf("status=%#v pid=%d", status, client.PID())
	}
	if err := client.Write([]byte("abc")); err != nil || owner.handle.WriteCount() != 1 {
		t.Fatalf("write count=%d err=%v", owner.handle.WriteCount(), err)
	}
	if err := client.CloseStdin(); err != nil || owner.handle.CloseStdinCount() != 1 {
		t.Fatalf("close stdin count=%d err=%v", owner.handle.CloseStdinCount(), err)
	}
	if err := owner.Emit([]byte("out")); err != nil {
		t.Fatal(err)
	}
	data, next, extent, err := client.ReadOutput(context.Background(), 0, 16)
	if err != nil || string(data) != "out" || next != 3 || extent != 3 {
		t.Fatalf("output=%q next=%d extent=%d err=%v", data, next, extent, err)
	}
	owner.handle.FinishCode(0)
	exit := client.Wait(context.Background())
	if !exit.Reaped || exit.Code == nil || *exit.Code != 0 {
		t.Fatalf("exit=%#v", exit)
	}
}

func TestDialAttachmentRejectsWrongCapabilitySessionOrGeneration(t *testing.T) {
	layout, capability := shortRuntimePrivateState(t, "client-auth", "generation-client-auth")
	owner := newRuntimeFakeOwner()
	runtime := mustRuntime(t, layout, capability, owner, operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "sleep 10", CWD: "/tmp"}, 64)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	listener, err := ListenControl(layout)
	if err != nil {
		t.Fatal(err)
	}
	server, _ := NewServer(runtime, capability)
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(serveCtx, listener) }()
	defer func() {
		owner.handle.FinishCode(0)
		_, _ = runtime.WaitTerminal(context.Background())
		cancelServe()
		_ = listener.Close()
		_ = runtime.Close()
		<-serveDone
	}()

	other, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		capability Capability
		sessionID  string
		generation string
	}{
		{"capability", other, "client-auth", "generation-client-auth"},
		{"session", capability, "client-other", "generation-client-auth"},
		{"generation", capability, "client-auth", "generation-other"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if client, _, err := DialAttachment(ctx, layout, tc.capability, tc.sessionID, tc.generation); err == nil {
				_ = client.Close()
				t.Fatal("invalid attachment proof accepted")
			}
		})
	}
}

func shortRuntimePrivateState(t *testing.T, sessionID, generation string) (Layout, Capability) {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "s-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	capability, err := NewCapability()
	if err != nil {
		t.Fatal(err)
	}
	layout, err := PreparePrivateState(filepath.Join(base, "runtime"), sessionID, generation, capability)
	if err != nil {
		t.Fatal(err)
	}
	return layout, capability
}

func TestAttachmentExplicitControlAckWaitAndVerifiedTerminal(t *testing.T) {
	layout, capability := shortRuntimePrivateState(t, "client-control", "generation-control")
	owner := newRuntimeFakeOwner()
	runtime := mustRuntime(t, layout, capability, owner, operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "cat", CWD: "/tmp"}, 64)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	listener, err := ListenControl(layout)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(runtime, capability)
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(serveCtx, listener) }()
	defer func() {
		cancelServe()
		_ = listener.Close()
		_ = runtime.Close()
		<-serveDone
	}()

	client, initial, err := DialAttachment(context.Background(), layout, capability, "client-control", "generation-control")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	first, err := client.WriteInput(context.Background(), 0, []byte("abc"), false)
	if err != nil || first.NextOffset != 3 || first.Duplicate || owner.handle.WriteCount() != 1 {
		t.Fatalf("first=%#v writes=%d err=%v", first, owner.handle.WriteCount(), err)
	}
	replay, err := client.WriteInput(context.Background(), 0, []byte("abc"), false)
	if err != nil || !replay.Duplicate || owner.handle.WriteCount() != 1 {
		t.Fatalf("replay=%#v writes=%d err=%v", replay, owner.handle.WriteCount(), err)
	}
	changed, err := client.WaitStatus(context.Background(), initial.Change, 1000)
	if err != nil || changed.Change <= initial.Change || changed.InputAcceptedBytes != 3 || changed.InputDeliveredBytes != 3 {
		t.Fatalf("changed=%#v initial=%#v err=%v", changed, initial, err)
	}
	if err := owner.Emit([]byte("out")); err != nil {
		t.Fatal(err)
	}
	if err := client.AcknowledgeOutput(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	status, err := client.Status(context.Background())
	if err != nil || status.OutputAcknowledged != 3 {
		t.Fatalf("status=%#v err=%v", status, err)
	}
	firstKill, err := client.SignalWithID(context.Background(), "caller-kill-1", "TERM")
	if err != nil || !firstKill.Attempted || !firstKill.Succeeded || owner.handle.SignalCount("TERM") != 1 {
		t.Fatalf("kill=%#v signals=%d err=%v", firstKill, owner.handle.SignalCount("TERM"), err)
	}
	replayKill, err := client.SignalWithID(context.Background(), "caller-kill-1", "TERM")
	if err != nil || !replayKill.Attempted || owner.handle.SignalCount("TERM") != 1 {
		t.Fatalf("kill replay=%#v signals=%d err=%v", replayKill, owner.handle.SignalCount("TERM"), err)
	}
	owner.handle.FinishSignal("terminated")
	if exit := client.Wait(context.Background()); !exit.Reaped {
		t.Fatalf("exit=%#v", exit)
	}
	terminal, err := client.Terminal(context.Background())
	if err != nil || terminal.SessionID != "client-control" || terminal.GenerationID != "generation-control" || terminal.State != session.Killed || terminal.OutputBytes != 3 || !terminal.OutputComplete {
		t.Fatalf("terminal=%#v err=%v", terminal, err)
	}
}

func TestAttachmentRecoversAndAcknowledgesOutputOfflineThenCleansPrivateState(t *testing.T) {
	layout, capability := shortRuntimePrivateState(t, "client-offline", "generation-offline")
	owner := newRuntimeFakeOwner()
	runtime := mustRuntime(t, layout, capability, owner, operation.ExecutionSpec{Mode: operation.ExecutionModeShell, Shell: "/bin/sh", Executable: "/bin/sh", Command: "printf out", CWD: "/tmp"}, 64)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	listener, err := ListenControl(layout)
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(runtime, capability)
	if err != nil {
		t.Fatal(err)
	}
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(serveCtx, listener) }()
	client, _, err := DialAttachment(context.Background(), layout, capability, "client-offline", "generation-offline")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := owner.Emit([]byte("offline-output")); err != nil {
		t.Fatal(err)
	}
	owner.handle.FinishCode(0)
	if _, err := runtime.WaitTerminal(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancelServe()
	_ = listener.Close()
	<-serveDone

	ackBefore, recoveryExtent, err := client.RecoveryState(context.Background())
	if err != nil || ackBefore != 0 || recoveryExtent != int64(len("offline-output")) {
		t.Fatalf("recovery state ack=%d extent=%d err=%v", ackBefore, recoveryExtent, err)
	}
	data, next, extent, err := client.ReadOutput(context.Background(), 0, 64)
	if err != nil || string(data) != "offline-output" || next != int64(len(data)) || extent != int64(len(data)) {
		t.Fatalf("offline output=%q next=%d extent=%d err=%v", data, next, extent, err)
	}
	if err := client.AcknowledgeOutput(context.Background(), extent); err != nil {
		t.Fatalf("offline ack err=%v", err)
	}
	spool, err := OpenSpool(layout, extent)
	if err != nil {
		t.Fatal(err)
	}
	if ack := spool.Acknowledged(); ack != extent {
		_ = spool.Close()
		t.Fatalf("offline ack=%d want=%d", ack, extent)
	}
	_ = spool.Close()
	if err := client.Cleanup(context.Background()); err != nil {
		t.Fatalf("cleanup err=%v", err)
	}
	if _, err := os.Lstat(layout.SessionDir); !os.IsNotExist(err) {
		t.Fatalf("private session state remains after cleanup: %v", err)
	}
}
