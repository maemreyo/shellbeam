package session

import "fmt"

type KillAttempt struct {
	ID        string
	Signal    string
	Attempted bool
	Succeeded bool
	Needed    bool
}
type KillLedger struct{ attempts map[string]KillAttempt }

func NewKillLedger() *KillLedger { return &KillLedger{attempts: map[string]KillAttempt{}} }
func (l *KillLedger) Admit(id, signal string, terminal bool) (KillAttempt, bool, error) {
	if id == "" {
		return KillAttempt{}, false, fmt.Errorf("invalid kill_id")
	}
	if signal != "INT" && signal != "TERM" && signal != "KILL" {
		return KillAttempt{}, false, fmt.Errorf("invalid signal")
	}
	if old, ok := l.attempts[id]; ok {
		if old.Signal != signal {
			return KillAttempt{}, false, fmt.Errorf("kill_conflict")
		}
		return old, false, nil
	}
	a := KillAttempt{ID: id, Signal: signal, Needed: !terminal}
	l.attempts[id] = a
	return a, !terminal, nil
}
func (l *KillLedger) Record(a KillAttempt) { l.attempts[a.ID] = a }
