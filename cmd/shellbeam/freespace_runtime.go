//go:build linux || darwin

package main

import (
	"context"
	"os"
	"strconv"
	"time"

	storeadapter "github.com/maemreyo/shellbeam/internal/adapter/store"
	"github.com/maemreyo/shellbeam/internal/observability"
)

// The disk fills while the daemon is running, not while someone is watching it.
//
// doctor answers "is there room" for whoever asks, which is the wrong shape for
// a condition that arrives hours into a session nobody is supervising. This
// watch is the same question asked on the daemon's own schedule, and like
// retention it is housekeeping: it observes and reports, and it never refuses
// work. Admission is bounded by the store's byte budget; free space is a fact
// about a shared filesystem, and a daemon that turned it into a refusal would
// be rationing a resource it does not own.
const freeSpaceSampleInterval = 5 * time.Minute

// freeSpaceWatch reports crossings of the configured floor, not the level.
type freeSpaceWatch struct {
	minimum   int64
	available func() (int64, error)
	log       *observability.Logger
	low       bool
}

func newFreeSpaceWatch(minimum int64, available func() (int64, error), log *observability.Logger) *freeSpaceWatch {
	return &freeSpaceWatch{minimum: minimum, available: available, log: log}
}

// sample takes one reading and emits only when the condition changes.
//
// Level-triggered reporting would write the same line every interval for as
// long as the disk stayed full, so the crossing -- the only part an operator
// can act on -- would be buried in repetition of itself.
func (w *freeSpaceWatch) sample() {
	available, err := w.available()
	if err != nil {
		// A probe that cannot answer is not evidence of a full disk, and this
		// is housekeeping: it reports what it knows and leaves the daemon
		// alone.
		return
	}
	low := available < w.minimum
	if low == w.low {
		return
	}
	w.low = low
	event := "free_space_recovered"
	if low {
		event = "free_space_low"
	}
	w.log.Event(event,
		"available_bytes", strconv.FormatInt(available, 10),
		"minimum_bytes", strconv.FormatInt(w.minimum, 10),
	)
}

// startFreeSpaceWatch begins reporting free space on the volume holding the
// state store. A minimum of zero or less disables it: an operator who
// configured no floor has not asked to be told about one.
func startFreeSpaceWatch(ctx context.Context, stateDir string, minimum int64) {
	if minimum <= 0 {
		return
	}
	watch := newFreeSpaceWatch(minimum,
		func() (int64, error) { return storeadapter.AvailableBytes(stateDir) },
		observability.New(os.Stderr),
	)
	go func() {
		for {
			watch.sample()
			select {
			case <-ctx.Done():
				return
			case <-time.After(freeSpaceSampleInterval):
			}
		}
	}()
}
