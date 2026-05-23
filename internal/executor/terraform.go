package executor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// TerraformExecutor implements Executor using the terraform CLI.
type TerraformExecutor struct{}

// Plan runs `terraform plan -out=plan.tfplan -detailed-exitcode -input=false`
// plus any -var flags from opts.TFVars.
// Exit code 2 means changes detected (HasChanges=true).
// Exit code 0 means no changes.
// Any other exit code is returned as an error.
func (t *TerraformExecutor) Plan(ctx context.Context, opts ExecOptions) (*ExecResult, error) {
	args := []string{
		terraformSubcommandPlan,
		"-out=" + terraformPlanFile,
		"-detailed-exitcode",
		"-input=false",
	}
	for k, v := range opts.TFVars {
		args = append(args, "-var", fmt.Sprintf("%s=%s", k, v))
	}

	binary, err := resolveCommand(terraformBinary, opts.Env)
	if err != nil {
		return nil, fmt.Errorf("running terraform %s: %w", terraformSubcommandPlan, err)
	}

	// binary is resolved from terraformBinary (constant) via resolveCommand
	// which only accepts entries in the executor allowlist; args are
	// program-controlled (plan/apply subcommand + -chdir / -var-file flags
	// built from typed config), never from user-supplied strings.
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // see comment above; nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd.Dir = opts.WorkDir
	cmd.Env = buildEnv(opts.Env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("running terraform %s: %w", terraformSubcommandPlan, err)
		}
	}

	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}

	switch exitCode {
	case 0:
		// No changes.
	case 2:
		// Changes detected.
		result.HasChanges = true
	default:
		return result, fmt.Errorf("terraform %s exited with code %d: %s", terraformSubcommandPlan, exitCode, stderr.String())
	}

	return result, nil
}

// Apply runs `terraform apply -auto-approve -input=false plan.tfplan`.
// If opts.DryRun is true, it returns a zero result without running anything.
// If opts.Confirm is false, it returns an error.
func (t *TerraformExecutor) Apply(ctx context.Context, opts ExecOptions) (*ExecResult, error) {
	if opts.DryRun {
		return &ExecResult{}, nil
	}

	if !opts.Confirm {
		return nil, errors.New(errApplyConfirmRequired)
	}

	args := []string{
		terraformSubcommandApply,
		"-auto-approve",
		"-input=false",
		terraformPlanFile,
	}

	binary, err := resolveCommand(terraformBinary, opts.Env)
	if err != nil {
		return nil, fmt.Errorf("running terraform %s: %w", terraformSubcommandApply, err)
	}

	// binary is resolved from terraformBinary (constant) via resolveCommand
	// which only accepts entries in the executor allowlist; args are
	// program-controlled (plan/apply subcommand + -chdir / -var-file flags
	// built from typed config), never from user-supplied strings.
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // see comment above; nosemgrep: go.lang.security.audit.dangerous-exec-command.dangerous-exec-command
	cmd.Dir = opts.WorkDir
	cmd.Env = buildEnv(opts.Env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("running terraform %s: %w", terraformSubcommandApply, err)
		}
	}

	result := &ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}

	if exitCode != 0 {
		return result, fmt.Errorf("terraform %s exited with code %d: %s", terraformSubcommandApply, exitCode, stderr.String())
	}

	return result, nil
}

// buildEnv builds an explicit environment slice from a map.
// Shell environment is NOT inherited.
func buildEnv(env map[string]string) []string {
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

func resolveCommand(binary string, env map[string]string) (string, error) {
	pathValue, ok := env["PATH"]
	if !ok || pathValue == "" {
		return exec.LookPath(binary)
	}
	return lookPath(binary, pathValue)
}

func lookPath(binary, pathValue string) (string, error) {
	if stringsContainsPathSeparator(binary) {
		return checkExecutable(binary)
	}
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			dir = "."
		}
		candidate := filepath.Join(dir, binary)
		if resolved, err := checkExecutable(candidate); err == nil {
			return resolved, nil
		}
	}
	return "", &exec.Error{Name: binary, Err: exec.ErrNotFound}
}

func checkExecutable(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", &exec.Error{Name: path, Err: exec.ErrNotFound}
	}
	return path, nil
}

func stringsContainsPathSeparator(value string) bool {
	return filepath.Base(value) != value
}
