package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ffreis/platform-runner/internal/logging"
	"github.com/ffreis/platform-runner/internal/repos"
	"github.com/ffreis/platform-runner/internal/ui"
)

var (
	deliverFlemmingConfirm         bool
	deliverFlemmingEnv             = "prod"
	deliverFlemmingOrg             string
	deliverFlemmingProfile         string
	deliverFlemmingInfraRepo       = "FelipeFuhr/ffreis-flemming-infra"
	deliverFlemmingWebsiteRepo     = "FelipeFuhr/flemming-website"
	deliverFlemmingCompilerRepo    = "FelipeFuhr/ffreis-website-compiler"
	deliverFlemmingPackerRepo      = "FelipeFuhr/ffreis-website-packer"
	deliverFlemmingDomainName      string
	deliverFlemmingWWWDomainName   string
	deliverFlemmingRoute53ZoneName string
	deliverFlemmingPublishPrefix   string
)

var deliverFlemmingCmd = &cobra.Command{
	Use:   "deliver-flemming",
	Short: "Run the Flemming infra+website delivery flow through the shared runner workspace",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if flagDryRun {
			return fmt.Errorf("--dry-run is not supported for deliver-flemming")
		}
		if !deliverFlemmingConfirm {
			return fmt.Errorf("--confirm is required")
		}

		presenter, err := ui.New(flagUI)
		if err != nil {
			return fmt.Errorf("building ui: %w", err)
		}
		log, err := logging.New(flagLogLevel, presenter.Interactive())
		if err != nil {
			return fmt.Errorf("building logger: %w", err)
		}

		token := flagToken
		if token == "" {
			token = os.Getenv("GITHUB_TOKEN")
		}

		out := newCommandOutput(cmd.OutOrStdout(), presenter)
		out.Header("Platform Runner Flemming Delivery", "workspace "+flagWorkspace)
		out.Summary("Repos",
			"infra="+deliverFlemmingInfraRepo,
			"website="+deliverFlemmingWebsiteRepo,
			"compiler="+deliverFlemmingCompilerRepo,
			"packer="+deliverFlemmingPackerRepo,
		)
		out.Blank()

		ensureRepo := func(repo string) (string, error) {
			w := &repos.Workspace{Repo: repo, RootDir: flagWorkspace, Token: token}
			if err := w.Ensure(cmd.Context()); err != nil {
				return "", err
			}
			return w.Dir(), nil
		}

		out.Status("info", "sync", "ensuring local workspaces")
		infraDir, err := ensureRepo(deliverFlemmingInfraRepo)
		if err != nil {
			return err
		}
		websiteDir, err := ensureRepo(deliverFlemmingWebsiteRepo)
		if err != nil {
			return err
		}
		compilerDir, err := ensureRepo(deliverFlemmingCompilerRepo)
		if err != nil {
			return err
		}
		packerDir, err := ensureRepo(deliverFlemmingPackerRepo)
		if err != nil {
			return err
		}
		out.Status("ok", "sync", "workspace repos ready")

		args := []string{"go-deliver", "ENV=" + deliverFlemmingEnv}
		if strings.TrimSpace(deliverFlemmingOrg) != "" {
			args = append(args, "ORG="+deliverFlemmingOrg)
		}
		if strings.TrimSpace(deliverFlemmingProfile) != "" {
			args = append(args, "PROFILE="+deliverFlemmingProfile)
		}
		args = append(args,
			"WEBSITE_ROOT="+websiteDir,
			"WEBSITE_COMPILER_ROOT="+compilerDir,
			"WEBSITE_PACKER_ROOT="+packerDir,
		)
		if strings.TrimSpace(deliverFlemmingPublishPrefix) != "" {
			args = append(args, "WEBSITE_PUBLISH_PREFIX="+deliverFlemmingPublishPrefix)
		}
		if strings.TrimSpace(deliverFlemmingDomainName) != "" {
			args = append(args, "DOMAIN_NAME="+deliverFlemmingDomainName)
		}
		if strings.TrimSpace(deliverFlemmingWWWDomainName) != "" {
			args = append(args, "WWW_DOMAIN_NAME="+deliverFlemmingWWWDomainName)
		}
		if strings.TrimSpace(deliverFlemmingRoute53ZoneName) != "" {
			args = append(args, "ROUTE53_ZONE_NAME="+deliverFlemmingRoute53ZoneName)
		}

		out.Blank()
		out.Status("info", "deliver", "running make "+strings.Join(args, " "))
		makeCmd := exec.CommandContext(cmd.Context(), "make", args...) //nolint:gosec
		makeCmd.Dir = infraDir
		makeCmd.Stdout = cmd.OutOrStdout()
		makeCmd.Stderr = cmd.ErrOrStderr()
		makeCmd.Env = os.Environ()
		if err := makeCmd.Run(); err != nil {
			log.Error("flemming delivery failed", "error", err)
			return fmt.Errorf("flemming delivery failed: %w", err)
		}

		out.Blank()
		out.Status("ok", "live", "flemming delivery completed through platform-runner")
		return nil
	},
}

func init() {
	deliverFlemmingCmd.Flags().BoolVar(&deliverFlemmingConfirm, "confirm", false, "Confirm the Flemming delivery run")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingEnv, "env", "prod", "Environment to deliver")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingOrg, "org", "", "Org passed through to make go-deliver")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingProfile, "profile", "", "AWS profile passed through to make go-deliver")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingInfraRepo, "infra-repo", deliverFlemmingInfraRepo, "Infra repo in org/repo format")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingWebsiteRepo, "website-repo", deliverFlemmingWebsiteRepo, "Website repo in org/repo format")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingCompilerRepo, "compiler-repo", deliverFlemmingCompilerRepo, "Website compiler repo in org/repo format")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingPackerRepo, "packer-repo", deliverFlemmingPackerRepo, "Website packer repo in org/repo format")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingDomainName, "domain-name", "", "Temporary domain_name override passed through to go-deliver")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingWWWDomainName, "www-domain-name", "", "Temporary www_domain_name override passed through to go-deliver")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingRoute53ZoneName, "route53-zone-name", "", "Temporary route53_zone_name override passed through to go-deliver")
	deliverFlemmingCmd.Flags().StringVar(&deliverFlemmingPublishPrefix, "publish-prefix", "", "Website publish prefix passed through to go-deliver")
}
