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
User={{.RuntimeUser}}
Group={{.RuntimeGroup}}
Environment=HOME={{.HomeDir}}
Environment=PATH={{.PathEnv}}
WorkingDirectory={{.WorkingDir}}
ExecStart={{.BinPath}} watch start
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
`

func renderUnit(spec *ServiceSpec) (string, error) {
	tpl, err := template.New("unit").Parse(unitTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, spec); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n") + "\n", nil
}
