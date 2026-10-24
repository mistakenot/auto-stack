package app

import (
	"io"
	"os"
	"time"

	"github.com/mistakenot/auto-shared/backend"
	"github.com/mistakenot/auto-watch/internal/shell"
)

type App struct {
	Stdout   io.Writer
	Stderr   io.Writer
	CWD      string
	Now      func() time.Time
	Backends *backend.Registry
	Runner   shell.Runner
}

func New(stdout, stderr io.Writer) *App {
	cwd, _ := os.Getwd()
	return &App{
		Stdout:   stdout,
		Stderr:   stderr,
		CWD:      cwd,
		Now:      time.Now,
		Backends: backend.NewRegistry(&backend.Tmux{}),
		Runner:   shell.ExecRunner{},
	}
}
