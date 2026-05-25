package middleware

import (
	"net/http"

	_ "example.com/sample/internal/handler"
	"example.com/sample/pkg/logger"
)

// Recovery middleware catches panics and logs them.
type Recovery struct {
	log *logger.Logger
}

// NewRecovery creates a recovery middleware.
func NewRecovery(log *logger.Logger) *Recovery {
	return &Recovery{log: log}
}

// Wrap returns an http.Handler that recovers from panics.
func (rc *Recovery) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				rc.log.Info("panic recovered")
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
