package session

import "testing"

func TestTransitionMatrix(t *testing.T) {
	allowed := map[State][]State{
		Starting: {Running, Finalizing, Abandoned}, Running: {Finalizing, Abandoned}, Finalizing: {Completed, Failed, TimedOut, Killed, Abandoned},
	}
	for from, tos := range allowed {
		for _, to := range tos {
			if !CanTransition(from, to) {
				t.Errorf("want %s -> %s", from, to)
			}
		}
	}
	for _, terminal := range []State{Completed, Failed, TimedOut, Killed, Abandoned} {
		if !terminal.Terminal() {
			t.Errorf("%s not terminal", terminal)
		}
		if CanTransition(terminal, Running) {
			t.Errorf("terminal transition")
		}
	}
}
