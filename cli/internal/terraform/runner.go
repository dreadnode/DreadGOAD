// Package terraform wraps the terraform CLI for direct Terraform operations
// (as opposed to Terragrunt). Used by providers like Proxmox that don't
// need the Terragrunt multi-module orchestration layer.
package terraform

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Options configures a Terraform command invocation.
type Options struct {
	Action          string
	WorkDir         string
	TerraformBinary string
	AutoApprove     bool
	LogFile         string
	Debug           bool
	VarFile         string   // optional path to a .tfvars file
	Vars            []string // extra -var flags (e.g. "pm_password=xxx")
}

// Run executes a single terraform command.
func Run(ctx context.Context, opts Options) error {
	args := buildArgs(opts)

	slog.Info("running terraform",
		"action", opts.Action,
		"dir", opts.WorkDir,
		"args", strings.Join(args, " "),
	)

	binary := opts.TerraformBinary
	if binary == "" {
		binary = "tofu"
	}

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = opts.WorkDir
	cmd.Env = os.Environ()

	writer, cleanup, err := outputWriter(opts.LogFile)
	if err != nil {
		return fmt.Errorf("setup output: %w", err)
	}
	defer cleanup()

	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("terraform %s failed: %w", opts.Action, err)
	}
	return nil
}

// Output runs `terraform output -json` and returns the raw JSON.
func Output(ctx context.Context, opts Options) ([]byte, error) {
	binary := opts.TerraformBinary
	if binary == "" {
		binary = "tofu"
	}

	cmd := exec.CommandContext(ctx, binary, "output", "-json")
	cmd.Dir = opts.WorkDir
	cmd.Env = os.Environ()

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("terraform output failed: %w", err)
	}
	return out, nil
}

func buildArgs(opts Options) []string {
	args := []string{opts.Action}
	if opts.Action == "init" {
		args = append(args, "-upgrade")
	}
	if opts.AutoApprove && (opts.Action == "apply" || opts.Action == "destroy") {
		args = append(args, "-auto-approve")
	}
	if opts.VarFile != "" {
		args = append(args, "-var-file="+opts.VarFile)
	}
	for _, v := range opts.Vars {
		args = append(args, "-var", v)
	}
	return args
}

func outputWriter(logFile string) (io.Writer, func(), error) {
	if logFile == "" {
		return os.Stdout, func() {}, nil
	}

	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return nil, nil, err
	}

	f, err := os.Create(logFile)
	if err != nil {
		return nil, nil, err
	}

	mw := io.MultiWriter(os.Stdout, f)
	return mw, func() {
		if err := f.Close(); err != nil {
			slog.Warn("failed to close log file", "path", logFile, "error", err)
		}
	}, nil
}
