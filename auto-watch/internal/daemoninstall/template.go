package daemoninstall

import (
	"bytes"
	"strings"
	"text/template"
)

const unitTemplate = `[Unit]
Description={{.Description}}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
{{- if .IsSystem}}
User={{.RuntimeUser}}
Group={{.RuntimeGroup}}
{{- end}}
Environment=HOME={{.HomeDir}}
Environment=PATH={{.PathEnv}}
WorkingDirectory={{.WorkingDir}}
ExecStart={{.BinPath}} watch start
Restart=always
RestartSec=10

[Install]
WantedBy={{.WantedBy}}
`

// wantedByForScope returns the [Install] WantedBy target for a scope: system
// units attach to multi-user.target; user units to default.target.
func wantedByForScope(scope Scope) string {
	if normalizeScope(scope) == ScopeSystem {
		return "multi-user.target"
	}
	return "default.target"
}

func renderUnit(spec *ServiceSpec, scope Scope) (string, error) {
	tpl, err := template.New("unit").Parse(unitTemplate)
	if err != nil {
		return "", err
	}
	data := struct {
		*ServiceSpec
		WantedBy string
		IsSystem bool
	}{
		ServiceSpec: spec,
		WantedBy:    wantedByForScope(scope),
		// systemd's user manager runs as the user already and cannot apply
		// User=/Group= (it fails with status=216/GROUP "Failed to determine
		// supplementary groups"), so those directives are system-scope only.
		IsSystem: normalizeScope(scope) == ScopeSystem,
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n") + "\n", nil
}
