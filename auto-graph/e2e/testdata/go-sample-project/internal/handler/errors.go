package handler

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse represents a JSON error payload.
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// WriteError sends a structured error response.
func WriteError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(ErrorResponse{Code: code, Message: msg})
}
