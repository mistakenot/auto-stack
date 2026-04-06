package daemoninstall

import (
	"io/fs"
	"os"
	"os/exec"
	"os/user"

	"github.com/mistakenot/auto-watch/internal/shell"
)

const (
	defaultDescription = "autowatch daemon"
	defaultServiceBase = "autowatch"
)

type Manager struct {
	Runner          shell.Runner
	goos            string
	unitDir         string
	getenv          func(string) string
	geteuid         func() int
	currentUser     func() (*user.User, error)
	lookupUser      func(string) (*user.User, error)
	lookupGroupID   func(string) (*user.Group, error)
	lookPath        func(string) (string, error)
	readFile        func(string) ([]byte, error)
	stat            func(string) (os.FileInfo, error)
	writeFileAtomic func(string, []byte, fs.FileMode) error
}

func NewManager(runner shell.Runner) *Manager {
	if runner == nil {
		runner = shell.ExecRunner{}
	}
	return &Manager{
		Runner:          runner,
		goos:            runtimeGOOS(),
		unitDir:         "/etc/systemd/system",
		getenv:          os.Getenv,
		geteuid:         os.Geteuid,
		currentUser:     user.Current,
		lookupUser:      user.Lookup,
		lookupGroupID:   user.LookupGroupId,
		lookPath:        exec.LookPath,
		readFile:        os.ReadFile,
		stat:            os.Stat,
		writeFileAtomic: writeFileAtomic,
	}
}
