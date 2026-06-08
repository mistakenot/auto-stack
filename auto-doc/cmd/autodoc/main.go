package main

import (
	"os"

	"github.com/datadyne-io/autodoc/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
