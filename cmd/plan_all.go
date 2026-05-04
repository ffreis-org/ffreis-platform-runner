package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ffreis/platform-runner/internal/executor"
	"github.com/ffreis/platform-runner/internal/runner"
)

var planAllConcurrency int

var planAllCmd = &cobra.Command{
	Use:   "plan-all",
	Short: "Run terraform plan for all configured repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := buildDeps(cmd)
		if err != nil {
			return err
		}

		r := runner.NewRunner(d.cfg, &executor.TerraformExecutor{}, runner.RunnerOptions{
			Workspace:   d.workspace,
			Concurrency: planAllConcurrency,
			DryRun:      d.dryRun,
			ProgressOut: cmd.ErrOrStderr(),
			Token:       d.token,
			Log:         d.log,
			UI:          d.ui,
		})

		report, err := r.PlanAll(cmd.Context())
		if err != nil {
			return fmt.Errorf("plan-all failed: %w", err)
		}

		newCommandOutput(cmd.OutOrStdout(), d.ui).Report(report)

		if report.HasFailures() {
			return &ExitError{Code: exitError, Err: errors.New("one or more repositories failed planning")}
		}
		return nil
	},
}

func init() {
	planAllCmd.Flags().IntVar(&planAllConcurrency, "concurrency", 5, "Number of concurrent workers")
}
