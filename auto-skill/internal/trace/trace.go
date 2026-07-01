package trace

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// Logger writes elapsed trace lines for long-running command phases.
type Logger struct {
	w     io.Writer
	start time.Time
	mu    sync.Mutex
}

// New returns a Logger that writes to w. A nil writer disables logging.
func New(w io.Writer) *Logger {
	if w == nil {
		return nil
	}
	return &Logger{w: w, start: time.Now()}
}

// Logf writes one trace line when l is enabled.
func Logf(l *Logger, format string, args ...any) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	elapsed := time.Since(l.start).Round(time.Millisecond)
	fmt.Fprintf(l.w, "[trace +%s] %s\n", elapsed, fmt.Sprintf(format, args...))
}

// Spanf logs a start line and returns a function that logs a matching done line.
func Spanf(l *Logger, format string, args ...any) func(string, ...any) {
	if l == nil {
		return func(string, ...any) {}
	}
	msg := fmt.Sprintf(format, args...)
	started := time.Now()
	Logf(l, "%s start", msg)
	var once sync.Once
	return func(doneFormat string, doneArgs ...any) {
		once.Do(func() {
			suffix := ""
			if doneFormat != "" {
				suffix = " " + fmt.Sprintf(doneFormat, doneArgs...)
			}
			Logf(l, "%s done in %s%s", msg, time.Since(started).Round(time.Millisecond), suffix)
		})
	}
}
