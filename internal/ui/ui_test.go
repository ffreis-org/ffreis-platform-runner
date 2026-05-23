package ui

import (
	"strings"
	"testing"
	"time"
)

func TestResolveMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		requested       string
		stdoutTTY       bool
		stderrTTY       bool
		noColor         bool
		wantMode        string
		wantInteractive bool
	}{
		{name: "auto tty", requested: "auto", stdoutTTY: true, wantMode: ModeRich, wantInteractive: true},
		{name: "auto no tty", requested: "auto", wantMode: ModePlain, wantInteractive: false},
		{name: "auto no color", requested: "auto", stdoutTTY: true, noColor: true, wantMode: ModePlain, wantInteractive: true},
		{name: "plain explicit", requested: "plain", wantMode: ModePlain, wantInteractive: true},
		{name: "rich explicit", requested: "rich", wantMode: ModeRich, wantInteractive: true},
		{name: "rich explicit no color", requested: "rich", noColor: true, wantMode: ModePlain, wantInteractive: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotMode, gotInteractive, err := ResolveMode(tc.requested, tc.stdoutTTY, tc.stderrTTY, tc.noColor)
			if err != nil {
				t.Fatalf("ResolveMode() error: %v", err)
			}
			if gotMode != tc.wantMode || gotInteractive != tc.wantInteractive {
				t.Fatalf("ResolveMode() = (%q, %v), want (%q, %v)", gotMode, gotInteractive, tc.wantMode, tc.wantInteractive)
			}
		})
	}
}

func TestResolveMode_Invalid(t *testing.T) {
	t.Parallel()

	if _, _, err := ResolveMode("loud", true, true, false); err == nil {
		t.Fatal("expected invalid mode error")
	}
}

func TestPresenterPlainOutput(t *testing.T) {
	t.Parallel()

	p := &Presenter{mode: ModePlain}
	if got := p.Badge("ok", "OK"); got != "[ok]" {
		t.Fatalf("Badge() = %q", got)
	}
	if got := p.Duration(1260 * time.Millisecond); got != "1.3s" {
		t.Fatalf("Duration() = %q", got)
	}
	if got := p.Status("error", "FAILED", "acme/repo [dev]"); !strings.Contains(got, "[failed]") {
		t.Fatalf("Status() = %q", got)
	}
}
