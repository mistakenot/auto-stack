package app

import (
	"io"
	"os"
)

type App struct {
	Stdout io.Writer
	Stderr io.Writer
	CWD    string
}

func New(stdout, stderr io.Writer) *App {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return &App{
		Stdout: stdout,
		Stderr: stderr,
		CWD:    cwd,
	}
}
