package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ffreis/platform-runner/internal/config"
	"github.com/ffreis/platform-runner/internal/logging"
)

var (
	flagConfig    string
	flagLogLevel  string
	flagDryRun    bool
	flagWorkspace string
	flagToken     string
)

// deps holds shared dependencies built from global flags.
type deps struct {
	cfg       []config.RepoConfig
	log       *zap.Logger
	dryRun    bool
	workspace string
	token     string
}

// buildDeps loads config and builds a logger from the root command flags.
func buildDeps(cmd *cobra.Command) (*deps, error) {
	log, err := logging.New(flagLogLevel)
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
	}, nil
}

// rootCmd is the top-level command.
var rootCmd = &cobra.Command{
	Use:   "platform-runner",
	Short: "Operate all platform repositories continuously",
	Long: `platform-runner validates, plans Terraform, syncs templates, and detects drift
across every configured repository.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagConfig, "config", "", "DynamoDB table name or YAML config path")
	rootCmd.PersistentFlags().StringVar(&flagLogLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false, "Dry-run mode: show what would happen without making changes")
	rootCmd.PersistentFlags().StringVar(&flagWorkspace, "workspace", "./workspace", "Local workspace directory for repo clones")
	rootCmd.PersistentFlags().StringVar(&flagToken, "token", "", "GitHub token (falls back to GITHUB_TOKEN env var)")

	rootCmd.AddCommand(planAllCmd)
	rootCmd.AddCommand(applyAllCmd)
	rootCmd.AddCommand(syncTemplateCmd)
	rootCmd.AddCommand(validateCmd)
}
