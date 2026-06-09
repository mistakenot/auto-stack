package daemoninstall

import (
	"os/user"
	"path/filepath"
)

// DefaultServiceName is the canonical unit file name for the watch daemon.
const DefaultServiceName = defaultServiceBase + ".service"

// DefaultUnitPaths returns the candidate systemd unit paths for the watch
// daemon in lookup priority order: the user-scope unit under the current user's
// ~/.config/systemd/user first, then the system-scope unit under
// /etc/systemd/system. Consumers (for example doctor) can probe these in order
// to locate an installed unit without duplicating the scope path logic. If the
// current user's home cannot be resolved, only the system path is returned.
func DefaultUnitPaths() []string {
	var paths []string
	if current, err := user.Current(); err == nil && current.HomeDir != "" {
		paths = append(paths, filepath.Join(current.HomeDir, ".config", "systemd", "user", DefaultServiceName))
	}
	paths = append(paths, filepath.Join(systemUnitDir, DefaultServiceName))
	return paths
}
