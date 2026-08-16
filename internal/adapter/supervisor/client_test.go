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
