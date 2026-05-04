package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ffreis/platform-runner/internal/executor"
	"github.com/ffreis/platform-runner/internal/runner"
)

var validateRulesDir string

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Run platform-guardian validation for all configured repositories",
	RunE: func(cmd *cobra.Command, args []string) error {
		d, err := buildDeps(cmd)
		if err != nil {
			return err
		}

		r := runner.NewRunner(d.cfg, &executor.TerraformExecutor{}, runner.RunnerOptions{
			RulesDir:    validateRulesDir,
			Workspace:   d.workspace,
			Concurrency: 5,
			DryRun:      d.dryRun,
			ProgressOut: cmd.ErrOrStderr(),
			Token:       d.token,
			Log:         d.log,
			UI:          d.ui,
		})

		report, err := r.Validate(cmd.Context())
		if err != nil {
			return fmt.Errorf("validate failed: %w", err)
		}

		newCommandOutput(cmd.OutOrStdout(), d.ui).Report(report)

		if report.HasFailures() {
			return &ExitError{Code: exitError, Err: errors.New("one or more repositories failed validation")}
		}
		return nil
	},
}

func init() {
	validateCmd.Flags().StringVar(&validateRulesDir, "rules-dir", "", "Path to guardian rules directory")
}
