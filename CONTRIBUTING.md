# Contributing

## CLI Output Rule

- Use structured logging for diagnostic events, not operator UX.
- Use the command/output layer for human-facing terminal lines such as headers, summaries, prompts, and status updates.
- Keep `stdout` for intended command output and `stderr` for logs, prompts, and progress.

## `fmt` Usage

- Prefer `io.WriteString`, `strconv`, `strings.Builder`, and `path/filepath` when they express the code more directly.
- Use `fmt.Errorf` for wrapped errors.
- Use `fmt` for local formatting only when simpler helpers would make the code less clear.

## Logging

- Log structured fields instead of interpolated messages.
- Treat logs as machine-friendly diagnostics; do not use the logger to emit `ok`, `warn`, or prompt text.

## Repo Layout

- This repo follows the Go CLI archetype.
- Keep the executable entrypoint in `cmd/platform-runner/main.go`.
- Keep Cobra wiring outside `main.go`; `main.go` should only call the top-level execute path.
- Keep automation in `scripts/`.
- Do not move the CLI entrypoint back to the repo root.
