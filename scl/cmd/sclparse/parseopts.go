// SPDX-License-Identifier: MIT

package main

import "github.com/otfabric/go-iec61850/scl"

// assumeNamespace is the persistent --assume-namespace flag: read an SCL
// root element without a namespace as the IEC 61850-6 namespace.
var assumeNamespace bool

// parseOpts adds the global parse flags to opts.
func parseOpts(opts scl.ParseOptions) scl.ParseOptions {
	opts.AssumeSCLNamespace = assumeNamespace
	return opts
}

// parseFile parses path with the global parse flags and returns the result.
func parseFile(path string) (*scl.Result, error) {
	return scl.ParseFileOpts(path, parseOpts(scl.ParseOptions{}))
}

// parseDoc parses path with the global parse flags and returns the model.
func parseDoc(path string) (*scl.SCL, error) {
	r, err := parseFile(path)
	if err != nil {
		return nil, err
	}
	return r.Document, nil
}
