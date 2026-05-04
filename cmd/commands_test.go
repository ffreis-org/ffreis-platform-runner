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
	origUI := flagUI
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
		flagUI = origUI
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
	flagUI = "plain"

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
	flagUI = "plain"

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
	flagUI = "plain"

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
	flagUI = "plain"

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
	flagUI = "plain"

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
	flagUI = "plain"

	validateCmd.SetContext(context.Background())
	if err := validateCmd.RunE(validateCmd, nil); err != nil {
		t.Fatalf("RunE() unexpected error: %v", err)
	}
}

func TestDeliverFlemmingCmd_RunE_RequiresConfirm(t *testing.T) {
	restoreCommandGlobals(t)

	flagLogLevel = "info"
	flagWorkspace = t.TempDir()
	flagUI = "plain"
	deliverFlemmingConfirm = false

	deliverFlemmingCmd.SetContext(context.Background())
	if err := deliverFlemmingCmd.RunE(deliverFlemmingCmd, nil); err == nil || err.Error() != "--confirm is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeliverFlemmingCmd_RunE_InvokesMakeWithResolvedRepos(t *testing.T) {
	restoreCommandGlobals(t)

	workspace := t.TempDir()
	infraRepo := "acme/flemming-infra"
	websiteRepo := "acme/flemming-website"
	compilerRepo := "acme/website-compiler"
	packerRepo := "acme/website-packer"

	setupWorkspaceRepo(t, workspace, infraRepo)
	setupWorkspaceRepo(t, workspace, websiteRepo)
	setupWorkspaceRepo(t, workspace, compilerRepo)
	setupWorkspaceRepo(t, workspace, packerRepo)

	binDir := t.TempDir()
	traceFile := filepath.Join(t.TempDir(), "make-trace.txt")
	writeStubBin(t, binDir, "make", "#!/bin/sh\nset -eu\necho \"$PWD:$@\" >> \"$TRACE_FILE\"\n")
	withPatchedPath(t, binDir)
	t.Setenv("TRACE_FILE", traceFile)

	flagLogLevel = "info"
	flagWorkspace = workspace
	flagUI = "plain"
	deliverFlemmingConfirm = true
	deliverFlemmingInfraRepo = infraRepo
	deliverFlemmingWebsiteRepo = websiteRepo
	deliverFlemmingCompilerRepo = compilerRepo
	deliverFlemmingPackerRepo = packerRepo
	deliverFlemmingEnv = "prod"
	deliverFlemmingOrg = "ffreis"
	deliverFlemmingProfile = "bootstrap-admin"
	deliverFlemmingDomainName = "ffreis.com"
	deliverFlemmingWWWDomainName = "www.ffreis.com"
	deliverFlemmingRoute53ZoneName = "ffreis.com"
	deliverFlemmingPublishPrefix = ""

	var stdout bytes.Buffer
	deliverFlemmingCmd.SetContext(context.Background())
	deliverFlemmingCmd.SetOut(&stdout)
	deliverFlemmingCmd.SetErr(&stdout)
	if err := deliverFlemmingCmd.RunE(deliverFlemmingCmd, nil); err != nil {
		t.Fatalf("RunE() unexpected error: %v", err)
	}

	trace, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("ReadFile(trace): %v", err)
	}
	traceText := string(trace)
	if !strings.Contains(traceText, filepath.Join(workspace, "acme-flemming-infra")+":go-deliver") {
		t.Fatalf("unexpected make trace: %s", traceText)
	}
	if !strings.Contains(traceText, "WEBSITE_ROOT="+filepath.Join(workspace, "acme-flemming-website")) {
		t.Fatalf("missing website root in make trace: %s", traceText)
	}
	if !strings.Contains(traceText, "DOMAIN_NAME=ffreis.com") {
		t.Fatalf("missing domain override in make trace: %s", traceText)
	}
	if !strings.Contains(stdout.String(), "flemming delivery completed through platform-runner") {
		t.Fatalf("unexpected output: %s", stdout.String())
	}

	deliverFlemmingConfirm = false
	deliverFlemmingInfraRepo = "FelipeFuhr/ffreis-flemming-infra"
	deliverFlemmingWebsiteRepo = "FelipeFuhr/flemming-website"
	deliverFlemmingCompilerRepo = "FelipeFuhr/ffreis-website-compiler"
	deliverFlemmingPackerRepo = "FelipeFuhr/ffreis-website-packer"
	deliverFlemmingEnv = "prod"
	deliverFlemmingOrg = ""
	deliverFlemmingProfile = ""
	deliverFlemmingDomainName = ""
	deliverFlemmingWWWDomainName = ""
	deliverFlemmingRoute53ZoneName = ""
	deliverFlemmingPublishPrefix = ""
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
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

func assertExitError(t *testing.T, err error, wantCode int, wantMessage string) {
	t.Helper()

	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected ExitError, got %T (%v)", err, err)
	}
	if exitErr.Code != wantCode {
		t.Fatalf("ExitError.Code: got %d, want %d", exitErr.Code, wantCode)
	}
	if exitErr.Error() != wantMessage {
		t.Fatalf("ExitError.Error(): got %q, want %q", exitErr.Error(), wantMessage)
	}
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
	flagUI = "plain"

	out := captureStdout(t, func() {
		planAllCmd.SetContext(context.Background())
		if err := planAllCmd.RunE(planAllCmd, nil); err != nil {
			t.Fatalf("RunE() unexpected error: %v", err)
		}
	})
	if !strings.Contains(out, "warn") {
		t.Fatalf("expected warn output, got %q", out)
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
	flagUI = "plain"

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
	flagUI = "plain"

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
	flagUI = "plain"

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
	flagUI = "plain"

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
	restoreCommandGlobals(t)

	repo := "acme/repo"
	workspace := t.TempDir()
	setupWorkspaceRepo(t, workspace, repo)
	binDir := t.TempDir()
	writeStubBin(t, binDir, "terraform", "#!/bin/sh\nprintf '%s\n' 'boom' 1>&2\nexit 1\n")
	withPatchedPath(t, binDir)

	flagConfig = writeEnabledConfig(t, repo)
	flagLogLevel = "info"
	flagWorkspace = workspace
	flagUI = "plain"

	planAllCmd.SetContext(context.Background())
	err := planAllCmd.RunE(planAllCmd, nil)
	assertExitError(t, err, exitError, "one or more repositories failed planning")
}

func TestValidateCmd_RunE_FailureExitsOne(t *testing.T) {
	restoreCommandGlobals(t)

	binDir := t.TempDir()
	writeStubBin(t, binDir, "platform-guardian", "#!/bin/sh\nprintf '%s' '{\"passed\":false,\"failures\":[{\"id\":1}]}'\n")
	withPatchedPath(t, binDir)

	flagConfig = writeEnabledConfig(t, "acme/repo")
	flagLogLevel = "info"
	flagWorkspace = t.TempDir()
	validateRulesDir = t.TempDir()
	flagUI = "plain"

	validateCmd.SetContext(context.Background())
	err := validateCmd.RunE(validateCmd, nil)
	assertExitError(t, err, exitError, "one or more repositories failed validation")
}
