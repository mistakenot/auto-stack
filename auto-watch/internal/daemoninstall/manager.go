package daemoninstall

import (
	"io/fs"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"

	"github.com/mistakenot/auto-watch/internal/shell"
)

const (
	defaultDescription = "autowatch daemon"
	defaultServiceBase = "autowatch"
	systemUnitDir      = "/etc/systemd/system"
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
		unitDir:         systemUnitDir,
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

// unitDirFor returns the systemd unit directory for the given scope. System
// scope uses m.unitDir (overridable in tests, defaults to /etc/systemd/system);
// user scope uses <home>/.config/systemd/user.
func (m *Manager) unitDirFor(scope Scope, home string) string {
	if normalizeScope(scope) == ScopeSystem {
		return m.unitDir
	}
	return filepath.Join(home, ".config", "systemd", "user")
}

// userHome resolves the invoking (current) user's home directory. For user
// scope the runtime user is the current user, so this is also the home that
// anchors the user unit directory for the read paths (status/restart).
func (m *Manager) userHome() (string, error) {
	current, err := m.currentUser()
	if err != nil {
		return "", remediationError(
			"failed to determine the current user",
			"rerun from an interactive login session, or pass --system",
		)
	}
	home := current.HomeDir
	if home == "" {
		return "", remediationError(
			"failed to determine the current user's home directory",
			"rerun with --home <existing-absolute-path>, or pass --system",
		)
	}
	return home, nil
}

// unitDirHome resolves the home directory used to anchor the unit dir for the
// read paths (Status/Restart). System scope never needs a home; user scope
// resolves the current user's home (which anchors ~/.config/systemd/user) so
// both the default unit path and validation of an explicit one are correct.
func (m *Manager) unitDirHome(scope Scope) (string, error) {
	if normalizeScope(scope) == ScopeSystem {
		return "", nil
	}
	return m.userHome()
}

// systemctlArgs prepends "--user" to the systemctl arguments for user scope.
func systemctlArgs(scope Scope, args ...string) []string {
	if normalizeScope(scope) == ScopeSystem {
		return args
	}
	return append([]string{"--user"}, args...)
}

// installRemediation returns the scope-appropriate hint for re-running install.
func installRemediation(scope Scope) string {
	if normalizeScope(scope) == ScopeSystem {
		return "rerun with sudo auto watch daemon install --system"
	}
	return "rerun auto watch daemon install"
}

// restartRemediation returns the scope-appropriate hint for re-running restart.
func restartRemediation(scope Scope) string {
	if normalizeScope(scope) == ScopeSystem {
		return "rerun with sudo auto watch daemon restart --system"
	}
	return "rerun auto watch daemon restart"
}

// startRemediation returns the scope-appropriate hint for starting the unit.
func startRemediation(scope Scope, serviceName string) string {
	if normalizeScope(scope) == ScopeSystem {
		return "run sudo systemctl start " + serviceName
	}
	return "run systemctl --user start " + serviceName
}

// statusRemediation returns the scope-appropriate hint for re-running status.
func statusRemediation(scope Scope) string {
	if normalizeScope(scope) == ScopeSystem {
		return "rerun with sudo auto watch daemon status --system"
	}
	return "rerun auto watch daemon status"
}
