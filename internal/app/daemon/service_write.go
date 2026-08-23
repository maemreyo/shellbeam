package daemon

import (
	"fmt"

	"github.com/maemreyo/shellbeam/internal/core/session"
)

func (s *Service) writeLoop(l *liveSession) {
	defer close(l.writerDone)
	for job := range l.jobs {
		var err error
		if job.eof {
			err = l.handle.CloseStdin()
		} else {
			err = l.handle.Write(job.data)
		}
		l.mu.Lock()
		if err != nil {
			if l.captureErr == nil {
				l.captureErr = fmt.Errorf("input_delivery_failed")
			}
			l.handle.Signal("TERM")
			l.terminalTarget = session.Killed
		} else if !job.eof {
			l.delivered += int64(len(job.data))
			l.input.Delivered(len(job.data))
		}
		l.notify()
		l.mu.Unlock()
	}
}
