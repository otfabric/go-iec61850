// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var detectCmd = &cobra.Command{
	Use:   "detect <file>",
	Short: "Identify schema version and document kind",
	Long: `Identify the SCL schema version and document kind without fully parsing.

Example:
  sclparse detect station.scd`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: sclFileCompletion,
	RunE:              runDetect,
}

var (
	detectJSON  bool
	detectQuiet bool
)

func init() {
	detectCmd.Flags().BoolVar(&detectJSON, "json", false, "output as JSON")
	detectCmd.Flags().BoolVar(&detectQuiet, "quiet", false, "only print schema version")
}

func runDetect(_ *cobra.Command, args []string) error {
	path := args[0]

	data, err := os.ReadFile(path)
	if err != nil {
		return exitErr(exitParseFail, "read %s: %v", path, err)
	}

	vi, err := scl.DetectVersion(data)
	if err != nil {
		return exitErr(exitParseFail, "detect: %v", err)
	}

	kind := scl.KindFromPath(path)

	if detectJSON {
		out := struct {
			File             string            `json:"file"`
			Schema           scl.SchemaVersion `json:"schema"`
			Namespace        string            `json:"namespace"`
			Version          string            `json:"version,omitempty"`
			Revision         string            `json:"revision,omitempty"`
			Release          string            `json:"release,omitempty"`
			Kind             scl.DocumentKind  `json:"kind"`
			Confidence       scl.Confidence    `json:"confidence"`
			Reasons          []string          `json:"reasons,omitempty"`
			VendorNamespaces []string          `json:"vendorNamespaces,omitempty"`
		}{
			File:             filepath.Base(path),
			Schema:           vi.Schema,
			Namespace:        vi.Namespace,
			Version:          vi.Version,
			Revision:         vi.Revision,
			Release:          vi.Release,
			Kind:             kind,
			Confidence:       vi.Confidence,
			Reasons:          vi.Reasons,
			VendorNamespaces: vi.VendorNamespaces,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return nil
	}

	if detectQuiet {
		if vi.Schema == scl.VersionUnknown {
			fmt.Println("unknown")
		} else {
			fmt.Println(vi.Schema)
		}
		return nil
	}

	fmt.Printf("File:       %s\n", filepath.Base(path))
	fmt.Printf("Schema:     %s\n", schemaLabel(vi.Schema))
	fmt.Printf("Namespace:  %s\n", vi.Namespace)
	if vi.Version != "" || vi.Revision != "" || vi.Release != "" {
		fmt.Printf("Declared:   version=%s revision=%s release=%s\n",
			vi.Version, vi.Revision, vi.Release)
	}
	fmt.Printf("Kind:       %s\n", kind)
	fmt.Printf("Confidence: %s\n", vi.Confidence)
	if len(vi.Reasons) > 0 {
		for _, r := range vi.Reasons {
			fmt.Printf("  - %s\n", r)
		}
	}
	if len(vi.VendorNamespaces) > 0 {
		fmt.Printf("Vendor NS:  %v\n", vi.VendorNamespaces)
	}
	return nil
}

func schemaLabel(v scl.SchemaVersion) string {
	if v == scl.VersionUnknown {
		return "unknown"
	}
	return string(v)
}
