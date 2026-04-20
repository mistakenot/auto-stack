package app

import "io"

type App struct {
	Stdout io.Writer
	Stderr io.Writer
	CWD    string
}

func New(stdout, stderr io.Writer, cwd string) *App {
	return &App{Stdout: stdout, Stderr: stderr, CWD: cwd}
}
