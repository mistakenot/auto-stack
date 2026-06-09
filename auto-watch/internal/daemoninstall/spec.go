package daemoninstall

// Scope selects whether the daemon is managed as a system-level systemd unit
// (root, /etc/systemd/system, systemctl) or a user-level unit (no sudo,
// ~/.config/systemd/user, systemctl --user). The empty value defaults to
// ScopeUser.
type Scope string

const (
	ScopeUser   Scope = "user"
	ScopeSystem Scope = "system"
)

// normalizeScope treats the empty scope as ScopeUser.
func normalizeScope(s Scope) Scope {
	if s == ScopeSystem {
		return ScopeSystem
	}
	return ScopeUser
}

type ServiceSpec struct {
	ServiceName  string `json:"serviceName"`
	Description  string `json:"description,omitempty"`
	RuntimeUser  string `json:"runtimeUser"`
	RuntimeGroup string `json:"runtimeGroup"`
	HomeDir      string `json:"homeDir"`
	WorkingDir   string `json:"workingDir"`
	BinPath      string `json:"binPath"`
	PathEnv      string `json:"pathEnv"`
	UnitPath     string `json:"unitPath"`
}

type InstallOptions struct {
	ServiceName string
	RuntimeUser string
	HomeDir     string
	WorkingDir  string
	BinPath     string
	PathEnv     string
	Description string
	UnitPath    string
	Scope       Scope
	Enable      bool
	Start       bool
	DryRun      bool
	PrintUnit   bool
}

type InstallResult struct {
	Spec             ServiceSpec
	Unit             string
	ExistingUnit     bool
	Changed          bool
	PlannedActions   []string
	CompletedActions []string
	Warnings         []string
}

type RestartOptions struct {
	ServiceName string
	UnitPath    string
	Scope       Scope
}

type RestartResult struct {
	ServiceName string
	UnitPath    string
}

type StatusOptions struct {
	ServiceName string
	UnitPath    string
	Scope       Scope
}

type ServiceStatus struct {
	Installed   bool   `json:"installed"`
	Enabled     bool   `json:"enabled"`
	Running     bool   `json:"running"`
	ServiceName string `json:"serviceName"`
	UnitPath    string `json:"unitPath"`
	Manager     string `json:"manager"`
	User        string `json:"user,omitempty"`
	Group       string `json:"group,omitempty"`
	HomeDir     string `json:"homeDir,omitempty"`
	WorkingDir  string `json:"workingDir,omitempty"`
	ExecStart   string `json:"execStart,omitempty"`
}

type CombinedStatus struct {
	Daemon         ServiceStatus  `json:"daemon"`
	Runtime        map[string]any `json:"runtime,omitempty"`
	RuntimeWarning string         `json:"runtimeWarning,omitempty"`
}
