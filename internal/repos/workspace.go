package repos

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var repoNamePattern = regexp.MustCompile(`\A[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+\z`)

// Workspace manages the local clone of a repository.
type Workspace struct {
	Repo    string // "org/repo"
	RootDir string // base workspace directory
	Token   string // GitHub token for clone auth (never logged)
}

// Dir returns the local path for this repo's clone.
// Slashes in the repo name are replaced with dashes.
func (w *Workspace) Dir() string {
	safeName := strings.ReplaceAll(w.Repo, "/", "-")
	return filepath.Join(w.RootDir, safeName)
}

// remoteURL builds the repository URL without embedding credentials.
func (w *Workspace) remoteURL() string {
	return fmt.Sprintf("https://github.com/%s.git", w.Repo)
}

func (w *Workspace) gitEnv() []string {
	env := os.Environ()
	// Ensure git never blocks prompting for credentials.
	env = append(env, "GIT_TERMINAL_PROMPT=0")

	if w.Token == "" {
		return env
	}

	// Provide authentication without embedding the token in the remote URL.
	// This avoids persisting secrets to .git/config and reduces accidental leaks.
	//
	// NOTE: environment variables may still be readable by processes running as
	// the same user, but this is strictly better than storing the token in URLs.
	auth := "x-access-token:" + w.Token
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))

	env = append(env,
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://github.com/.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+encoded,
	)
	return env
}

func (w *Workspace) validate() error {
	if w.RootDir == "" {
		return fmt.Errorf("RootDir is required")
	}
	if w.Repo == "" {
		return fmt.Errorf("Repo is required")
	}
	if !repoNamePattern.MatchString(w.Repo) {
		return fmt.Errorf("Repo must be in org/repo format with safe characters: %q", w.Repo)
	}
	return nil
}

func shouldSanitizeOriginURL(originURL string) bool {
	if strings.Contains(originURL, "x-access-token:") {
		return true
	}
	// URLs can embed credentials as: https://user:pass@host/...
	if strings.Contains(originURL, "://") && strings.Contains(originURL, "@") {
		return strings.Contains(originURL, "github.com")
	}
	return false
}

// Ensure clones the repo if not present, or fetches and resets to HEAD if already cloned.
func (w *Workspace) Ensure(ctx context.Context) error {
	if err := w.validate(); err != nil {
		return fmt.Errorf("invalid workspace: %w", err)
	}

	dir := w.Dir()

	_, err := os.Stat(filepath.Join(dir, ".git"))
	if os.IsNotExist(err) {
		// Clone fresh.
		if err := os.MkdirAll(filepath.Dir(dir), 0o750); err != nil {
			return fmt.Errorf("creating workspace parent dir: %w", err)
		}
		// #nosec G204 -- command and flags are constant; user-provided repo is validated and passed as a single argument (no shell).
		cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", w.remoteURL(), dir)
		cmd.Env = w.gitEnv()
		cmd.Stdout = nil
		cmd.Stderr = nil
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git clone failed: %w", sanitizeOutput(string(out), w.Token))
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("checking workspace dir %q: %w", dir, err)
	}

	// If the repo was cloned previously with an embedded token URL, sanitize it.
	// Do not overwrite non-GitHub origins (tests use local bare remotes).
	getURLCmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "get-url", "origin")
	getURLCmd.Env = w.gitEnv()
	if out, err := getURLCmd.CombinedOutput(); err == nil {
		originURL := strings.TrimSpace(string(out))
		if shouldSanitizeOriginURL(originURL) {
			setURLCmd := exec.CommandContext(ctx, "git", "-C", dir, "remote", "set-url", "origin", w.remoteURL())
			setURLCmd.Env = w.gitEnv()
			if out, err := setURLCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git remote set-url failed: %w", sanitizeOutput(string(out), w.Token))
			}
		}
	}

	// Repo already cloned — fetch and reset.
	fetchCmd := exec.CommandContext(ctx, "git", "-C", dir, "fetch", "--depth", "1", "origin")
	fetchCmd.Env = w.gitEnv()
	if out, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %w", sanitizeOutput(string(out), w.Token))
	}

	resetTarget := "origin/HEAD"
	resetCmd := exec.CommandContext(ctx, "git", "-C", dir, "reset", "--hard", resetTarget)
	resetCmd.Env = w.gitEnv()
	if out, err := resetCmd.CombinedOutput(); err != nil {
		// Fallback: FETCH_HEAD is populated by the previous fetch invocation.
		// Some repos may not have origin/HEAD configured locally.
		fallbackCmd := exec.CommandContext(ctx, "git", "-C", dir, "reset", "--hard", "FETCH_HEAD")
		fallbackCmd.Env = w.gitEnv()
		if out2, err2 := fallbackCmd.CombinedOutput(); err2 != nil {
			return fmt.Errorf("git reset failed: %w", sanitizeOutput(string(out)+"\n"+string(out2), w.Token))
		}
	}

	return nil
}

// Remove deletes the local clone directory. Returns nil if the directory does not exist.
func (w *Workspace) Remove() error {
	dir := w.Dir()
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing workspace dir %q: %w", dir, err)
	}
	return nil
}

// sanitizeOutput wraps an error message, ensuring the token is not included.
func sanitizeOutput(output, token string) error {
	if token != "" {
		output = strings.ReplaceAll(output, token, "***")
	}
	return fmt.Errorf("%s", output)
}
