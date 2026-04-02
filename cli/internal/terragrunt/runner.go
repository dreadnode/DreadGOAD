package terragrunt

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Options configures a terragrunt execution.
type Options struct {
	// Action is the terraform action: init, plan, apply, destroy, output.
	Action string
	// WorkDir is the directory to run terragrunt in.
	WorkDir string
	// TerragruntBinary is the path to the terragrunt binary.
	TerragruntBinary string
	// TerraformBinary is the path to the terraform/tofu binary.
	TerraformBinary string
	// AutoApprove skips confirmation prompts (apply/destroy).
	AutoApprove bool
	// NonInteractive disables interactive prompts.
	NonInteractive bool
	// ExcludeDirs is a comma-separated list of dirs to exclude from run-all.
	ExcludeDirs string
	// LogFile is an optional path to write output to.
	LogFile string
	// Debug enables verbose output.
	Debug bool
}

// Result holds the outcome of a terragrunt execution.
type Result struct {
	Module  string
	Success bool
	Error   error
}

// Run executes a single terragrunt command in the given working directory.
func Run(ctx context.Context, opts Options) error {
	args := buildArgs(opts)

	slog.Info("running terragrunt",
		"action", opts.Action,
		"dir", opts.WorkDir,
		"args", strings.Join(args, " "),
	)

	cmd := exec.CommandContext(ctx, opts.TerragruntBinary, args...)
	cmd.Dir = opts.WorkDir
	cmd.Env = buildEnv(opts)

	writer, cleanup, err := outputWriter(opts.LogFile)
	if err != nil {
		return fmt.Errorf("setup output: %w", err)
	}
	defer cleanup()

	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("terragrunt %s failed: %w", opts.Action, err)
	}
	return nil
}

// RunAll executes `terragrunt run-all <action>` across all modules in the working directory.
func RunAll(ctx context.Context, opts Options) error {
	args := []string{"run-all", opts.Action}
	if opts.AutoApprove && (opts.Action == "apply" || opts.Action == "destroy") {
		args = append(args, "-auto-approve")
	}
	if opts.NonInteractive {
		args = append(args, "--non-interactive")
	}

	slog.Info("running terragrunt run-all",
		"action", opts.Action,
		"dir", opts.WorkDir,
	)

	cmd := exec.CommandContext(ctx, opts.TerragruntBinary, args...)
	cmd.Dir = opts.WorkDir
	cmd.Env = buildEnv(opts)

	if opts.ExcludeDirs != "" {
		cmd.Env = append(cmd.Env, "TG_QUEUE_EXCLUDE_DIR="+opts.ExcludeDirs)
	}

	writer, cleanup, err := outputWriter(opts.LogFile)
	if err != nil {
		return fmt.Errorf("setup output: %w", err)
	}
	defer cleanup()

	cmd.Stdout = writer
	cmd.Stderr = writer

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("terragrunt run-all %s failed: %w", opts.Action, err)
	}
	return nil
}

// RunIndividual iterates subdirectories of modulePath and runs terragrunt
// in each one individually. Returns results for each subdirectory.
func RunIndividual(ctx context.Context, opts Options, modulePath string, exclude []string) ([]Result, error) {
	entries, err := os.ReadDir(modulePath)
	if err != nil {
		return nil, fmt.Errorf("read module directory %s: %w", modulePath, err)
	}

	excludeSet := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		excludeSet[strings.TrimSpace(e)] = true
	}

	var subdirs []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		subdirs = append(subdirs, entry.Name())
	}
	sort.Strings(subdirs)

	if len(subdirs) == 0 {
		return nil, fmt.Errorf("no subdirectories found in %s", modulePath)
	}

	var results []Result
	for i, subdir := range subdirs {
		if excludeSet[subdir] {
			slog.Info("skipping excluded subdirectory", "subdir", subdir)
			results = append(results, Result{Module: subdir, Success: true})
			continue
		}

		fmt.Printf("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("Processing: %s (%d/%d)\n", subdir, i+1, len(subdirs))
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

		subdirOpts := opts
		subdirOpts.WorkDir = filepath.Join(modulePath, subdir)

		if opts.LogFile != "" {
			ext := filepath.Ext(opts.LogFile)
			base := strings.TrimSuffix(opts.LogFile, ext)
			subdirOpts.LogFile = fmt.Sprintf("%s_%s%s", base, subdir, ext)
		}

		runErr := Run(ctx, subdirOpts)
		results = append(results, Result{
			Module:  subdir,
			Success: runErr == nil,
			Error:   runErr,
		})

		if runErr != nil {
			fmt.Printf("FAILED: %s - %v\n", subdir, runErr)
		} else {
			fmt.Printf("OK: %s\n", subdir)
		}
	}

	return results, nil
}

// Output runs `terragrunt output -json` and returns the raw JSON bytes.
func Output(ctx context.Context, opts Options) ([]byte, error) {
	args := []string{"output", "-json"}

	cmd := exec.CommandContext(ctx, opts.TerragruntBinary, args...)
	cmd.Dir = opts.WorkDir
	cmd.Env = buildEnv(opts)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("terragrunt output failed: %w", err)
	}
	return out, nil
}

func buildArgs(opts Options) []string {
	args := []string{opts.Action}
	if opts.AutoApprove && (opts.Action == "apply" || opts.Action == "destroy") {
		args = append(args, "-auto-approve")
	}
	if opts.NonInteractive {
		args = append(args, "--non-interactive")
	}
	return args
}

func buildEnv(opts Options) []string {
	env := os.Environ()
	if opts.TerraformBinary != "" {
		env = append(env, "TERRAGRUNT_TFPATH="+opts.TerraformBinary)
	}
	return env
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
	return mw, func() { f.Close() }, nil
}
