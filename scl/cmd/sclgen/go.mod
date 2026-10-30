module github.com/otfabric/go-iec61850/scl/cmd/sclgen

go 1.24.0

require (
	github.com/otfabric/go-iec61850 v1.2.2
	github.com/spf13/cobra v1.10.2
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
)

// The tool is built from the library in this repository. v1.2.2 still contains
// this package, so the requirement alone is an ambiguous import.
replace github.com/otfabric/go-iec61850 => ../../..
