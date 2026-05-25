package handler

import (
	"encoding/json"
	"net/http"

	"example.com/sample/pkg/logger"
)

// Handler provides HTTP handler methods.
type Handler struct {
	log *logger.Logger
}

// New creates a handler with the given logger.
func New(log *logger.Logger) *Handler {
	return &Handler{log: log}
}

// Health returns a health check response.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	h.log.Info("health check")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// Data returns sample data.
func (h *Handler) Data(w http.ResponseWriter, r *http.Request) {
	h.log.Info("data request")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"items": []string{"a", "b", "c"}})
}
