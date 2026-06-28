package app

import "io"

// App carries the runtime context shared by every auto-artifact command.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	CWD    string
}

func New(stdout, stderr io.Writer, cwd string) *App {
	return &App{Stdout: stdout, Stderr: stderr, CWD: cwd}
}
