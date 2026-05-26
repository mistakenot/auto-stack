package server

import "time"

// Config holds server tuning parameters.
type Config struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	MaxConns     int
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		MaxConns:     100,
	}
}
