package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ffreis/platform-runner/internal/config"
	"github.com/ffreis/platform-runner/internal/executor"
	"github.com/ffreis/platform-runner/internal/guardian"
	"github.com/ffreis/platform-runner/internal/logging"
	"github.com/ffreis/platform-runner/internal/repos"
	"github.com/ffreis/platform-runner/internal/template"
)

const msgWorkspaceEnsureFailed = "workspace ensure failed"

// RunnerOptions configures a Runner instance.
type RunnerOptions struct {
	TemplateDir string
	RulesDir    string
	Workspace   string
	Token       string
	Concurrency int
	DryRun      bool
	Log         *zap.Logger
}

// Runner orchestrates per-repo actions using a worker pool.
type Runner struct {
	cfg         []config.RepoConfig
	executor    executor.Executor
	templateDir string
	rulesDir    string
	workspace   string
	token       string
	concurrency int
	dryRun      bool
	log         *zap.Logger
}

// NewRunner creates a Runner with the given configuration and options.
func NewRunner(cfg []config.RepoConfig, exec executor.Executor, opts RunnerOptions) *Runner {
	log := opts.Log
	if log == nil {
		log = zap.NewNop()
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	return &Runner{
		cfg:         cfg,
		executor:    exec,
		templateDir: opts.TemplateDir,
		rulesDir:    opts.RulesDir,
		workspace:   opts.Workspace,
		token:       opts.Token,
		concurrency: concurrency,
		dryRun:      opts.DryRun,
		log:         log,
	}
}

// task represents a unit of work for the worker pool.
type task struct {
	repo config.RepoConfig
	env  string
}

// taskFunc is the function executed per task, returning a RepoResult.
type taskFunc func(ctx context.Context, t task) RepoResult

// runPool dispatches tasks through a worker pool and collects results.
func (r *Runner) runPool(ctx context.Context, tasks []task, fn taskFunc) []RepoResult {
	taskCh := make(chan task, len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	resultCh := make(chan RepoResult, len(tasks))
	var wg sync.WaitGroup

	for i := 0; i < r.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskCh {
				res := fn(ctx, t)
				resultCh <- res
			}
		}()
	}

	wg.Wait()
	close(resultCh)

	var results []RepoResult
	for res := range resultCh {
		results = append(results, res)
	}
	return results
}

// buildTasks returns tasks for all enabled repos × environments.
// A repo with an empty environments list is skipped with a warning — there is
// no implicit fallback environment, because silently operating without an
// explicit env would risk touching the wrong Terraform workspace.
func (r *Runner) buildTasks() []task {
	var tasks []task
	for _, rc := range r.cfg {
		if !rc.Enabled {
			continue
		}
		if len(rc.Environments) == 0 {
			r.log.Warn("repo has no environments configured — skipping",
				zap.String("repo", rc.Name),
			)
			continue
		}
		for _, env := range rc.Environments {
			tasks = append(tasks, task{repo: rc, env: env})
		}
	}
	return tasks
}

// newReport creates a RunReport and records start time.
func newReport(action string) *RunReport {
	return &RunReport{
		Action:    action,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// PlanAll runs terraform plan for all enabled repos × environments.
func (r *Runner) PlanAll(ctx context.Context) (*RunReport, error) {
	report := newReport(actionPlan)
	tasks := r.buildTasks()

	results := r.runPool(ctx, tasks, func(ctx context.Context, t task) RepoResult {
		log := logging.WithRepo(r.log, t.repo.Name, t.env)
		log.Info("running plan")

		w := &repos.Workspace{
			Repo:    t.repo.Name,
			RootDir: r.workspace,
			Token:   r.token,
		}
		if err := w.Ensure(ctx); err != nil {
			log.Error(msgWorkspaceEnsureFailed, zap.Error(err))
			return RepoResult{
				Repo:   t.repo.Name,
				Env:    t.env,
				Status: RepoStatusFailed,
				Action: actionPlan,
				ErrMsg: err.Error(),
			}
		}

		workDir := w.Dir()
		if t.repo.TFWorkingDir != "" {
			workDir = fmt.Sprintf("%s/%s", workDir, t.repo.TFWorkingDir)
		}

		opts := executor.ExecOptions{
			WorkDir: workDir,
			TFVars:  t.repo.TFVars,
			DryRun:  r.dryRun,
		}

		res, err := r.executor.Plan(ctx, opts)
		if err != nil {
			log.Error("plan failed", zap.Error(err))
			return RepoResult{
				Repo:   t.repo.Name,
				Env:    t.env,
				Status: RepoStatusFailed,
				Action: actionPlan,
				ErrMsg: err.Error(),
			}
		}

		log.Info("plan complete", zap.Bool("has_changes", res.HasChanges))
		return RepoResult{
			Repo:       t.repo.Name,
			Env:        t.env,
			Status:     RepoStatusSuccess,
			Action:     actionPlan,
			Output:     res.Stdout,
			HasChanges: res.HasChanges,
		}
	})

	report.Results = results
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return report, nil
}

// ApplyAll runs terraform apply for all enabled repos × environments.
func (r *Runner) ApplyAll(ctx context.Context, confirm bool) (*RunReport, error) {
	if !confirm {
		return nil, errors.New(errApplyConfirmRequired)
	}

	report := newReport(actionApply)
	tasks := r.buildTasks()

	results := r.runPool(ctx, tasks, func(ctx context.Context, t task) RepoResult {
		log := logging.WithRepo(r.log, t.repo.Name, t.env)
		log.Info("running apply")

		w := &repos.Workspace{
			Repo:    t.repo.Name,
			RootDir: r.workspace,
			Token:   r.token,
		}
		if err := w.Ensure(ctx); err != nil {
			log.Error(msgWorkspaceEnsureFailed, zap.Error(err))
			return RepoResult{
				Repo:   t.repo.Name,
				Env:    t.env,
				Status: RepoStatusFailed,
				Action: actionApply,
				ErrMsg: err.Error(),
			}
		}

		workDir := w.Dir()
		if t.repo.TFWorkingDir != "" {
			workDir = fmt.Sprintf("%s/%s", workDir, t.repo.TFWorkingDir)
		}

		opts := executor.ExecOptions{
			WorkDir: workDir,
			TFVars:  t.repo.TFVars,
			DryRun:  r.dryRun,
			Confirm: confirm,
		}

		res, err := r.executor.Apply(ctx, opts)
		if err != nil {
			log.Error("apply failed", zap.Error(err))
			return RepoResult{
				Repo:   t.repo.Name,
				Env:    t.env,
				Status: RepoStatusFailed,
				Action: actionApply,
				ErrMsg: err.Error(),
			}
		}

		log.Info("apply complete")
		return RepoResult{
			Repo:   t.repo.Name,
			Env:    t.env,
			Status: RepoStatusSuccess,
			Action: actionApply,
			Output: res.Stdout,
		}
	})

	report.Results = results
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return report, nil
}

// SyncTemplate syncs the template directory to all enabled repos.
func (r *Runner) SyncTemplate(ctx context.Context) (*RunReport, error) {
	report := newReport(actionSyncTemplate)

	// For template sync, we iterate repos (not repo×env).
	var results []RepoResult
	sem := make(chan struct{}, r.concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, rc := range r.cfg {
		if !rc.Enabled {
			continue
		}

		rc := rc // capture
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			log := logging.WithRepo(r.log, rc.Name, "")
			log.Info("syncing template")

			w := &repos.Workspace{
				Repo:    rc.Name,
				RootDir: r.workspace,
				Token:   r.token,
			}
			if err := w.Ensure(ctx); err != nil {
				log.Error(msgWorkspaceEnsureFailed, zap.Error(err))
				mu.Lock()
				results = append(results, RepoResult{
					Repo:   rc.Name,
					Status: RepoStatusFailed,
					Action: actionSyncTemplate,
					ErrMsg: err.Error(),
				})
				mu.Unlock()
				return
			}

			syncResult, err := template.Sync(ctx, template.SyncOptions{
				TemplateDir: r.templateDir,
				RepoDir:     w.Dir(),
				DryRun:      r.dryRun,
				Log:         log,
			})
			if err != nil {
				log.Error("sync failed", zap.Error(err))
				mu.Lock()
				results = append(results, RepoResult{
					Repo:   rc.Name,
					Status: RepoStatusFailed,
					Action: actionSyncTemplate,
					ErrMsg: err.Error(),
				})
				mu.Unlock()
				return
			}

			log.Info("sync complete",
				zap.Int("applied", len(syncResult.Applied)),
				zap.Int("skipped", len(syncResult.Skipped)),
				zap.Int("unchanged", len(syncResult.Unchanged)),
			)

			mu.Lock()
			results = append(results, RepoResult{
				Repo:   rc.Name,
				Status: RepoStatusSuccess,
				Action: actionSyncTemplate,
				Output: fmt.Sprintf("applied=%d skipped=%d unchanged=%d",
					len(syncResult.Applied), len(syncResult.Skipped), len(syncResult.Unchanged)),
			})
			mu.Unlock()
		}()
	}

	wg.Wait()
	report.Results = results
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return report, nil
}

// Validate runs platform-guardian checks for all enabled repos.
func (r *Runner) Validate(ctx context.Context) (*RunReport, error) {
	report := newReport(actionValidate)

	gr := &guardian.GuardianRunner{
		Binary:   "platform-guardian",
		RulesDir: r.rulesDir,
		Token:    r.token,
	}

	var results []RepoResult
	sem := make(chan struct{}, r.concurrency)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, rc := range r.cfg {
		if !rc.Enabled {
			continue
		}

		rc := rc // capture
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			log := logging.WithRepo(r.log, rc.Name, "")
			log.Info("running validate")

			guardianResult, err := gr.Check(ctx, rc.Name)
			if err != nil {
				log.Error("guardian check error", zap.Error(err))
			mu.Lock()
			results = append(results, RepoResult{
				Repo:   rc.Name,
				Status: RepoStatusFailed,
				Action: actionValidate,
				ErrMsg: err.Error(),
			})
			mu.Unlock()
			return
		}

			status := RepoStatusSuccess
			if !guardianResult.Passed {
				status = RepoStatusFailed
			}

			mu.Lock()
			results = append(results, RepoResult{
				Repo:   rc.Name,
				Status: status,
				Action: actionValidate,
				Output: guardianResult.Output,
				ErrMsg: guardianResult.ErrMsg,
			})
			mu.Unlock()
		}()
	}

	wg.Wait()
	report.Results = results
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return report, nil
}
