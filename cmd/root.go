package cmd

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/ffreis/platform-runner/internal/config"
	"github.com/ffreis/platform-runner/internal/logging"
	"github.com/ffreis/platform-runner/internal/ui"
)

var (
	flagConfig    string
	flagLogLevel  string
	flagDryRun    bool
	flagWorkspace string
	flagToken     string
	flagUI        string
)

const (
	exitOK    = 0
	exitError = 1
)

// ExitError carries an exit code without forcing command code to terminate the process.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// deps holds shared dependencies built from global flags.
type deps struct {
	cfg       []config.RepoConfig
	log       *slog.Logger
	dryRun    bool
	workspace string
	token     string
	ui        *ui.Presenter
}

// buildDeps loads config and builds a logger from the root command flags.
func buildDeps(cmd *cobra.Command) (*deps, error) {
	presenter, err := ui.New(flagUI)
	if err != nil {
		return nil, fmt.Errorf("building ui: %w", err)
	}

	log, err := logging.New(flagLogLevel, presenter.Interactive())
	if err != nil {
		return nil, fmt.Errorf("building logger: %w", err)
	}

	token := flagToken
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	var (
		tableName string
		yamlPath  string
	)

	if flagConfig != "" {
		if info, statErr := os.Stat(flagConfig); statErr == nil && !info.IsDir() {
			// Treat an existing file path as a YAML config file.
			yamlPath = flagConfig
		} else {
			// Otherwise, treat it as a DynamoDB table name.
			tableName = flagConfig
		}
	}

	cfg, err := config.Load(cmd.Context(), tableName, yamlPath)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	return &deps{
		cfg:       cfg,
		log:       log,
		dryRun:    flagDryRun,
		workspace: flagWorkspace,
		token:     token,
		ui:        presenter,
	}, nil
}

// rootCmd is the top-level command.
var rootCmd = &cobra.Command{
	Use:   "platform-runner",
	Short: "Operate all platform repositories continuously",
	Long: `platform-runner validates, plans Terraform, syncs templates, and detects drift
across every configured repository.`,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command and returns the process exit code for main.
func Execute() int {
	return executeCommand(rootCmd, os.Stderr)
}

func executeCommand(cmd *cobra.Command, stderr io.Writer) int {
	if err := cmd.Execute(); err != nil {
		if message := err.Error(); message != "" {
			_, _ = io.WriteString(stderr, "error: "+message+"\n")
		}
		return exitCodeForError(err)
	}
	return exitOK
}

func exitCodeForError(err error) int {
	var exitErr *ExitError
	if errors.As(err, &exitErr) && exitErr != nil {
		if exitErr.Code != 0 {
			return exitErr.Code
		}
	}
	return exitError
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "DynamoDB table name or YAML config path")
	rootCmd.PersistentFlags().StringVar(&flagLogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false, "Dry-run mode: show what would happen without making changes")
	rootCmd.PersistentFlags().StringVar(&flagWorkspace, "workspace", "./workspace", "Local workspace directory for repo clones")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "GitHub token (falls back to GITHUB_TOKEN env var)")
	rootCmd.PersistentFlags().StringVar(&flagUI, "ui", "auto", "UI mode: auto, plain, rich")

	rootCmd.AddCommand(planAllCmd)
	rootCmd.AddCommand(applyAllCmd)
	rootCmd.AddCommand(deliverFlemmingCmd)
	rootCmd.AddCommand(syncTemplateCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(versionCmd)
}
