package logger

import (
	"fmt"
	"io"
	"time"
)

// Logger provides structured logging.
type Logger struct {
	out io.Writer
}

// New creates a logger writing to the given writer.
func New(out io.Writer) *Logger {
	return &Logger{out: out}
}

// Info logs an informational message.
func (l *Logger) Info(msg string) {
	fmt.Fprintf(l.out, "%s [INFO] %s\n", time.Now().Format(time.RFC3339), msg)
}

// Error logs an error message.
func (l *Logger) Error(msg string) {
	fmt.Fprintf(l.out, "%s [ERROR] %s\n", time.Now().Format(time.RFC3339), msg)
}
