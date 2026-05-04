package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestNew_InvalidLevel(t *testing.T) {
	t.Parallel()

	_, err := New("not-a-level", false)
	if err == nil {
		t.Fatalf("expected error for invalid level")
	}
}

func TestNew_ValidLevel(t *testing.T) {
	t.Parallel()

	log, err := New("info", true)
	if err != nil {
		t.Fatalf("New(info) unexpected error: %v", err)
	}
	if log == nil {
		t.Fatalf("expected non-nil logger")
	}
}

func TestNew_ValidLevelJSON(t *testing.T) {
	t.Parallel()

	log, err := New("info", false)
	if err != nil {
		t.Fatalf("New(info,false) unexpected error: %v", err)
	}
	if log == nil {
		t.Fatalf("expected non-nil logger")
	}
}

func TestWithRepo_AddsFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base, err := NewWithWriter("info", false, &buf)
	if err != nil {
		t.Fatalf("NewWithWriter() unexpected error: %v", err)
	}

	child := WithRepo(base, "acme/repo", "dev")
	child.Info("hello")

	got := buf.String()
	if !strings.Contains(got, `"repo":"acme/repo"`) || !strings.Contains(got, `"env":"dev"`) {
		t.Fatalf("expected repo/env fields in output, got %q", got)
	}
}
