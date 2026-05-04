package runner

import "fmt"

// RepoStatus describes the final state of a per-repo action.
type RepoStatus string

const (
	RepoStatusSuccess RepoStatus = "success"
	RepoStatusFailed  RepoStatus = "failed"
	RepoStatusSkipped RepoStatus = "skipped"
)

// RepoResult holds the outcome of a single repo+env action.
type RepoResult struct {
	Repo       string
	Env        string
	Status     RepoStatus
	Action     string // "plan", "apply", "sync-template", "validate"
	Output     string
	ErrMsg     string
	HasChanges bool
	Duration   string
}

// RunReport aggregates results from a full runner invocation.
type RunReport struct {
	Action     string
	Results    []RepoResult
	StartedAt  string // RFC3339
	FinishedAt string
	Duration   string
}

// Summary returns a human-readable summary of the report.
func (r *RunReport) Summary() string {
	var succeeded, failed, skipped int
	for _, res := range r.Results {
		switch res.Status {
		case RepoStatusSuccess:
			succeeded++
		case RepoStatusFailed:
			failed++
		case RepoStatusSkipped:
			skipped++
		}
	}
	if r.Duration != "" {
		return fmt.Sprintf("%d succeeded, %d failed, %d skipped in %s", succeeded, failed, skipped, r.Duration)
	}
	return fmt.Sprintf("%d succeeded, %d failed, %d skipped", succeeded, failed, skipped)
}

// HasFailures returns true if any result has status "failed".
func (r *RunReport) HasFailures() bool {
	for _, res := range r.Results {
		if res.Status == RepoStatusFailed {
			return true
		}
	}
	return false
}
