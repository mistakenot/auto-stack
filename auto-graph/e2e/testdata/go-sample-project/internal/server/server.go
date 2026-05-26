package server

import (
	"net/http"

	"example.com/sample/internal/handler"
	"example.com/sample/internal/middleware"
	"example.com/sample/pkg/logger"
)

// Server holds the HTTP server configuration.
type Server struct {
	addr   string
	logger *logger.Logger
}

// New creates a new server instance.
func New(addr string, log *logger.Logger) *Server {
	return &Server{addr: addr, logger: log}
}

// Start begins listening on the configured address.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	h := handler.New(s.logger)
	mw := middleware.NewLogging(s.logger)

	mux.Handle("/api/health", mw.Wrap(http.HandlerFunc(h.Health)))
	mux.Handle("/api/data", mw.Wrap(http.HandlerFunc(h.Data)))

	s.logger.Info("listening on " + s.addr)
	return http.ListenAndServe(s.addr, mux)
}
