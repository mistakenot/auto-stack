module github.com/mistakenot/auto-graph

go 1.26.1

require (
	github.com/datadyne-io/autodoc v0.0.0
	github.com/mistakenot/auto-shared v0.0.0
	github.com/spf13/cobra v1.10.2
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
)

replace github.com/datadyne-io/autodoc => ../auto-doc

replace github.com/mistakenot/auto-shared => ../auto-shared
