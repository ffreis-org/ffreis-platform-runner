package repos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceDir(t *testing.T) {
	w := &Workspace{
		Repo:    "myorg/my-repo",
		RootDir: "/tmp/workspace",
	}
	got := w.Dir()
	want := filepath.Join("/tmp/workspace", "myorg-my-repo")
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestWorkspaceDir_SlashReplacement(t *testing.T) {
	w := &Workspace{
		Repo:    "example-org/infra-repo",
		RootDir: "/workspace",
	}
	got := w.Dir()
	want := "/workspace/example-org-infra-repo"
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestWorkspaceRemoteURL_DoesNotEmbedToken(t *testing.T) {
	w := &Workspace{
		Repo:    "myorg/my-repo",
		RootDir: "/tmp/workspace",
		Token:   "ghp_exampletoken",
	}

	url := w.remoteURL()
	if strings.Contains(url, w.Token) {
		t.Fatalf("remoteURL() must not embed token, got %q", url)
	}
	if url != "https://github.com/myorg/my-repo.git" {
		t.Fatalf("unexpected remoteURL(): got %q", url)
	}
}

func TestWorkspace_Remove_NonExistent(t *testing.T) {
	tmp, err := os.MkdirTemp("", "workspace-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmp)

	w := &Workspace{
		Repo:    "nonexistent/repo",
		RootDir: tmp,
	}

	// Dir does not exist — Remove should return nil (idempotent).
	if err := w.Remove(); err != nil {
		t.Errorf("Remove() on non-existent dir returned error: %v", err)
	}
}

func TestWorkspace_Remove_ExistingDir(t *testing.T) {
	tmp, err := os.MkdirTemp("", "workspace-remove-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmp)

	w := &Workspace{
		Repo:    "myorg/repo",
		RootDir: tmp,
	}

	// Create the directory.
	if err := os.MkdirAll(w.Dir(), 0o750); err != nil {
		t.Fatalf("creating workspace dir: %v", err)
	}

	if err := w.Remove(); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if _, err := os.Stat(w.Dir()); !os.IsNotExist(err) {
		t.Errorf("expected dir to be removed, but it still exists")
	}
}
