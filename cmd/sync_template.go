package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ffreis/platform-runner/internal/executor"
	"github.com/ffreis/platform-runner/internal/runner"
)

var (
	syncTemplateDir          string
	syncTemplateSafePatterns []string
)

var syncTemplateCmd = &cobra.Command{
	Use:   "sync-template",
	Short: "Sync template files to all configured repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := buildDeps(cmd)
		if err != nil {
			return err
		}
		defer d.log.Sync() //nolint:errcheck

		if syncTemplateDir == "" {
			return fmt.Errorf("--template-dir is required")
		}

		r := runner.NewRunner(d.cfg, &executor.TerraformExecutor{}, runner.RunnerOptions{
			TemplateDir:  syncTemplateDir,
			SafePatterns: syncTemplateSafePatterns,
			Workspace:    d.workspace,
			Concurrency:  5,
			DryRun:       d.dryRun,
			Token:        d.token,
			Log:          d.log,
		})

		report, err := r.SyncTemplate(cmd.Context())
		if err != nil {
			return fmt.Errorf("sync-template failed: %w", err)
		}

		fmt.Println(report.Summary())
		for _, res := range report.Results {
			if res.Status == runner.RepoStatusFailed {
				fmt.Printf("  FAILED  %s: %s\n", res.Repo, res.ErrMsg)
			} else {
				fmt.Printf("  ok      %s: %s\n", res.Repo, res.Output)
			}
		}

		return nil
	},
}

func init() {
	syncTemplateCmd.Flags().StringVar(&syncTemplateDir, "template-dir", "", "Path to template directory (required)")
	syncTemplateCmd.Flags().StringSliceVar(&syncTemplateSafePatterns, "safe-patterns", nil, "Glob patterns for files safe to overwrite unconditionally")
}
