package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandFailureSubprocess(t *testing.T) {
	mode := os.Getenv("RUNNER_CMD_FAILURE_MODE")
	if mode == "" {
		return
	}

	flagConfig = os.Getenv("RUNNER_CMD_CONFIG")
	flagLogLevel = "info"
	flagWorkspace = os.Getenv("RUNNER_CMD_WORKSPACE")
	validateRulesDir = os.Getenv("RUNNER_CMD_RULES")

	switch mode {
	case "plan":
		planAllCmd.SetContext(context.Background())
		_ = planAllCmd.RunE(planAllCmd, nil)
	case "validate":
		validateCmd.SetContext(context.Background())
		_ = validateCmd.RunE(validateCmd, nil)
	default:
		t.Fatalf("unknown subprocess mode %q", mode)
	}
}

func writeDisabledConfig(t *testing.T) string {
	t.Helper()

	f, err := os.CreateTemp("", "runner-cmd-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp(): %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })

	const content = `
repos:
  - name: acme/disabled
    environments: [dev]
    enabled: false
`
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString(): %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	return f.Name()
}

func restoreCommandGlobals(t *testing.T) {
	t.Helper()

	origConfig := flagConfig
	origLogLevel := flagLogLevel
	origDryRun := flagDryRun
	origWorkspace := flagWorkspace
	origToken := flagToken
	origPlanConcurrency := planAllConcurrency
	origApplyConcurrency := applyAllConcurrency
	origApplyConfirm := applyAllConfirm
	origTemplateDir := syncTemplateDir
	origSafePatterns := append([]string(nil), syncTemplateSafePatterns...)
	origRulesDir := validateRulesDir

	t.Cleanup(func() {
		flagConfig = origConfig
		flagLogLevel = origLogLevel
		flagDryRun = origDryRun
		flagWorkspace = origWorkspace
		flagToken = origToken
		planAllConcurrency = origPlanConcurrency
		applyAllConcurrency = origApplyConcurrency
		applyAllConfirm = origApplyConfirm
		syncTemplateDir = origTemplateDir
		syncTemplateSafePatterns = origSafePatterns
		validateRulesDir = origRulesDir
	})
}

func TestPlanAllCmd_RunE_NoRepos(t *testing.T) {
	restoreCommandGlobals(t)

	flagConfig = writeDisabledConfig(t)
	flagLogLevel = "info"
	flagWorkspace = t.TempDir()
	planAllConcurrency = 7

	var stdout bytes.Buffer
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe(): %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })
	t.Cleanup(func() { _ = r.Close() })

	planAllCmd.SetContext(context.Background())
	runErr := planAllCmd.RunE(planAllCmd, nil)
	_ = w.Close()
	_, _ = stdout.ReadFrom(r)

	if runErr != nil {
		t.Fatalf("RunE() unexpected error: %v", runErr)
	}
	if stdout.String() == "" {
		t.Fatal("expected output")
	}
}

func TestApplyAllCmd_RunE_MutuallyExclusiveFlags(t *testing.T) {
	restoreCommandGlobals(t)

	flagConfig = writeDisabledConfig(t)
	flagLogLevel = "info"
	flagWorkspace = t.TempDir()
	flagDryRun = true
	applyAllConfirm = true

	applyAllCmd.SetContext(context.Background())
	if err := applyAllCmd.RunE(applyAllCmd, nil); err == nil || err.Error() != "--dry-run and --confirm are mutually exclusive" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyAllCmd_RunE_NoRepos(t *testing.T) {
	restoreCommandGlobals(t)

	flagConfig = writeDisabledConfig(t)
	flagLogLevel = "info"
	flagWorkspace = t.TempDir()
	applyAllConfirm = true

	applyAllCmd.SetContext(context.Background())
	if err := applyAllCmd.RunE(applyAllCmd, nil); err != nil {
		t.Fatalf("RunE() unexpected error: %v", err)
	}
}

func TestSyncTemplateCmd_RunE_RequiresTemplateDir(t *testing.T) {
	restoreCommandGlobals(t)

	flagConfig = writeDisabledConfig(t)
	flagLogLevel = "info"
	flagWorkspace = t.TempDir()
	syncTemplateDir = ""

	syncTemplateCmd.SetContext(context.Background())
	err := syncTemplateCmd.RunE(syncTemplateCmd, nil)
	if err == nil || err.Error() != "--template-dir is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSyncTemplateCmd_RunE_NoRepos(t *testing.T) {
	restoreCommandGlobals(t)

	flagConfig = writeDisabledConfig(t)
	flagLogLevel = "info"
	flagWorkspace = t.TempDir()
	syncTemplateDir = t.TempDir()
	syncTemplateSafePatterns = []string{"*.md"}

	syncTemplateCmd.SetContext(context.Background())
	if err := syncTemplateCmd.RunE(syncTemplateCmd, nil); err != nil {
		t.Fatalf("RunE() unexpected error: %v", err)
	}
}

func TestValidateCmd_RunE_NoRepos(t *testing.T) {
	restoreCommandGlobals(t)

	flagConfig = writeDisabledConfig(t)
	flagLogLevel = "info"
	flagWorkspace = t.TempDir()
	validateRulesDir = t.TempDir()

	validateCmd.SetContext(context.Background())
	if err := validateCmd.RunE(validateCmd, nil); err != nil {
		t.Fatalf("RunE() unexpected error: %v", err)
	}
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func setupWorkspaceRepo(t *testing.T, workspaceRoot, repo string) {
	t.Helper()

	baseDir := t.TempDir()
	remotePath := filepath.Join(baseDir, "remote.git")
	workTree := filepath.Join(baseDir, "worktree")

	if err := os.MkdirAll(remotePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(remote): %v", err)
	}
	if err := os.MkdirAll(workTree, 0o755); err != nil {
		t.Fatalf("MkdirAll(worktree): %v", err)
	}

	runGitCmd(t, remotePath, "init", "--bare")
	runGitCmd(t, workTree, "init")
	runGitCmd(t, workTree, "config", "user.email", "test@example.com")
	runGitCmd(t, workTree, "config", "user.name", "Test")
	runGitCmd(t, workTree, "commit", "--allow-empty", "-m", "initial")
	runGitCmd(t, workTree, "branch", "-M", "main")
	runGitCmd(t, workTree, "remote", "add", "origin", remotePath)
	runGitCmd(t, workTree, "push", "-u", "origin", "main")

	cloneDir := filepath.Join(workspaceRoot, strings.ReplaceAll(repo, "/", "-"))
	runGitCmd(t, workspaceRoot, "clone", remotePath, cloneDir)
}

func writeEnabledConfig(t *testing.T, repo string) string {
	t.Helper()

	f, err := os.CreateTemp("", "runner-cmd-enabled-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp(): %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })

	content := `
repos:
  - name: ` + repo + `
    environments: [dev]
    enabled: true
`
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString(): %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
	return f.Name()
}

func writeStubBin(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(%s): %v", name, err)
	}
	return path
}

func withPatchedPath(t *testing.T, prefix string) {
	t.Helper()
	origPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", prefix+string(os.PathListSeparator)+origPath); err != nil {
		t.Fatalf("Setenv(PATH): %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe(): %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })
	defer r.Close()

	fn()
	_ = w.Close()

	var stdout bytes.Buffer
	_, _ = stdout.ReadFrom(r)
	return stdout.String()
}

func TestPlanAllCmd_RunE_ChangesOutput(t *testing.T) {
	restoreCommandGlobals(t)

	repo := "acme/repo"
	workspace := t.TempDir()
	setupWorkspaceRepo(t, workspace, repo)
	binDir := t.TempDir()
	writeStubBin(t, binDir, "terraform", "#!/bin/sh\nexit 2\n")
	withPatchedPath(t, binDir)

	flagConfig = writeEnabledConfig(t, repo)
	flagLogLevel = "info"
	flagWorkspace = workspace

	out := captureStdout(t, func() {
		planAllCmd.SetContext(context.Background())
		if err := planAllCmd.RunE(planAllCmd, nil); err != nil {
			t.Fatalf("RunE() unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "CHANGES") {
		t.Fatalf("expected CHANGES output, got %q", out)
	}
}

func TestPlanAllCmd_RunE_OkOutput(t *testing.T) {
	restoreCommandGlobals(t)

	repo := "acme/repo"
	workspace := t.TempDir()
	setupWorkspaceRepo(t, workspace, repo)
	binDir := t.TempDir()
	writeStubBin(t, binDir, "terraform", "#!/bin/sh\nexit 0\n")
	withPatchedPath(t, binDir)

	flagConfig = writeEnabledConfig(t, repo)
	flagLogLevel = "info"
	flagWorkspace = workspace

	out := captureStdout(t, func() {
		planAllCmd.SetContext(context.Background())
		if err := planAllCmd.RunE(planAllCmd, nil); err != nil {
			t.Fatalf("RunE() unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "ok") {
		t.Fatalf("expected ok output, got %q", out)
	}
}

func TestApplyAllCmd_RunE_SuccessOutput(t *testing.T) {
	restoreCommandGlobals(t)

	repo := "acme/repo"
	workspace := t.TempDir()
	setupWorkspaceRepo(t, workspace, repo)
	binDir := t.TempDir()
	writeStubBin(t, binDir, "terraform", "#!/bin/sh\nexit 0\n")
	withPatchedPath(t, binDir)

	flagConfig = writeEnabledConfig(t, repo)
	flagLogLevel = "info"
	flagWorkspace = workspace
	applyAllConfirm = true

	out := captureStdout(t, func() {
		applyAllCmd.SetContext(context.Background())
		if err := applyAllCmd.RunE(applyAllCmd, nil); err != nil {
			t.Fatalf("RunE() unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "ok") {
		t.Fatalf("expected success output, got %q", out)
	}
}

func TestSyncTemplateCmd_RunE_SuccessOutput(t *testing.T) {
	restoreCommandGlobals(t)

	repo := "acme/repo"
	workspace := t.TempDir()
	setupWorkspaceRepo(t, workspace, repo)
	templateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(templateDir, "README.md"), []byte("template"), 0o644); err != nil {
		t.Fatalf("WriteFile(template): %v", err)
	}

	flagConfig = writeEnabledConfig(t, repo)
	flagLogLevel = "info"
	flagWorkspace = workspace
	syncTemplateDir = templateDir

	out := captureStdout(t, func() {
		syncTemplateCmd.SetContext(context.Background())
		if err := syncTemplateCmd.RunE(syncTemplateCmd, nil); err != nil {
			t.Fatalf("RunE() unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "applied=") {
		t.Fatalf("expected sync output, got %q", out)
	}
}

func TestValidateCmd_RunE_SuccessOutput(t *testing.T) {
	restoreCommandGlobals(t)

	binDir := t.TempDir()
	writeStubBin(t, binDir, "platform-guardian", "#!/bin/sh\nprintf '%s' '{\"passed\":true,\"failures\":[]}'\n")
	withPatchedPath(t, binDir)

	flagConfig = writeEnabledConfig(t, "acme/repo")
	flagLogLevel = "info"
	flagWorkspace = t.TempDir()

	out := captureStdout(t, func() {
		validateCmd.SetContext(context.Background())
		if err := validateCmd.RunE(validateCmd, nil); err != nil {
			t.Fatalf("RunE() unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "ok") {
		t.Fatalf("expected validate output, got %q", out)
	}
}

func TestPlanAllCmd_RunE_FailureExitsOne(t *testing.T) {
	repo := "acme/repo"
	workspace := t.TempDir()
	setupWorkspaceRepo(t, workspace, repo)
	binDir := t.TempDir()
	writeStubBin(t, binDir, "terraform", "#!/bin/sh\nprintf '%s\n' 'boom' 1>&2\nexit 1\n")
	configPath := writeEnabledConfig(t, repo)

	cmd := exec.Command(os.Args[0], "-test.run=TestCommandFailureSubprocess")
	cmd.Env = append(os.Environ(),
		"RUNNER_CMD_FAILURE_MODE=plan",
		"RUNNER_CMD_CONFIG="+configPath,
		"RUNNER_CMD_WORKSPACE="+workspace,
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected subprocess to exit non-zero")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("unexpected subprocess error: %v", err)
	}
}

func TestValidateCmd_RunE_FailureExitsOne(t *testing.T) {
	binDir := t.TempDir()
	writeStubBin(t, binDir, "platform-guardian", "#!/bin/sh\nprintf '%s' '{\"passed\":false,\"failures\":[{\"id\":1}]}'\n")
	configPath := writeEnabledConfig(t, "acme/repo")

	cmd := exec.Command(os.Args[0], "-test.run=TestCommandFailureSubprocess")
	cmd.Env = append(os.Environ(),
		"RUNNER_CMD_FAILURE_MODE=validate",
		"RUNNER_CMD_CONFIG="+configPath,
		"RUNNER_CMD_WORKSPACE="+t.TempDir(),
		"RUNNER_CMD_RULES="+t.TempDir(),
		"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	err := cmd.Run()
	if err == nil {
		t.Fatal("expected subprocess to exit non-zero")
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("unexpected subprocess error: %v", err)
	}
}
