package middleware

import (
	"net/http"

	"example.com/sample/pkg/logger"
)

// Auth provides token-based authentication middleware.
type Auth struct {
	log   *logger.Logger
	token string
}

// NewAuth creates an auth middleware with the given token.
func NewAuth(log *logger.Logger, token string) *Auth {
	return &Auth{log: log, token: token}
}

// Wrap returns an http.Handler that checks authorization.
func (a *Auth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.Header.Get("Authorization")
		if tok != "Bearer "+a.token {
			a.log.Info("unauthorized request to " + r.URL.Path)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
