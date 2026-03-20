package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/otfabric/go-iec61850/scl"
	"github.com/spf13/cobra"
)

var summaryCmd = &cobra.Command{
	Use:   "summary <file>",
	Short: "Print compact overview with element counts",
	Long: `Print a compact overview of an SCL file with element counts.

Example:
  sclparse summary station.scd`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: sclFileCompletion,
	RunE:              runSummary,
}

var (
	summaryJSON       bool
	summaryPretty     bool
	summaryCountsOnly bool
)

func init() {
	summaryCmd.Flags().BoolVar(&summaryJSON, "json", false, "output as JSON")
	summaryCmd.Flags().BoolVar(&summaryPretty, "pretty", false, "pretty-print JSON output")
	summaryCmd.Flags().BoolVar(&summaryCountsOnly, "counts-only", false, "show only element counts")
}

func runSummary(_ *cobra.Command, args []string) error {
	path := args[0]

	result, err := scl.ParseFileOpts(path, scl.ParseOptions{})
	if err != nil {
		return exitErr(exitParseFail, "%v", err)
	}

	sum := scl.Summarize(result.Document)

	if summaryJSON {
		out := struct {
			File   string            `json:"file"`
			Schema scl.SchemaVersion `json:"schema"`
			Kind   scl.DocumentKind  `json:"kind"`
			Counts scl.Summary       `json:"counts"`
		}{
			File:   filepath.Base(path),
			Schema: result.Version.Schema,
			Kind:   result.Kind,
			Counts: sum,
		}
		enc := json.NewEncoder(os.Stdout)
		if summaryPretty {
			enc.SetIndent("", "  ")
		}
		_ = enc.Encode(out)
		return nil
	}

	if !summaryCountsOnly {
		fmt.Printf("File:     %s\n", filepath.Base(path))
		fmt.Printf("Schema:   %s\n", schemaLabel(result.Version.Schema))
		fmt.Printf("Kind:     %s\n", result.Kind)
		fmt.Println()
	}

	fmt.Printf("Substations:     %d\n", sum.Substations)
	fmt.Printf("Voltage levels:  %d\n", sum.VoltageLevels)
	fmt.Printf("Bays:            %d\n", sum.Bays)
	fmt.Printf("IEDs:            %d\n", sum.IEDs)
	fmt.Printf("Access points:   %d\n", sum.AccessPoints)
	fmt.Printf("Logical devices: %d\n", sum.LogicalDevices)
	fmt.Printf("Logical nodes:   %d (LN0: %d)\n", sum.LogicalNodes, sum.LN0Count)
	fmt.Printf("Data sets:       %d\n", sum.DataSets)
	fmt.Printf("Report controls: %d\n", sum.ReportControls)
	fmt.Printf("Log controls:    %d\n", sum.LogControls)
	fmt.Printf("GSE controls:    %d\n", sum.GSEControls)
	fmt.Printf("SMV controls:    %d\n", sum.SMVControls)
	fmt.Printf("Connected APs:   %d\n", sum.ConnectedAPs)
	fmt.Printf("LNodeTypes:      %d\n", sum.LNodeTypes)
	fmt.Printf("DOTypes:         %d\n", sum.DOTypes)
	fmt.Printf("DATypes:         %d\n", sum.DATypes)
	fmt.Printf("EnumTypes:       %d\n", sum.EnumTypes)
	if sum.HasServices {
		fmt.Printf("Services:        present\n")
	}
	if sum.PrivateCount > 0 {
		fmt.Printf("Private elems:   %d\n", sum.PrivateCount)
	}
	return nil
}
