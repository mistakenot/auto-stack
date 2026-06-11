module github.com/mistakenot/auto-etl

go 1.26.1

require (
	github.com/google/go-github/v72 v72.0.0
	github.com/mistakenot/auto-shared v0.0.0
	github.com/parquet-go/parquet-go v0.30.1
	github.com/spf13/cobra v1.10.2
)

replace github.com/mistakenot/auto-shared => ../auto-shared

require (
	github.com/andybalholm/brotli v1.1.1 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/parquet-go/bitpack v1.0.0 // indirect
	github.com/parquet-go/jsonlite v1.0.0 // indirect
	github.com/pierrec/lz4/v4 v4.1.21 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/twpayne/go-geom v1.6.1 // indirect
	golang.org/x/sys v0.42.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)
