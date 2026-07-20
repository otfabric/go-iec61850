// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"text/tabwriter"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect <file>",
	Short: "Show schema version, vendor namespaces, and extension details",
	Args:  cobra.ExactArgs(1), ValidArgsFunction: sclFileCompletion,
	RunE: runInspect,
}

var inspectJSON bool

func init() {
	inspectCmd.Flags().BoolVar(&inspectJSON, "json", false, "output as JSON")
}

type inspectResult struct {
	Schema           string         `json:"schema"`
	Kind             string         `json:"kind"`
	DetectedKind     string         `json:"detectedKind"`
	Confidence       string         `json:"confidence"`
	Namespace        string         `json:"namespace"`
	VendorNamespaces []string       `json:"vendorNamespaces,omitempty"`
	Reasons          []string       `json:"reasons,omitempty"`
	PrivateCount     int            `json:"privateCount"`
	PrivateTypes     map[string]int `json:"privateTypes,omitempty"`
}

func runInspect(_ *cobra.Command, args []string) error {
	path := args[0]

	vi, err := scl.DetectFile(path)
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	doc, err := scl.ParseFile(path)
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	privateTypes := make(map[string]int)
	totalPrivate := 0
	countPrivates := func(privs []scl.Private) {
		for _, p := range privs {
			totalPrivate++
			t := p.Type
			if t == "" {
				t = "(untyped)"
			}
			privateTypes[t]++
		}
	}

	for _, ied := range doc.IEDs {
		countPrivates(ied.Private)
		for _, ap := range ied.AccessPoints {
			countPrivates(ap.Private)
			if ap.Server == nil {
				continue
			}
			for _, ld := range ap.Server.LDevices {
				countPrivates(ld.Private)
				if ld.LN0 != nil {
					countPrivates(ld.LN0.Private)
				}
				for _, ln := range ld.LNs {
					countPrivates(ln.Private)
				}
			}
		}
	}

	result := inspectResult{
		Schema:           string(vi.Schema),
		Kind:             string(scl.KindFromPath(path)),
		DetectedKind:     string(scl.DetectKind(doc)),
		Confidence:       string(vi.Confidence),
		Namespace:        vi.Namespace,
		VendorNamespaces: vi.VendorNamespaces,
		Reasons:          vi.Reasons,
		PrivateCount:     totalPrivate,
		PrivateTypes:     privateTypes,
	}

	if inspectJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	fmt.Printf("Schema:          %s\n", result.Schema)
	fmt.Printf("Kind (ext):      %s\n", result.Kind)
	fmt.Printf("Kind (content):  %s\n", result.DetectedKind)
	fmt.Printf("Confidence:      %s\n", result.Confidence)
	fmt.Printf("Namespace:       %s\n", result.Namespace)

	if len(result.VendorNamespaces) > 0 {
		fmt.Println("\nVendor namespaces:")
		for _, ns := range result.VendorNamespaces {
			fmt.Printf("  %s\n", ns)
		}
	}

	if len(result.Reasons) > 0 {
		fmt.Println("\nDetection reasons:")
		for _, r := range result.Reasons {
			fmt.Printf("  %s\n", r)
		}
	}

	fmt.Printf("\nPrivate elements: %d\n", result.PrivateCount)
	if len(result.PrivateTypes) > 0 {
		tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "  TYPE\tCOUNT")
		sorted := make([]string, 0, len(result.PrivateTypes))
		for t := range result.PrivateTypes {
			sorted = append(sorted, t)
		}
		sort.Strings(sorted)
		for _, t := range sorted {
			_, _ = fmt.Fprintf(tw, "  %s\t%d\n", t, result.PrivateTypes[t])
		}
		_ = tw.Flush()
	}

	return nil
}
