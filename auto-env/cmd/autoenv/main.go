package main

import (
	"context"
	"os"

	"github.com/mistakenot/auto-env/internal/cli"
)

func main() {
	os.Exit(cli.Execute(context.Background(), os.Stdout, os.Stderr))
}
