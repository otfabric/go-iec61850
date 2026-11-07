// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionJSON bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long: `Print the version, release tag, commit, build date, Go version and platform
on one line, or as one line of JSON with --json.`,
	Args: cobra.NoArgs,
	RunE: func(_ *cobra.Command, _ []string) error {
		info := currentBuildInfo()
		line := info.Text()
		if versionJSON {
			line = info.JSON()
		}
		_, err := fmt.Println(line)
		return outputErr(err)
	},
}

func init() {
	versionCmd.Flags().BoolVar(&versionJSON, "json", false, "print the version information as one line of JSON")
}
