package runner

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ffreis/platform-runner/internal/config"
	"github.com/ffreis/platform-runner/internal/executor"
)

// panickyExecutor panics on Plan/Apply for any repo whose workspace dir
// contains "boom"; otherwise returns a zero-change result. Counters use
// atomics because Plan is called from multiple worker goroutines.
type panickyExecutor struct {
	plans  atomic.Int32
	panics atomic.Int32
}

func (p *panickyExecutor) Plan(_ context.Context, opts executor.ExecOptions) (*executor.ExecResult, error) {
	p.plans.Add(1)
	if strings.Contains(opts.WorkDir, "boom") {
		p.panics.Add(1)
		panic("simulated executor panic in " + opts.WorkDir)
	}
	return &executor.ExecResult{ExitCode: 0}, nil
}

func (p *panickyExecutor) Apply(_ context.Context, opts executor.ExecOptions) (*executor.ExecResult, error) {
	if strings.Contains(opts.WorkDir, "boom") {
		panic("simulated apply panic")
	}
	return &executor.ExecResult{ExitCode: 0}, nil
}

// TestPlanAll_PanicInStepBecomesFailedResult is the panic-recovery contract
// test for the runner worker pool. Before the fix a panic in executor.Plan
// took down the entire runner process via the Go runtime's
// panic-in-goroutine semantics. After the fix:
//   - The panicking task produces a RepoResult with Status=Failed and an
//     ErrMsg prefixed with "panic:".
//   - Other tasks in the same worker pool continue to completion.
//   - PlanAll returns normally (no top-level error).
func TestPlanAllPanicInStepBecomesFailedResult(t *testing.T) {
	cfg := []config.RepoConfig{
		// One repo whose workspace-dir name contains "boom" so the panicky
		// executor triggers on it. The repo name itself becomes the directory
		// name (after slash-replacement) so include "boom" in the name.
		{Name: "example/boom-repo", Environments: []string{testEnvDev}, Enabled: true},
		// Two healthy repos that must still produce success results.
		{Name: testRepoA, Environments: []string{testEnvDev}, Enabled: true},
		{Name: testRepoB, Environments: []string{testEnvProd}, Enabled: true},
	}

	mock := &panickyExecutor{}
	wsDir := t.TempDir()
	preCreateWorkspace(t, wsDir, cfg)

	r := NewRunner(cfg, mock, RunnerOptions{
		Workspace:   wsDir,
		Concurrency: 3,
		Log:         testLogger(),
	})

	report, err := r.PlanAll(context.Background())
	if err != nil {
		t.Fatalf("PlanAll returned top-level error after worker panic: %v", err)
	}
	if report == nil {
		t.Fatal("nil report")
	}
	if len(report.Results) != 3 {
		t.Fatalf("expected 3 results (one per repo×env), got %d", len(report.Results))
	}

	// Find the panicking repo's result.
	var panicResult *RepoResult
	var successCount int
	for i := range report.Results {
		res := &report.Results[i]
		if strings.Contains(res.Repo, "boom") {
			panicResult = res
		} else if res.Status == RepoStatusSuccess {
			successCount++
		}
	}

	if panicResult == nil {
		t.Fatal("expected a result for the panicking repo, got none")
	}
	if panicResult.Status != RepoStatusFailed {
		t.Errorf("panicking repo: status = %s, want Failed", panicResult.Status)
	}
	if !strings.HasPrefix(panicResult.ErrMsg, panicErrPrefix) {
		t.Errorf("panicking repo ErrMsg = %q, want prefix %q", panicResult.ErrMsg, panicErrPrefix)
	}

	if successCount != 2 {
		t.Errorf("healthy repos: %d succeeded, want 2 (one panic must not abort the pool)", successCount)
	}
	if mock.panics.Load() == 0 {
		t.Error("panicky executor was never invoked on the boom repo")
	}
}

// TestRunTaskSafely_RecoversPanic is a focused unit test for the recovery
// helper itself. It verifies that runTaskSafely converts any panic into a
// Failed RepoResult carrying the repo identity, env, and the panic value in
// ErrMsg — without re-panicking.
func TestRunTaskSafelyRecoversPanic(t *testing.T) {
	r := NewRunner(nil, nil, RunnerOptions{Log: testLogger()})

	tk := task{
		repo: config.RepoConfig{Name: "org/x"},
		env:  "prod",
	}

	fn := func(context.Context, task) RepoResult {
		panic("kaboom")
	}

	res := r.runTaskSafely(context.Background(), tk, fn)

	if res.Repo != "org/x" {
		t.Errorf("Repo = %q, want org/x", res.Repo)
	}
	if res.Env != "prod" {
		t.Errorf("Env = %q, want prod", res.Env)
	}
	if res.Status != RepoStatusFailed {
		t.Errorf("Status = %s, want Failed", res.Status)
	}
	if !strings.Contains(res.ErrMsg, "kaboom") {
		t.Errorf("ErrMsg = %q, expected to contain the panic value", res.ErrMsg)
	}
}
