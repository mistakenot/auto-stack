package main

import (
	"fmt"
	"os"

	"example.com/sample/internal/server"
	"example.com/sample/pkg/logger"
)

func main() {
	log := logger.New(os.Stdout)
	log.Info("starting application")

	srv := server.New(":8080", log)
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "server error: %v\n", err)
		os.Exit(1)
	}
}
