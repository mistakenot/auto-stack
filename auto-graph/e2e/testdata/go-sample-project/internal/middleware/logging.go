package middleware

import (
	"net/http"
	"time"

	"example.com/sample/pkg/logger"
)

// Logging wraps handlers with request/response logging.
type Logging struct {
	log *logger.Logger
}

// NewLogging creates a logging middleware.
func NewLogging(log *logger.Logger) *Logging {
	return &Logging{log: log}
}

// Wrap returns an http.Handler that logs request duration.
func (m *Logging) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		m.log.Info(r.Method + " " + r.URL.Path + " " + time.Since(start).String())
	})
}
