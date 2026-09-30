// Package app carries the runtime context shared by every auto-plan command.
package app

import "io"

// App carries the runtime context shared by every auto-plan command.
type App struct {
	Stdout io.Writer
	Stderr io.Writer
	CWD    string
}

// New builds an App.
func New(stdout, stderr io.Writer, cwd string) *App {
	return &App{Stdout: stdout, Stderr: stderr, CWD: cwd}
}
