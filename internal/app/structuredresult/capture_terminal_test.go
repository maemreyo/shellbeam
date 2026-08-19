package structuredresult

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTerminalSourceHandle struct {
	closed atomic.Int32
	read   []byte
	id     ArtifactSourceIdentity
}

func (h *fakeTerminalSourceHandle) Read(p []byte) (int, error) {
	if len(h.read) == 0 {
		return 0, io.EOF
	}
	n := copy(p, h.read)
	h.read = h.read[n:]
	return n, nil
}
func (h *fakeTerminalSourceHandle) StatIdentity() (ArtifactSourceIdentity, error) { return h.id, nil }
func (h *fakeTerminalSourceHandle) Close() error {
	h.closed.Add(1)
	return nil
}

type fakeArtifactSourceOpener struct {
	mu      sync.Mutex
	order   *[]string
	entered chan struct{}
	release chan struct{}
	handle  *fakeTerminalSourceHandle
	err     error
	calls   int
}

func (o *fakeArtifactSourceOpener) OpenArtifactSource(ctx context.Context, captureAuthorityID string, maxBlobBytes int64) (ArtifactSourceHandle, ArtifactSourceIdentity, error) {
	o.mu.Lock()
	o.calls++
	if o.order != nil {
		*o.order = append(*o.order, "open")
	}
	o.mu.Unlock()
	if o.entered != nil {
		select {
		case o.entered <- struct{}{}:
		default:
		}
	}
	if o.release != nil {
		<-o.release // deliberately ignores ctx: models a syscall/helper returning late
	}
	if o.err != nil {
		return nil, ArtifactSourceIdentity{}, o.err
	}
	if o.handle == nil {
		o.handle = &fakeTerminalSourceHandle{id: ArtifactSourceIdentity{Scheme: ArtifactSourceIdentityUnixV1, Digest: strings.Repeat("a", 64), Size: 4}}
	}
	return o.handle, o.handle.id, nil
}

type fakeBlobBudgetCapability struct{ releases atomic.Int32 }

func (c *fakeBlobBudgetCapability) Release() error { c.releases.Add(1); return nil }

type fakeBlobBudgetProvider struct {
	order      *[]string
	capability *fakeBlobBudgetCapability
	err        error
	calls      atomic.Int32
}

func (p *fakeBlobBudgetProvider) AcquireBlobBudgetCapability(context.Context, string, int64) (BlobBudgetCapability, error) {
	p.calls.Add(1)
	if p.order != nil {
		*p.order = append(*p.order, "budget")
	}
	if p.err != nil {
		return nil, p.err
	}
	if p.capability == nil {
		p.capability = &fakeBlobBudgetCapability{}
	}
	return p.capability, nil
}

func TestTerminalCaptureReservesFiniteResourcesBeforeFinalOpen(t *testing.T) {
	var order []string
	opener := &fakeArtifactSourceOpener{order: &order}
	budget := &fakeBlobBudgetProvider{order: &order}
	acquirer := newTerminalCaptureAcquirerWithHooks(TerminalCaptureLimits{
		AcquisitionConcurrency: 1, PinnedHandles: 1, MaterializationQueueDepth: 1, MaxAcquireDuration: 250 * time.Millisecond,
	}, budget, terminalCaptureHooks{onAcquire: func(stage string) { order = append(order, stage) }})
	result := acquirer.Acquire(context.Background(), TerminalCaptureRequest{
		CaptureAuthorityID: strings.Repeat("b", 64), MaxBlobBytes: DefaultMaxArtifactBlobBytes, Opener: opener,
	})
	if result.State != TerminalCaptureAcquired || result.Source() == nil || result.SourceIdentity.Size != 4 {
		t.Fatalf("result=%#v", result)
	}
	if got := strings.Join(order, ","); got != "worker,pinned,queue,budget,open" {
		t.Fatalf("resource order=%s", got)
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	if opener.handle.closed.Load() != 1 || budget.capability.releases.Load() != 1 {
		t.Fatalf("cleanup source=%d budget=%d", opener.handle.closed.Load(), budget.capability.releases.Load())
	}
	if acquirer.activePinned() != 0 || acquirer.activeQueued() != 0 || acquirer.activeWorkers() != 0 {
		t.Fatalf("resource leak workers=%d pinned=%d queue=%d", acquirer.activeWorkers(), acquirer.activePinned(), acquirer.activeQueued())
	}
}

func TestTerminalCaptureDoesNotOpenWhenAnyPreOpenReservationFails(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*TerminalCaptureAcquirer, *fakeBlobBudgetProvider)
	}{
		{"worker", func(a *TerminalCaptureAcquirer, _ *fakeBlobBudgetProvider) { a.workerSlots <- struct{}{} }},
		{"pinned", func(a *TerminalCaptureAcquirer, _ *fakeBlobBudgetProvider) { a.pinnedSlots <- struct{}{} }},
		{"queue", func(a *TerminalCaptureAcquirer, _ *fakeBlobBudgetProvider) { a.queueSlots <- struct{}{} }},
		{"budget", func(_ *TerminalCaptureAcquirer, b *fakeBlobBudgetProvider) { b.err = ErrArtifactSourceBudgetExceeded }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opener := &fakeArtifactSourceOpener{}
			budget := &fakeBlobBudgetProvider{}
			acquirer := NewTerminalCaptureAcquirer(TerminalCaptureLimits{
				AcquisitionConcurrency: 1, PinnedHandles: 1, MaterializationQueueDepth: 1, MaxAcquireDuration: 15 * time.Millisecond,
			}, budget)
			tc.setup(acquirer, budget)
			result := acquirer.Acquire(context.Background(), TerminalCaptureRequest{CaptureAuthorityID: strings.Repeat("c", 64), MaxBlobBytes: DefaultMaxArtifactBlobBytes, Opener: opener})
			if result.State != TerminalCaptureUnavailable && result.State != TerminalCaptureBudgetExceeded {
				t.Fatalf("result=%#v", result)
			}
			opener.mu.Lock()
			calls := opener.calls
			opener.mu.Unlock()
			if calls != 0 {
				t.Fatalf("final open happened after %s reservation failure: %d", tc.name, calls)
			}
			// Release any test-owned blocking token.
			select {
			case <-acquirer.workerSlots:
			default:
			}
			select {
			case <-acquirer.pinnedSlots:
			default:
			}
			select {
			case <-acquirer.queueSlots:
			default:
			}
		})
	}
}

func TestTerminalCaptureDeadlineDoesNotResurrectLateSuccess(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	handle := &fakeTerminalSourceHandle{id: ArtifactSourceIdentity{Scheme: ArtifactSourceIdentityUnixV1, Digest: strings.Repeat("d", 64), Size: 8}}
	opener := &fakeArtifactSourceOpener{entered: entered, release: release, handle: handle}
	budget := &fakeBlobBudgetProvider{}
	acquirer := NewTerminalCaptureAcquirer(TerminalCaptureLimits{
		AcquisitionConcurrency: 1, PinnedHandles: 1, MaterializationQueueDepth: 1, MaxAcquireDuration: 25 * time.Millisecond,
	}, budget)
	start := time.Now()
	result := acquirer.Acquire(context.Background(), TerminalCaptureRequest{CaptureAuthorityID: strings.Repeat("e", 64), MaxBlobBytes: DefaultMaxArtifactBlobBytes, Opener: opener})
	elapsed := time.Since(start)
	if result.State != TerminalCaptureUnavailable || result.Source() != nil {
		t.Fatalf("deadline result=%#v", result)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("terminal acquisition blocked %s", elapsed)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("late helper never entered opener")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for handle.closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if handle.closed.Load() != 1 || budget.capability.releases.Load() != 1 {
		t.Fatalf("late success cleanup source=%d budget=%d", handle.closed.Load(), budget.capability.releases.Load())
	}
	if result.State != TerminalCaptureUnavailable || result.Source() != nil {
		t.Fatalf("late helper resurrected result=%#v", result)
	}
	if acquirer.activePinned() != 0 || acquirer.activeQueued() != 0 {
		t.Fatalf("late helper leaked resources pinned=%d queue=%d", acquirer.activePinned(), acquirer.activeQueued())
	}
}

func TestTerminalCaptureMapsSourceFailureTaxonomyAndOwnsNoHandle(t *testing.T) {
	cases := []struct {
		err  error
		want TerminalCaptureState
	}{
		{ErrArtifactSourceMissing, TerminalCaptureMissing},
		{ErrArtifactSourceKindMismatch, TerminalCaptureKindMismatch},
		{ErrArtifactSourceBudgetExceeded, TerminalCaptureBudgetExceeded},
		{errors.New("io unavailable"), TerminalCaptureUnavailable},
	}
	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			budget := &fakeBlobBudgetProvider{}
			acquirer := NewTerminalCaptureAcquirer(TerminalCaptureLimits{AcquisitionConcurrency: 1, PinnedHandles: 1, MaterializationQueueDepth: 1, MaxAcquireDuration: time.Second}, budget)
			result := acquirer.Acquire(context.Background(), TerminalCaptureRequest{CaptureAuthorityID: strings.Repeat("f", 64), MaxBlobBytes: DefaultMaxArtifactBlobBytes, Opener: &fakeArtifactSourceOpener{err: tc.err}})
			if result.State != tc.want || result.Source() != nil {
				t.Fatalf("result=%#v", result)
			}
			if budget.capability.releases.Load() != 1 {
				t.Fatalf("budget capability releases=%d", budget.capability.releases.Load())
			}
		})
	}
}
