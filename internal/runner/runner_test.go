package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ffreis/platform-runner/internal/config"
	"github.com/ffreis/platform-runner/internal/executor"
	"github.com/ffreis/platform-runner/internal/logging"
	"github.com/ffreis/platform-cli/pkg/ui"
)

const (
	testRepoA = "example-org/repo-a"
	testRepoB = "example-org/repo-b"

	testEnvDev  = "dev"
	testEnvProd = "prod"

	gitTestUserEmail    = "test@test.com"
	gitTestUserName     = "Test"
	gitInitialCommitMsg = "init"
	gitDefaultBranch    = "master"
)

// mockExecutor is a test double for executor.Executor.
type mockExecutor struct {
	planResult  *executor.ExecResult
	planErr     error
	applyResult *executor.ExecResult
	applyErr    error
}

func (m *mockExecutor) Plan(_ context.Context, _ executor.ExecOptions) (*executor.ExecResult, error) {
	return m.planResult, m.planErr
}

func (m *mockExecutor) Apply(_ context.Context, opts executor.ExecOptions) (*executor.ExecResult, error) {
	if !opts.Confirm {
		return nil, errors.New(errApplyConfirmRequired)
	}
	if opts.DryRun {
		return &executor.ExecResult{}, nil
	}
	return m.applyResult, m.applyErr
}

// countingMockExecutor delegates Plan to a function.
type countingMockExecutor struct {
	planFn func(opts executor.ExecOptions) (*executor.ExecResult, error)
}

func (m *countingMockExecutor) Plan(_ context.Context, opts executor.ExecOptions) (*executor.ExecResult, error) {
	return m.planFn(opts)
}

func (m *countingMockExecutor) Apply(_ context.Context, opts executor.ExecOptions) (*executor.ExecResult, error) {
	if !opts.Confirm {
		return nil, errors.New(errApplyConfirmRequired)
	}
	return &executor.ExecResult{}, nil
}

type captureProgressReporter struct {
	messages []string
}

func (c *captureProgressReporter) Report(kind, label, message string) {
	c.messages = append(c.messages, kind+"|"+label+"|"+message)
}

func testLogger() *slog.Logger {
	return logging.Nop()
}

func twoEnabledRepos() []config.RepoConfig {
	return []config.RepoConfig{
		{Name: testRepoA, Environments: []string{testEnvDev}, Enabled: true},
		{Name: testRepoB, Environments: []string{testEnvProd}, Enabled: true},
	}
}

// repoSafeName converts "org/repo" to "org-repo".
func repoSafeName(repo string) string {
	return strings.ReplaceAll(repo, "/", "-")
}

// runCmd is a small helper to run git commands, fataling on error.
func runCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("runCmd %v in %s: %v\n%s", args, dir, err, out)
	}
}

// setupLocalRemote creates a bare git repo (the "remote") and clones it into the
// workspace directory for the given repo. This allows Workspace.Ensure to run
// git fetch + git reset without network access.
func setupLocalRemote(t *testing.T, baseDir, wsDir string, rc config.RepoConfig) {
	t.Helper()

	// Create the bare "remote" repo.
	remotePath := baseDir + "/remotes/" + repoSafeName(rc.Name) + ".git"
	if err := os.MkdirAll(remotePath, 0o750); err != nil {
		t.Fatalf("mkdir remote: %v", err)
	}
	runCmd(t, remotePath, "git", "init", "--bare")

	// Create a temp work tree so we can make an initial commit.
	workTree := baseDir + "/worktree/" + repoSafeName(rc.Name)
	if err := os.MkdirAll(workTree, 0o750); err != nil {
		t.Fatalf("mkdir worktree: %v", err)
	}
	runCmd(t, workTree, "git", "init")
	runCmd(t, workTree, "git", "config", "user.email", gitTestUserEmail)
	runCmd(t, workTree, "git", "config", "user.name", gitTestUserName)
	runCmd(t, workTree, "git", "commit", "--allow-empty", "-m", gitInitialCommitMsg)
	runCmd(t, workTree, "git", "remote", "add", "origin", remotePath)
	runCmd(t, workTree, "git", "push", "origin", "HEAD:"+gitDefaultBranch)

	// Clone from the local bare remote into the expected workspace location.
	cloneTarget := wsDir + "/" + repoSafeName(rc.Name)
	runCmd(t, wsDir, "git", "clone", remotePath, cloneTarget)
}

// preCreateWorkspace sets up local git repos with valid origins in the workspace.
func preCreateWorkspace(t *testing.T, wsDir string, repos []config.RepoConfig) {
	t.Helper()
	baseDir := t.TempDir()
	for _, rc := range repos {
		setupLocalRemote(t, baseDir, wsDir, rc)
	}
}

func TestPlanAll_AllSuccess(t *testing.T) {
	mock := &mockExecutor{
		planResult: &executor.ExecResult{ExitCode: 0, HasChanges: false},
	}

	cfg := twoEnabledRepos()
	wsDir := t.TempDir()
	preCreateWorkspace(t, wsDir, cfg)

	r := NewRunner(cfg, mock, RunnerOptions{
		Workspace:   wsDir,
		Concurrency: 2,
		Log:         testLogger(),
	})

	report, err := r.PlanAll(context.Background())
	if err != nil {
		t.Fatalf("PlanAll() unexpected top-level error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if len(report.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(report.Results))
	}
	if report.HasFailures() {
		for _, res := range report.Results {
			if res.Status == RepoStatusFailed {
				t.Logf("failure: repo=%s err=%s", res.Repo, res.ErrMsg)
			}
		}
		t.Errorf("expected no failures")
	}
}

func TestPlanAll_OneFailure_OthersRun(t *testing.T) {
	callCount := 0
	mock := &countingMockExecutor{
		planFn: func(_ executor.ExecOptions) (*executor.ExecResult, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("simulated plan failure")
			}
			return &executor.ExecResult{ExitCode: 0}, nil
		},
	}

	cfg := []config.RepoConfig{
		{Name: testRepoA, Environments: []string{testEnvDev}, Enabled: true},
		{Name: testRepoB, Environments: []string{testEnvProd}, Enabled: true},
	}

	wsDir := t.TempDir()
	preCreateWorkspace(t, wsDir, cfg)

	r := NewRunner(cfg, mock, RunnerOptions{
		Workspace:   wsDir,
		Concurrency: 1, // sequential so failure ordering is deterministic
		Log:         testLogger(),
	})

	report, err := r.PlanAll(context.Background())
	if err != nil {
		t.Fatalf("PlanAll() unexpected top-level error: %v", err)
	}
	if len(report.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(report.Results))
	}
	if !report.HasFailures() {
		t.Errorf("expected HasFailures=true (one plan failed)")
	}
}

func TestPlanAll_SkipsDisabledRepos(t *testing.T) {
	mock := &mockExecutor{
		planResult: &executor.ExecResult{ExitCode: 0},
	}

	cfg := []config.RepoConfig{
		{Name: "example-org/enabled-repo", Environments: []string{testEnvDev}, Enabled: true},
		{Name: "example-org/disabled-repo", Environments: []string{testEnvProd}, Enabled: false},
	}

	wsDir := t.TempDir()
	preCreateWorkspace(t, wsDir, []config.RepoConfig{cfg[0]})

	r := NewRunner(cfg, mock, RunnerOptions{
		Workspace:   wsDir,
		Concurrency: 2,
		Log:         testLogger(),
	})

	report, err := r.PlanAll(context.Background())
	if err != nil {
		t.Fatalf("PlanAll() unexpected top-level error: %v", err)
	}

	if len(report.Results) != 1 {
		t.Errorf("expected 1 result (disabled repo skipped), got %d", len(report.Results))
	}
	for _, res := range report.Results {
		if res.Repo == "example-org/disabled-repo" {
			t.Errorf("disabled repo should not appear in results")
		}
	}
}

func TestApplyAll_RequiresConfirm(t *testing.T) {
	mock := &mockExecutor{
		applyResult: &executor.ExecResult{ExitCode: 0},
	}

	r := NewRunner(twoEnabledRepos(), mock, RunnerOptions{
		Workspace:   t.TempDir(),
		Concurrency: 2,
		Log:         testLogger(),
	})

	_, err := r.ApplyAll(context.Background(), false)
	if err == nil {
		t.Fatal("expected error when confirm=false, got nil")
	}
}

func TestNewProgressReporter_UsesPresenterAndWriter(t *testing.T) {
	t.Parallel()

	presenter, err := ui.New("plain")
	if err != nil {
		t.Fatalf("ui.New(): %v", err)
	}

	var out bytes.Buffer
	reporter := newProgressReporter(presenter, &out)
	reporter.Report("warn", "warn", "acme/repo: updating")

	if got := out.String(); got != "[warn] acme/repo: updating\n" {
		t.Fatalf("reporter output: got %q", got)
	}
}

func TestNewProgressReporter_NoopWithoutInteractiveUI(t *testing.T) {
	t.Parallel()

	reporter := newProgressReporter(nil, io.Discard)
	if _, ok := reporter.(noopProgressReporter); !ok {
		t.Fatalf("expected noopProgressReporter, got %T", reporter)
	}
}

func TestRunnerProgress_UsesReporter(t *testing.T) {
	t.Parallel()

	reporter := &captureProgressReporter{}
	r := &Runner{reporter: reporter}

	r.progress("ok", "ok", "acme/repo [dev]", "completed")

	if len(reporter.messages) != 1 {
		t.Fatalf("expected 1 progress message, got %d", len(reporter.messages))
	}
	if got := reporter.messages[0]; got != "ok|ok|acme/repo [dev]: completed" {
		t.Fatalf("unexpected progress message: %q", got)
	}
}

func TestSyncTemplate_DryRun(t *testing.T) {
	mock := &mockExecutor{}

	cfg := []config.RepoConfig{
		{Name: testRepoA, Environments: []string{testEnvDev}, Enabled: true},
	}

	wsDir := t.TempDir()
	templateDir := t.TempDir()
	preCreateWorkspace(t, wsDir, cfg)

	r := NewRunner(cfg, mock, RunnerOptions{
		TemplateDir: templateDir,
		Workspace:   wsDir,
		Concurrency: 1,
		DryRun:      true,
		Log:         testLogger(),
	})

	report, err := r.SyncTemplate(context.Background())
	if err != nil {
		t.Fatalf("SyncTemplate() unexpected error: %v", err)
	}
	if report == nil {
		t.Fatal("expected non-nil report")
	}
	if len(report.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(report.Results))
	}
}

func TestNewRunner_Defaults(t *testing.T) {
	r := NewRunner(nil, &mockExecutor{}, RunnerOptions{})
	if r.concurrency != 5 {
		t.Fatalf("concurrency = %d, want 5", r.concurrency)
	}
	if r.log == nil {
		t.Fatal("expected logger to be initialized")
	}
}

func TestRunnerBuildTasks(t *testing.T) {
	r := NewRunner([]config.RepoConfig{
		{Name: testRepoA, Environments: []string{testEnvDev, testEnvProd}, Enabled: true},
		{Name: testRepoB, Environments: nil, Enabled: true},
		{Name: "example-org/disabled", Environments: []string{testEnvDev}, Enabled: false},
	}, &mockExecutor{}, RunnerOptions{Log: testLogger()})

	tasks := r.buildTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestRunnerRunPool(t *testing.T) {
	r := NewRunner(nil, &mockExecutor{}, RunnerOptions{Concurrency: 2, Log: testLogger()})
	tasks := []task{
		{repo: config.RepoConfig{Name: testRepoA}, env: testEnvDev},
		{repo: config.RepoConfig{Name: testRepoB}, env: testEnvProd},
	}

	results := r.runPool(context.Background(), tasks, func(_ context.Context, t task) RepoResult {
		return RepoResult{Repo: t.repo.Name, Env: t.env, Status: RepoStatusSuccess}
	})
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestNewReport(t *testing.T) {
	report := newReport(actionValidate)
	if report.Action != actionValidate {
		t.Fatalf("Action = %q, want %q", report.Action, actionValidate)
	}
	if report.StartedAt == "" {
		t.Fatal("expected StartedAt to be set")
	}
}

func TestApplyAll_NoTasksWithConfirm(t *testing.T) {
	r := NewRunner([]config.RepoConfig{
		{Name: "example-org/disabled", Environments: []string{testEnvDev}, Enabled: false},
	}, &mockExecutor{}, RunnerOptions{
		Workspace: t.TempDir(),
		Log:       testLogger(),
	})

	report, err := r.ApplyAll(context.Background(), true)
	if err != nil {
		t.Fatalf("ApplyAll() unexpected error: %v", err)
	}
	if len(report.Results) != 0 {
		t.Fatalf("expected no results, got %d", len(report.Results))
	}
}

func TestValidate_UsesGuardianBinaryOnPath(t *testing.T) {
	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "repos.txt")
	stubPath := filepath.Join(tmp, "platform-guardian")
	stub := `#!/bin/sh
set -eu
printf '%s\n' "$3" >> "` + argsFile + `"
printf '%s' '{"passed":true,"failures":[]}'
`
	if err := os.WriteFile(stubPath, []byte(stub), 0o755); err != nil {
		t.Fatalf("WriteFile(stub): %v", err)
	}

	origPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", tmp+string(os.PathListSeparator)+origPath); err != nil {
		t.Fatalf("Setenv(PATH): %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })

	cfg := []config.RepoConfig{
		{Name: testRepoA, Enabled: true},
		{Name: "example-org/disabled", Enabled: false},
	}
	r := NewRunner(cfg, &mockExecutor{}, RunnerOptions{Concurrency: 1, Log: testLogger()})

	report, err := r.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
	if len(report.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(report.Results))
	}
	if report.Results[0].Status != RepoStatusSuccess {
		t.Fatalf("expected success result, got %+v", report.Results[0])
	}
}

func TestValidate_GuardianFailureMarksRepoFailed(t *testing.T) {
	tmp := t.TempDir()
	stubPath := filepath.Join(tmp, "platform-guardian")
	stub := `#!/bin/sh
set -eu
printf '%s\n' 'guardian failed' 1>&2
exit 4
`
	if err := os.WriteFile(stubPath, []byte(stub), 0o755); err != nil {
		t.Fatalf("WriteFile(stub): %v", err)
	}

	origPath := os.Getenv("PATH")
	if err := os.Setenv("PATH", tmp+string(os.PathListSeparator)+origPath); err != nil {
		t.Fatalf("Setenv(PATH): %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })

	r := NewRunner([]config.RepoConfig{{Name: testRepoA, Enabled: true}}, &mockExecutor{}, RunnerOptions{
		Concurrency: 1,
		Log:         testLogger(),
	})

	report, err := r.Validate(context.Background())
	if err != nil {
		t.Fatalf("Validate() unexpected error: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != RepoStatusFailed {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestApplyAll_Success(t *testing.T) {
	mock := &mockExecutor{
		applyResult: &executor.ExecResult{ExitCode: 0, Stdout: "applied"},
	}

	cfg := []config.RepoConfig{
		{Name: testRepoA, Environments: []string{testEnvDev}, Enabled: true},
	}
	wsDir := t.TempDir()
	preCreateWorkspace(t, wsDir, cfg)

	r := NewRunner(cfg, mock, RunnerOptions{
		Workspace:   wsDir,
		Concurrency: 1,
		Log:         testLogger(),
	})

	report, err := r.ApplyAll(context.Background(), true)
	if err != nil {
		t.Fatalf("ApplyAll() unexpected error: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != RepoStatusSuccess {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestSyncTemplate_SuccessAndFailure(t *testing.T) {
	cfg := []config.RepoConfig{
		{Name: testRepoA, Environments: []string{testEnvDev}, Enabled: true},
	}
	wsDir := t.TempDir()
	preCreateWorkspace(t, wsDir, cfg)

	templateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(templateDir, "README.md"), []byte("template"), 0o644); err != nil {
		t.Fatalf("WriteFile(template): %v", err)
	}

	r := NewRunner(cfg, &mockExecutor{}, RunnerOptions{
		TemplateDir: templateDir,
		Workspace:   wsDir,
		Concurrency: 1,
		Log:         testLogger(),
	})

	report, err := r.SyncTemplate(context.Background())
	if err != nil {
		t.Fatalf("SyncTemplate() unexpected error: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != RepoStatusSuccess {
		t.Fatalf("unexpected report: %+v", report)
	}

	r = NewRunner(cfg, &mockExecutor{}, RunnerOptions{
		TemplateDir: filepath.Join(t.TempDir(), "missing"),
		Workspace:   wsDir,
		Concurrency: 1,
		Log:         testLogger(),
	})
	report, err = r.SyncTemplate(context.Background())
	if err != nil {
		t.Fatalf("SyncTemplate() unexpected error: %v", err)
	}
	if len(report.Results) != 1 || report.Results[0].Status != RepoStatusFailed {
		t.Fatalf("unexpected failure report: %+v", report)
	}
}
