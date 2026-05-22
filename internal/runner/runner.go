package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ffreis/platform-runner/internal/config"
	"github.com/ffreis/platform-runner/internal/executor"
	"github.com/ffreis/platform-runner/internal/guardian"
	"github.com/ffreis/platform-runner/internal/logging"
	"github.com/ffreis/platform-runner/internal/repos"
	"github.com/ffreis/platform-runner/internal/template"
	"github.com/ffreis/platform-runner/internal/ui"
)

// panicErrPrefix is the standard prefix for ErrMsg fields produced by a
// recovered worker-goroutine panic.
const panicErrPrefix = "panic: "

// recoverIntoResults converts a recovered panic into a Failed RepoResult and
// appends it under mu. Designed to be called from `defer` in a goroutine that
// writes to a shared results slice (SyncTemplate, Validate). The phase string
// is used only for logging context.
//
// Without this guard, a panic in any per-repo step (executor.Plan,
// template.Sync, guardian.Check) propagates through the goroutine and the
// Go runtime aborts the entire runner process. With it, the panic becomes a
// failed result and the other workers keep processing.
func (r *Runner) recoverIntoResults(phase, repo, action string, mu *sync.Mutex, results *[]RepoResult) {
	rec := recover()
	if rec == nil {
		return
	}
	stack := debug.Stack()
	r.log.Error("recovered panic in "+phase,
		"repo", repo,
		"panic", rec,
		"stack", string(stack),
	)
	mu.Lock()
	*results = append(*results, RepoResult{
		Repo:   repo,
		Status: RepoStatusFailed,
		Action: action,
		ErrMsg: fmt.Sprintf(panicErrPrefix+"%v", rec),
	})
	mu.Unlock()
}

const msgWorkspaceEnsureFailed = "workspace ensure failed"

// RunnerOptions configures a Runner instance.
type RunnerOptions struct {
	TemplateDir  string
	RulesDir     string
	Workspace    string
	ProgressOut  io.Writer
	Token        string
	SafePatterns []string
	Concurrency  int
	DryRun       bool
	Log          *slog.Logger
	UI           *ui.Presenter
}

// Runner orchestrates per-repo actions using a worker pool.
type Runner struct {
	cfg          []config.RepoConfig
	executor     executor.Executor
	templateDir  string
	rulesDir     string
	workspace    string
	token        string
	safePatterns []string
	concurrency  int
	dryRun       bool
	log          *slog.Logger
	ui           *ui.Presenter
	reporter     progressReporter
}

type progressReporter interface {
	Report(kind, label, message string)
}

type noopProgressReporter struct{}

func (noopProgressReporter) Report(string, string, string) {}

type stderrProgressReporter struct {
	ui  *ui.Presenter
	out io.Writer
}

func (r stderrProgressReporter) Report(kind, label, message string) {
	if r.ui == nil || r.out == nil {
		return
	}
	_, _ = io.WriteString(r.out, r.ui.Status(kind, label, message)+"\n")
}

func newProgressReporter(presenter *ui.Presenter, out io.Writer) progressReporter {
	if presenter == nil || !presenter.Interactive() || out == nil {
		return noopProgressReporter{}
	}
	return stderrProgressReporter{ui: presenter, out: out}
}

// NewRunner creates a Runner with the given configuration and options.
func NewRunner(cfg []config.RepoConfig, exec executor.Executor, opts RunnerOptions) *Runner {
	log := opts.Log
	if log == nil {
		log = logging.Nop()
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	return &Runner{
		cfg:          cfg,
		executor:     exec,
		templateDir:  opts.TemplateDir,
		rulesDir:     opts.RulesDir,
		workspace:    opts.Workspace,
		token:        opts.Token,
		safePatterns: opts.SafePatterns,
		concurrency:  concurrency,
		dryRun:       opts.DryRun,
		log:          log,
		ui:           opts.UI,
		reporter:     newProgressReporter(opts.UI, opts.ProgressOut),
	}
}

// task represents a unit of work for the worker pool.
type task struct {
	repo config.RepoConfig
	env  string
}

// taskFunc is the function executed per task, returning a RepoResult.
type taskFunc func(ctx context.Context, t task) RepoResult

// runTaskSafely runs fn(ctx, t) with panic recovery. A panicking task
// produces a Failed RepoResult instead of crashing the runner process. Each
// task is its own recovery scope, so one bad repo cannot disable a worker
// for the rest of its jobCh.
func (r *Runner) runTaskSafely(ctx context.Context, t task, fn taskFunc) (res RepoResult) {
	defer func() {
		if rec := recover(); rec != nil {
			stack := debug.Stack()
			r.log.Error("recovered panic in task",
				"repo", t.repo.Name,
				"env", t.env,
				"panic", rec,
				"stack", string(stack),
			)
			res = RepoResult{
				Repo:   t.repo.Name,
				Env:    t.env,
				Status: RepoStatusFailed,
				ErrMsg: fmt.Sprintf(panicErrPrefix+"%v", rec),
			}
		}
	}()
	return fn(ctx, t)
}

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
				resultCh <- r.runTaskSafely(ctx, t, fn)
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
				"repo", rc.Name,
			)
			r.progress("muted", "skip", rc.Name, "no environments configured")
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
		started := time.Now()
		log := logging.WithRepo(r.log, t.repo.Name, t.env)
		log.Info("running plan")
		r.progress("running", "...", r.repoLabel(t.repo.Name, t.env), "planning")

		w := &repos.Workspace{
			Repo:    t.repo.Name,
			RootDir: r.workspace,
			Token:   r.token,
		}
		if err := w.Ensure(ctx); err != nil {
			log.Error(msgWorkspaceEnsureFailed, "error", err)
			r.progress("error", "fail", r.repoLabel(t.repo.Name, t.env), err.Error())
			return RepoResult{
				Repo:     t.repo.Name,
				Env:      t.env,
				Status:   RepoStatusFailed,
				Action:   actionPlan,
				ErrMsg:   err.Error(),
				Duration: r.formatDuration(started),
			}
		}

		workDir := w.Dir()
		if t.repo.TFWorkingDir != "" {
			workDir = filepath.Join(workDir, t.repo.TFWorkingDir)
		}

		opts := executor.ExecOptions{
			WorkDir: workDir,
			TFVars:  t.repo.TFVars,
			DryRun:  r.dryRun,
		}

		res, err := r.executor.Plan(ctx, opts)
		if err != nil {
			log.Error("plan failed", "error", err)
			r.progress("error", "fail", r.repoLabel(t.repo.Name, t.env), err.Error())
			return RepoResult{
				Repo:     t.repo.Name,
				Env:      t.env,
				Status:   RepoStatusFailed,
				Action:   actionPlan,
				ErrMsg:   err.Error(),
				Duration: r.formatDuration(started),
			}
		}

		log.Info("plan complete", "has_changes", res.HasChanges)
		statusKind := "ok"
		statusLabel := "ok"
		statusDetail := "no changes"
		if res.HasChanges {
			statusKind = "warn"
			statusLabel = "warn"
			statusDetail = "plan contains updates"
		}
		r.progress(statusKind, statusLabel, r.repoLabel(t.repo.Name, t.env), statusDetail+" in "+r.formatDuration(started))
		return RepoResult{
			Repo:       t.repo.Name,
			Env:        t.env,
			Status:     RepoStatusSuccess,
			Action:     actionPlan,
			Output:     res.Stdout,
			HasChanges: res.HasChanges,
			Duration:   r.formatDuration(started),
		}
	})

	report.Results = results
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	report.Duration = r.reportDuration(report.StartedAt, report.FinishedAt)
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
		started := time.Now()
		log := logging.WithRepo(r.log, t.repo.Name, t.env)
		log.Info("running apply")
		r.progress("running", "...", r.repoLabel(t.repo.Name, t.env), "applying")

		w := &repos.Workspace{
			Repo:    t.repo.Name,
			RootDir: r.workspace,
			Token:   r.token,
		}
		if err := w.Ensure(ctx); err != nil {
			log.Error(msgWorkspaceEnsureFailed, "error", err)
			r.progress("error", "fail", r.repoLabel(t.repo.Name, t.env), err.Error())
			return RepoResult{
				Repo:     t.repo.Name,
				Env:      t.env,
				Status:   RepoStatusFailed,
				Action:   actionApply,
				ErrMsg:   err.Error(),
				Duration: r.formatDuration(started),
			}
		}

		workDir := w.Dir()
		if t.repo.TFWorkingDir != "" {
			workDir = filepath.Join(workDir, t.repo.TFWorkingDir)
		}

		opts := executor.ExecOptions{
			WorkDir: workDir,
			TFVars:  t.repo.TFVars,
			DryRun:  r.dryRun,
			Confirm: confirm,
		}

		res, err := r.executor.Apply(ctx, opts)
		if err != nil {
			log.Error("apply failed", "error", err)
			r.progress("error", "fail", r.repoLabel(t.repo.Name, t.env), err.Error())
			return RepoResult{
				Repo:     t.repo.Name,
				Env:      t.env,
				Status:   RepoStatusFailed,
				Action:   actionApply,
				ErrMsg:   err.Error(),
				Duration: r.formatDuration(started),
			}
		}

		log.Info("apply complete")
		r.progress("ok", "ok", r.repoLabel(t.repo.Name, t.env), "applied in "+r.formatDuration(started))
		return RepoResult{
			Repo:     t.repo.Name,
			Env:      t.env,
			Status:   RepoStatusSuccess,
			Action:   actionApply,
			Output:   res.Stdout,
			Duration: r.formatDuration(started),
		}
	})

	report.Results = results
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	report.Duration = r.reportDuration(report.StartedAt, report.FinishedAt)
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

		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer r.recoverIntoResults("SyncTemplate", rc.Name, actionSyncTemplate, &mu, &results)

			started := time.Now()

			log := logging.WithRepo(r.log, rc.Name, "")
			log.Info("syncing template")
			r.progress("running", "...", rc.Name, "syncing template")

			w := &repos.Workspace{
				Repo:    rc.Name,
				RootDir: r.workspace,
				Token:   r.token,
			}
			if err := w.Ensure(ctx); err != nil {
				log.Error(msgWorkspaceEnsureFailed, "error", err)
				r.progress("error", "fail", rc.Name, err.Error())
				mu.Lock()
				results = append(results, RepoResult{
					Repo:     rc.Name,
					Status:   RepoStatusFailed,
					Action:   actionSyncTemplate,
					ErrMsg:   err.Error(),
					Duration: r.formatDuration(started),
				})
				mu.Unlock()
				return
			}

			syncResult, err := template.Sync(ctx, template.SyncOptions{
				TemplateDir:  r.templateDir,
				RepoDir:      w.Dir(),
				SafePatterns: r.safePatterns,
				DryRun:       r.dryRun,
				Log:          log,
			})
			if err != nil {
				log.Error("sync failed", "error", err)
				r.progress("error", "fail", rc.Name, err.Error())
				mu.Lock()
				results = append(results, RepoResult{
					Repo:     rc.Name,
					Status:   RepoStatusFailed,
					Action:   actionSyncTemplate,
					ErrMsg:   err.Error(),
					Duration: r.formatDuration(started),
				})
				mu.Unlock()
				return
			}

			log.Info("sync complete",
				"applied", len(syncResult.Applied),
				"skipped", len(syncResult.Skipped),
				"unchanged", len(syncResult.Unchanged),
			)
			r.progress("ok", "ok", rc.Name, syncSummary(syncResult, r.formatDuration(started)))

			mu.Lock()
			results = append(results, RepoResult{
				Repo:     rc.Name,
				Status:   RepoStatusSuccess,
				Action:   actionSyncTemplate,
				Output:   syncCounts(syncResult),
				Duration: r.formatDuration(started),
			})
			mu.Unlock()
		}()
	}

	wg.Wait()
	report.Results = results
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	report.Duration = r.reportDuration(report.StartedAt, report.FinishedAt)
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

		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer r.recoverIntoResults("Validate", rc.Name, actionValidate, &mu, &results)

			started := time.Now()

			log := logging.WithRepo(r.log, rc.Name, "")
			log.Info("running validate")
			r.progress("running", "...", rc.Name, "validating")

			guardianResult, err := gr.Check(ctx, rc.Name)
			if err != nil {
				log.Error("guardian check error", "error", err)
				r.progress("error", "fail", rc.Name, err.Error())
				mu.Lock()
				results = append(results, RepoResult{
					Repo:     rc.Name,
					Status:   RepoStatusFailed,
					Action:   actionValidate,
					ErrMsg:   err.Error(),
					Duration: r.formatDuration(started),
				})
				mu.Unlock()
				return
			}

			status := RepoStatusSuccess
			if !guardianResult.Passed {
				status = RepoStatusFailed
			}
			statusKind := "ok"
			statusLabel := "ok"
			statusDetail := "all checks passed"
			if status == RepoStatusFailed {
				statusKind = "error"
				statusLabel = "fail"
				statusDetail = guardianResult.ErrMsg
				if statusDetail == "" {
					statusDetail = "guardian reported failures"
				}
			}
			r.progress(statusKind, statusLabel, rc.Name, statusDetail+" in "+r.formatDuration(started))

			mu.Lock()
			results = append(results, RepoResult{
				Repo:     rc.Name,
				Status:   status,
				Action:   actionValidate,
				Output:   guardianResult.Output,
				ErrMsg:   guardianResult.ErrMsg,
				Duration: r.formatDuration(started),
			})
			mu.Unlock()
		}()
	}

	wg.Wait()
	report.Results = results
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	report.Duration = r.reportDuration(report.StartedAt, report.FinishedAt)
	return report, nil
}

func (r *Runner) progress(kind, label, primary, detail string) {
	if r.reporter == nil {
		return
	}
	message := strings.TrimSpace(primary)
	if detail != "" {
		message = strings.TrimSpace(message + ": " + detail)
	}
	r.reporter.Report(kind, label, message)
}

func (r *Runner) repoLabel(repo, env string) string {
	if env == "" {
		return repo
	}
	return repo + " [" + env + "]"
}

func (r *Runner) formatDuration(started time.Time) string {
	if r.ui != nil {
		return r.ui.Duration(time.Since(started))
	}
	return time.Since(started).Round(100 * time.Millisecond).String()
}

func (r *Runner) reportDuration(startedAt, finishedAt string) string {
	started, err := time.Parse(time.RFC3339, startedAt)
	if err != nil {
		return ""
	}
	finished, err := time.Parse(time.RFC3339, finishedAt)
	if err != nil {
		return ""
	}
	if r.ui != nil {
		return r.ui.Duration(finished.Sub(started))
	}
	return finished.Sub(started).Round(100 * time.Millisecond).String()
}

func syncSummary(result *template.SyncResult, duration string) string {
	counts := syncCounts(result)
	if duration == "" {
		return counts
	}
	return counts + " in " + duration
}

func syncCounts(result *template.SyncResult) string {
	return "applied=" + strconv.Itoa(len(result.Applied)) +
		" skipped=" + strconv.Itoa(len(result.Skipped)) +
		" unchanged=" + strconv.Itoa(len(result.Unchanged))
}
