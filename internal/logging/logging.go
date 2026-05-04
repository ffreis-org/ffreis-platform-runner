package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// New creates a structured logger at the given level.
func New(level string, human bool) (*slog.Logger, error) {
	return NewWithWriter(level, human, os.Stderr)
}

// WithRepo returns a child logger with repo and env fields attached.
func WithRepo(log *slog.Logger, repo, env string) *slog.Logger {
	if log == nil {
		log = Nop()
	}
	if env == "" {
		return log.With("repo", repo)
	}
	return log.With("repo", repo, "env", env)
}

// NewWithWriter creates a structured logger that writes to the supplied writer.
func NewWithWriter(level string, human bool, w io.Writer) (*slog.Logger, error) {
	parsed, err := parseLevel(level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{
		Level:     parsed,
		AddSource: strings.EqualFold(level, "debug"),
	}
	if human {
		return slog.New(slog.NewTextHandler(w, opts)), nil
	}
	return slog.New(slog.NewJSONHandler(w, opts)), nil
}

// Nop returns a logger that discards all output.
func Nop() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid log level %q", level)
	}
}
