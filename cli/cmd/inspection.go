package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/spf13/cobra"
)

// labInspector keeps the user-facing inspection commands stable while each
// lab family implements the checks that are meaningful for its workloads.
type labInspector interface {
	health(context.Context, *cobra.Command, *config.Config, bool) error
	validate(context.Context, *cobra.Command, *config.Config, validateOpts) error
}

func inspectorFor(cfg *config.Config) labInspector {
	if cfg.ResolvedLab() == "SCOPE-RANGE" {
		return scopeRangeInspector{}
	}
	return goadInspector{}
}

type goadInspector struct{}

func (goadInspector) health(
	ctx context.Context, cmd *cobra.Command, cfg *config.Config, jsonOut bool,
) error {
	return runGOADHealthCheck(ctx, cmd, cfg, jsonOut)
}

func (goadInspector) validate(
	ctx context.Context, cmd *cobra.Command, cfg *config.Config, opts validateOpts,
) error {
	return runGOADValidate(ctx, cmd, cfg, opts)
}

type scopeRangeInspector struct{}

func (scopeRangeInspector) health(
	ctx context.Context, _ *cobra.Command, cfg *config.Config, jsonOut bool,
) error {
	args := []string{
		filepath.Join(cfg.ProjectRoot, "scripts", "validate-scope-range-live.py"),
		"--env", cfg.Env, "--health",
	}
	if jsonOut {
		args = append(args, "--json")
	}
	return runScopeRangeInspector(ctx, args, "health check")
}

func (scopeRangeInspector) validate(
	ctx context.Context, _ *cobra.Command, cfg *config.Config, opts validateOpts,
) error {
	return runScopeRangeValidate(ctx, cfg, opts)
}

func runScopeRangeInspector(ctx context.Context, args []string, operation string) error {
	if _, err := os.Stat(args[0]); err != nil {
		return fmt.Errorf("find SCOPE-RANGE validator: %w", err)
	}
	command := exec.CommandContext(ctx, "python3", args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("SCOPE-RANGE %s failed: %w", operation, err)
	}
	return nil
}
