// Package observability emits stable metadata-only operator events.
package observability

import (
	"io"
	"log/slog"
)

type Logger struct{ log *slog.Logger }

func New(w io.Writer) *Logger {
	return &Logger{log: slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{}))}
}
func (l *Logger) Event(event string, attrs ...string) {
	safe := []any{"event", event}
	for i := 0; i+1 < len(attrs); i += 2 {
		key, value := attrs[i], attrs[i+1]
		switch key {
		case "command", "cwd", "environment", "stdin", "output", "credential", "token", "path", "error":
			value = "[redacted]"
		}
		safe = append(safe, key, value)
	}
	l.log.Info("shellbeam", safe...)
}
