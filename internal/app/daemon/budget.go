package daemon

import (
	"fmt"
	"sync"
)

type Budget struct {
	mu          sync.Mutex
	sessions    int
	output      int64
	maxSessions int
	maxOutput   int64
	control     int64
}

func NewBudget(maxSessions int, maxOutput, control int64) *Budget {
	return &Budget{maxSessions: maxSessions, maxOutput: maxOutput, control: control}
}
func (b *Budget) AcquireStart() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions >= b.maxSessions {
		return fmt.Errorf("capacity_exceeded")
	}
	if b.output+b.control > b.maxOutput {
		return fmt.Errorf("persistence_unavailable")
	}
	b.sessions++
	b.output += b.control
	return nil
}
func (b *Budget) ReleaseStart() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessions > 0 {
		b.sessions--
		b.output -= b.control
	}
}
func (b *Budget) AcquireOutput(n int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n < 0 || b.output+n > b.maxOutput {
		return fmt.Errorf("storage_reserve_exhausted")
	}
	b.output += n
	return nil
}
