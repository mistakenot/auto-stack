package handler

import (
	mw "example.com/sample/internal/middleware"
	"example.com/sample/pkg/logger"
)

// WithLogging returns a handler wrapped with logging middleware.
// This creates a circular reference: handler -> middleware -> handler (via blank import).
func WithLogging(log *logger.Logger) *mw.Logging {
	return mw.NewLogging(log)
}
