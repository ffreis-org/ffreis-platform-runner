package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/ffreis/platform-cli/pkg/ui"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print build information",
	Run: func(cmd *cobra.Command, _ []string) {
		presenter, err := ui.New(flagUI)
		if err != nil {
			presenter = nil // fall back to plain text output
		}
		out := newCommandOutput(cmd.OutOrStdout(), presenter)

		v := strings.TrimSpace(version)
		if v == "" {
			v = "dev"
		}
		c := strings.TrimSpace(commit)
		if c == "" {
			c = "unknown"
		}
		t := strings.TrimSpace(buildTime)
		if t == "" {
			t = "unknown"
		}

		out.Line(v + " (commit=" + c + " built=" + t + ")")
	},
}
