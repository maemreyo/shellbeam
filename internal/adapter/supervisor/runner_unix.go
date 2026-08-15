//go:build linux || darwin

package supervisor

import (
	"context"
	"fmt"
	"net"
	"time"

	app "github.com/maemreyo/shellbeam/internal/app/daemon"
)

func Run(ctx context.Context, bootstrap Bootstrap, owner app.ProcessOwner) error {
	if err := bootstrap.Validate(); err != nil {
		return err
	}
	if owner == nil {
		return fmt.Errorf("supervisor process owner missing")
	}
	layout, err := OpenPrivateState(bootstrap.RuntimeRoot, bootstrap.SessionID, bootstrap.GenerationID)
	if err != nil {
		return err
	}
	capability, err := LoadCapability(layout)
	if err != nil {
		return err
	}
	listener, err := ListenControl(layout)
	if err != nil {
		return err
	}
	defer listener.Close()

	runtime, err := NewRuntime(RuntimeOptions{
		Layout: layout, Capability: capability, Owner: owner, Spec: bootstrap.ExecutionSpec(),
		MaxOutputBytes: bootstrap.MaxOutputBytes,
		InputLimits:    InputLimits{MaxRecords: bootstrap.MaxInputRecords, MaxMetadataBytes: bootstrap.MaxInputMetadataBytes, MaxQueuedBytes: bootstrap.MaxQueuedInputBytes},
		MaxKillRecords: bootstrap.MaxKillRecords, TerminationGrace: time.Duration(bootstrap.TerminationGraceMS) * time.Millisecond,
	})
	if err != nil {
		return err
	}
	defer runtime.Close()
	server, err := NewServer(runtime, capability)
	if err != nil {
		return err
	}
	if err := runtime.Start(context.Background()); err != nil {
		return err
	}

	serveCtx, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(serveCtx, listener) }()
	terminal := make(chan error, 1)
	go func() {
		_, waitErr := runtime.WaitTerminal(context.Background())
		terminal <- waitErr
	}()

	stopServer := func() {
		cancelServe()
		_ = listener.Close()
	}
	select {
	case waitErr := <-terminal:
		stopServer()
		return waitErr
	case <-ctx.Done():
		shutdownBudget := time.Duration(bootstrap.TerminationGraceMS)*time.Millisecond + 5*time.Second
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
		shutdownErr := runtime.Shutdown(shutdownCtx)
		cancel()
		stopServer()
		return shutdownErr
	case serveFailure := <-serveErr:
		if serveFailure == nil || isClosedListenerError(serveFailure) {
			return nil
		}
		shutdownBudget := time.Duration(bootstrap.TerminationGraceMS)*time.Millisecond + 5*time.Second
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
		shutdownErr := runtime.Shutdown(shutdownCtx)
		cancel()
		stopServer()
		if shutdownErr != nil {
			return shutdownErr
		}
		return fmt.Errorf("supervisor control server failed")
	}
}

func isClosedListenerError(err error) bool {
	if err == nil {
		return false
	}
	return err == net.ErrClosed
}
