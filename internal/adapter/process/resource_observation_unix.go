//go:build linux || darwin

package process

import (
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maemreyo/shellbeam/internal/core/receipt"
)

const resourceSampleInterval = 250 * time.Millisecond

type resourceSampler struct {
	rootPID  int
	peak     atomic.Int64
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newResourceSampler(pgid int) *resourceSampler {
	s := &resourceSampler{rootPID: pgid, stop: make(chan struct{}), done: make(chan struct{})}
	// Start succeeded and Setpgid makes the root child its process-group leader,
	// so one process is an observed lower bound even if the command exits before
	// the first periodic topology sample.
	s.peak.Store(1)
	go s.run()
	return s
}

func (s *resourceSampler) run() {
	defer close(s.done)
	s.sample()
	ticker := time.NewTicker(resourceSampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.sample()
		case <-s.stop:
			return
		}
	}
}

func (s *resourceSampler) sample() {
	count, err := platformProcessTreeCount(s.rootPID)
	if err != nil || count < 1 {
		return
	}
	value := int64(count)
	for {
		current := s.peak.Load()
		if value <= current || s.peak.CompareAndSwap(current, value) {
			return
		}
	}
}

func (s *resourceSampler) Finish(state *os.ProcessState) *receipt.ResourceEvidence {
	if state == nil {
		return nil
	}
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
	unavailable := receipt.ResourceMetric{Quality: receipt.ResourceUnavailable}
	userMS, systemMS := state.UserTime().Milliseconds(), state.SystemTime().Milliseconds()
	peak := s.peak.Load()
	resources := &receipt.ResourceEvidence{
		CPUUserMS:        observedResourceMetric(receipt.ResourcePlatformReported, userMS),
		CPUSystemMS:      observedResourceMetric(receipt.ResourcePlatformReported, systemMS),
		ReadBytes:        unavailable,
		WriteBytes:       unavailable,
		ProcessCountPeak: observedResourceMetric(receipt.ResourceSampled, peak),
		MaxRSSBytes:      unavailable,
	}
	if rss, ok := platformMaxRSSBytes(state); ok {
		resources.MaxRSSBytes = observedResourceMetric(receipt.ResourcePlatformReported, rss)
	}
	return resources
}

func countProcessTree(rootPID int, parents map[int]int) int {
	if rootPID <= 0 {
		return 0
	}
	children := make(map[int][]int, len(parents))
	for pid, parent := range parents {
		children[parent] = append(children[parent], pid)
	}
	count := 0
	seen := make(map[int]struct{}, len(parents))
	stack := []int{rootPID}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		if _, exists := parents[pid]; exists || pid == rootPID {
			count++
		}
		stack = append(stack, children[pid]...)
	}
	return count
}

func observedResourceMetric(quality receipt.ResourceQuality, value int64) receipt.ResourceMetric {
	copy := value
	return receipt.ResourceMetric{Quality: quality, Value: &copy}
}
