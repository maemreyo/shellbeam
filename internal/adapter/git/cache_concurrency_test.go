package git

import (
	"context"
	"sync"
	"testing"
	"time"

	core "github.com/maemreyo/shellbeam/internal/core/workspace"
)

func TestCacheConcurrentIdenticalObservationsSingleflight(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runner := &coalescingRunner{output: cleanStatusOutput(), started: started, release: release}
	adapter := newRepository(runner, SnapshotOptions{TTL: time.Second, Budget: time.Second, Now: time.Now})
	workspace := cacheWorkspace()

	const callers = 16
	results := make(chan core.FastSnapshot, callers)
	for i := 0; i < callers; i++ {
		go func() { results <- adapter.Snapshot(context.Background(), workspace) }()
	}
	<-started
	time.Sleep(20 * time.Millisecond)
	close(release)
	for i := 0; i < callers; i++ {
		got := <-results
		if got.Generation == "" || (got.Quality != core.QualityFresh && got.Quality != core.QualityCached) {
			t.Fatalf("snapshot=%#v", got)
		}
	}
	if runner.CallCount() != 1 {
		t.Fatalf("coalesced calls=%d want 1", runner.CallCount())
	}
}

type coalescingRunner struct {
	mu      sync.Mutex
	calls   int
	output  []byte
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *coalescingRunner) Run(ctx context.Context, _ ...string) ([]byte, []byte, error) {
	r.mu.Lock()
	r.calls++
	output := append([]byte(nil), r.output...)
	r.mu.Unlock()
	r.once.Do(func() { close(r.started) })
	select {
	case <-r.release:
		return output, nil, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}
func (r *coalescingRunner) CallCount() int { r.mu.Lock(); defer r.mu.Unlock(); return r.calls }
