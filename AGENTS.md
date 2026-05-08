# Agent Context

**This repo:** `ffreis-platform-runner` — platform runner service that executes
platform tasks (deployments, maintenance, health checks) with isolation, logging,
and status reporting. Containerized.

## Non-obvious facts

- **Logs to stderr, results to stdout.** Never mix diagnostic text with result output.

- **Includes a Containerfile** for OCI image builds — the binary is intended to run
  in containers, not only locally.

## Structure

```
cmd/platform-runner/   ← Cobra CLI entry point
cmd/                   ← task execution commands
Containerfile
```

## Build/run

```bash
make build
./bin/platform-runner <task>
```

## Public repo — private-repo hygiene

This is a **public** GitHub repository. When writing commit messages, PR titles,
PR descriptions, or any other user-visible text, **never name private repos** —
website content, inventory, infra, Lambda, or data repos that are not publicly
listed. Use generic terms instead: "the fleet inventory", "a private consumer",
"internal infra", "private data repo", etc.

## Keeping this file current

- **If you discover a fact not reflected here:** add it before finishing your task.
- **If something here is wrong or outdated:** correct it in the same commit as the code change.
- **If you rename a file, command, or concept referenced here:** update the reference.
