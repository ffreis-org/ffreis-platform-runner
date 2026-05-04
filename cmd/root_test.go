package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestBuildDeps_LoadsConfigAndTokenFallback(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
repos:
  - name: acme/repo
    environments: ["dev"]
    enabled: true
`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	origConfig := flagConfig
	origLogLevel := flagLogLevel
	origDryRun := flagDryRun
	origWorkspace := flagWorkspace
	origToken := flagToken
	origUI := flagUI
	t.Cleanup(func() {
		flagConfig = origConfig
		flagLogLevel = origLogLevel
		flagDryRun = origDryRun
		flagWorkspace = origWorkspace
		flagToken = origToken
		flagUI = origUI
	})

	if err := os.Setenv("GITHUB_TOKEN", "envtok"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("GITHUB_TOKEN") })

	flagConfig = cfgPath
	flagLogLevel = "info"
	flagDryRun = true
	flagWorkspace = "/tmp/ws"
	flagToken = ""
	flagUI = "plain"

	c := &cobra.Command{}
	c.SetContext(context.Background())
	d, err := buildDeps(c)
	if err != nil {
		t.Fatalf("buildDeps() unexpected error: %v", err)
	}
	if d == nil {
		t.Fatalf("expected non-nil deps")
	}
	if d.token != "envtok" {
		t.Fatalf("expected token from env, got %q", d.token)
	}
	if !d.dryRun || d.workspace != "/tmp/ws" {
		t.Fatalf("unexpected deps: %+v", d)
	}
	if len(d.cfg) != 1 || d.cfg[0].Name != "acme/repo" {
		t.Fatalf("unexpected config: %+v", d.cfg)
	}
}

func TestRootCmd_Help(t *testing.T) {
	var out bytes.Buffer
	origOut := rootCmd.OutOrStdout()
	origErr := rootCmd.ErrOrStderr()
	origArgs := rootCmd.Args
	t.Cleanup(func() {
		rootCmd.SetOut(origOut)
		rootCmd.SetErr(origErr)
		rootCmd.SetArgs(nil)
		rootCmd.Args = origArgs
	})

	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd --help unexpected error: %v", err)
	}
	if out.Len() == 0 {
		t.Fatalf("expected help output")
	}
}

func TestBuildDeps_InvalidLogLevel(t *testing.T) {
	origConfig := flagConfig
	origLogLevel := flagLogLevel
	origUI := flagUI
	t.Cleanup(func() {
		flagConfig = origConfig
		flagLogLevel = origLogLevel
		flagUI = origUI
	})

	flagConfig = filepath.Join(t.TempDir(), "missing.yaml")
	flagLogLevel = "invalid"
	flagUI = "plain"

	c := &cobra.Command{}
	c.SetContext(context.Background())

	_, err := buildDeps(c)
	if err == nil || err.Error() == "" {
		t.Fatal("expected buildDeps() to fail")
	}
}

func TestExecute_Help(t *testing.T) {
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"platform-runner", "--help"}
	if code := Execute(); code != exitOK {
		t.Fatalf("Execute() exit code: got %d, want %d", code, exitOK)
	}
}

func TestExecuteCommand_ReturnsExitCodeAndErrorText(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		RunE: func(*cobra.Command, []string) error {
			return &ExitError{Code: 9, Err: os.ErrPermission}
		},
	}

	var stderr bytes.Buffer
	code := executeCommand(cmd, &stderr)
	if code != 9 {
		t.Fatalf("executeCommand() code: got %d, want 9", code)
	}
	if got := stderr.String(); got != "error: permission denied\n" {
		t.Fatalf("executeCommand() stderr: got %q", got)
	}
}

func TestExitCodeForError_Default(t *testing.T) {
	t.Parallel()

	if code := exitCodeForError(os.ErrInvalid); code != exitError {
		t.Fatalf("exitCodeForError() = %d, want %d", code, exitError)
	}
}
