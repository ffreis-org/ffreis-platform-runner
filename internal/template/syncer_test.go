package template

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ffreis/platform-runner/internal/logging"
)

const (
	fileNew    = "new-file.txt"
	fileMainTF = "main.tf"
	fileReadme = "README.md"

	contentTemplate    = "template content"
	contentTemplateTF  = "template tf content"
	contentRepoTF      = "repo tf content"
	contentTemplateMD  = "template readme"
	contentOldReadmeMD = "old readme"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("creating dir for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing file %q: %v", path, err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading file %q: %v", path, err)
	}
	return string(content)
}

func TestSync_SourceOnly_Applied(t *testing.T) {
	tmpDir := t.TempDir()
	templateDir := filepath.Join(tmpDir, "template")
	repoDir := filepath.Join(tmpDir, "repo")

	writeFile(t, filepath.Join(templateDir, fileNew), contentTemplate)
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		t.Fatalf("creating repo dir: %v", err)
	}

	result, err := Sync(context.Background(), SyncOptions{
		TemplateDir: templateDir,
		RepoDir:     repoDir,
	})
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	if len(result.Applied) != 1 || result.Applied[0] != fileNew {
		t.Errorf("expected Applied=[%s], got %v", fileNew, result.Applied)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected no skipped files, got %v", result.Skipped)
	}

	// Verify the file was written to the repo.
	got := readFile(t, filepath.Join(repoDir, fileNew))
	if got != contentTemplate {
		t.Errorf("unexpected file content: %q", got)
	}
}

func TestSync_Conflict_Skipped(t *testing.T) {
	tmpDir := t.TempDir()
	templateDir := filepath.Join(tmpDir, "template")
	repoDir := filepath.Join(tmpDir, "repo")

	writeFile(t, filepath.Join(templateDir, fileMainTF), contentTemplateTF)
	writeFile(t, filepath.Join(repoDir, fileMainTF), contentRepoTF)

	result, err := Sync(context.Background(), SyncOptions{
		TemplateDir:  templateDir,
		RepoDir:      repoDir,
		SafePatterns: []string{"*.md"}, // main.tf does NOT match
	})
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	if len(result.Skipped) != 1 || result.Skipped[0] != fileMainTF {
		t.Errorf("expected Skipped=[%s], got %v", fileMainTF, result.Skipped)
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected no applied files, got %v", result.Applied)
	}
}

func TestSync_Safe_Applied(t *testing.T) {
	tmpDir := t.TempDir()
	templateDir := filepath.Join(tmpDir, "template")
	repoDir := filepath.Join(tmpDir, "repo")

	writeFile(t, filepath.Join(templateDir, fileReadme), contentTemplateMD)
	writeFile(t, filepath.Join(repoDir, fileReadme), contentOldReadmeMD)

	result, err := Sync(context.Background(), SyncOptions{
		TemplateDir:  templateDir,
		RepoDir:      repoDir,
		SafePatterns: []string{"*.md"},
	})
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	if len(result.Applied) != 1 || result.Applied[0] != fileReadme {
		t.Errorf("expected Applied=[%s], got %v", fileReadme, result.Applied)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected no skipped files, got %v", result.Skipped)
	}

	got := readFile(t, filepath.Join(repoDir, fileReadme))
	if got != contentTemplateMD {
		t.Errorf("unexpected content: %q", got)
	}
}

func TestSync_DryRun_NothingWritten(t *testing.T) {
	tmpDir := t.TempDir()
	templateDir := filepath.Join(tmpDir, "template")
	repoDir := filepath.Join(tmpDir, "repo")

	writeFile(t, filepath.Join(templateDir, fileNew), contentTemplate)
	if err := os.MkdirAll(repoDir, 0o750); err != nil {
		t.Fatalf("creating repo dir: %v", err)
	}

	result, err := Sync(context.Background(), SyncOptions{
		TemplateDir: templateDir,
		RepoDir:     repoDir,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	// Applied list should still be populated (what would have been applied).
	if len(result.Applied) != 1 || result.Applied[0] != fileNew {
		t.Errorf("expected Applied=[%s] in dry-run, got %v", fileNew, result.Applied)
	}

	// But file should NOT have been written.
	if _, err := os.Stat(filepath.Join(repoDir, fileNew)); !os.IsNotExist(err) {
		t.Errorf("file should not exist in dry-run mode")
	}
}

func TestSync_Identical_Unchanged(t *testing.T) {
	tmpDir := t.TempDir()
	templateDir := filepath.Join(tmpDir, "template")
	repoDir := filepath.Join(tmpDir, "repo")

	content := "identical content"
	writeFile(t, filepath.Join(templateDir, "file.txt"), content)
	writeFile(t, filepath.Join(repoDir, "file.txt"), content)

	result, err := Sync(context.Background(), SyncOptions{
		TemplateDir: templateDir,
		RepoDir:     repoDir,
	})
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}

	if len(result.Unchanged) != 1 || result.Unchanged[0] != "file.txt" {
		t.Errorf("expected Unchanged=[file.txt], got %v", result.Unchanged)
	}
	if len(result.Applied) != 0 {
		t.Errorf("expected no applied files, got %v", result.Applied)
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected no skipped files, got %v", result.Skipped)
	}
}

func TestLoggerOrNopAndWriteIfNotDryRun(t *testing.T) {
	if loggerOrNop(nil) == nil {
		t.Fatal("expected loggerOrNop(nil) to return a logger")
	}
	if loggerOrNop(logging.Nop()) == nil {
		t.Fatal("expected loggerOrNop(non-nil) to return the logger")
	}

	tmpDir := t.TempDir()
	if err := writeIfNotDryRun(SyncOptions{RepoDir: tmpDir}, FileDiff{
		Path:     "nested/file.txt",
		Template: "content",
		Status:   DiffSourceOnly,
	}, logging.Nop()); err != nil {
		t.Fatalf("writeIfNotDryRun() unexpected error: %v", err)
	}

	got := readFile(t, filepath.Join(tmpDir, "nested/file.txt"))
	if got != "content" {
		t.Fatalf("unexpected content: %q", got)
	}
}
