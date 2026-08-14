package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

func TestAgentExecutionA1ShutdownDoesNotWaitFullGraceForFinalizingSession(t *testing.T) {
	live := &liveSession{state: session.Finalizing, done: make(chan struct{})}
	service := &Service{
		options: Options{TerminationGrace: time.Second},
		live:    map[string]*liveSession{"s": live},
	}
	go func() {
		time.Sleep(25 * time.Millisecond)
		live.mu.Lock()
		live.state = session.Completed
		live.mu.Unlock()
		close(live.done)
	}()

	started := time.Now()
	if err := service.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Fatalf("shutdown waited for termination grace after finalization completed: %s", elapsed)
	}
}
