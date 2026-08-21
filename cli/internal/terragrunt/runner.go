package terragrunt

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const outputTailLimit = 64 * 1024

var (
	ansiEscapeRE     = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	stateLockErrorRE = regexp.MustCompile(`(?i)(error acquiring the state lock|failed to acquire state lock)`)
	stateLockIDRE    = regexp.MustCompile(`(?im)\bID:\s*([A-Za-z0-9][A-Za-z0-9._:/-]*)(?:\s|$|│)`)
	shellSafeArgRE   = regexp.MustCompile(`^[A-Za-z0-9_./:-]+$`)
)

type Options struct {
	Action           string
	WorkDir          string
	TerragruntBinary string
	TerraformBinary  string
	AutoApprove      bool
	NonInteractive   bool
	ExcludeDirs      string
	LogFile          string
	Debug            bool
	// ExtraEnv is appended to the child process environment in KEY=VALUE form.
	// Used by callers that need to set Terragrunt feature toggles (e.g. the
	// Azure Bastion module's DREADGOAD_ENABLE_AZURE_BASTION gate) without
	// mutating the parent process env.
	ExtraEnv []string
}

type Result struct {
	Module  string
	Success bool
	Error   error
}

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

	tail := newTailBuffer(outputTailLimit)
	cmd.Stdout = io.MultiWriter(writer, tail)
	cmd.Stderr = cmd.Stdout

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("terragrunt %s timed out (context: %w); last output:\n%s",
				opts.Action, ctx.Err(), lastLines(tail.String(), 20))
		}
		return commandError(fmt.Sprintf("terragrunt %s failed", opts.Action), err, tail.String(), opts.TerragruntBinary)
	}
	return nil
}

func RunAll(ctx context.Context, opts Options) error {
	// Terragrunt flags go before --, tofu flags go after the action.
	args := []string{"run", "--all"}
	// terragrunt v0.97+ auto-appends -auto-approve for run --all.
	// Only add --no-auto-approve when the caller explicitly wants a prompt.
	if !opts.AutoApprove && (opts.Action == "apply" || opts.Action == "destroy") {
		args = append(args, "--no-auto-approve")
	}
	if opts.NonInteractive {
		args = append(args, "--non-interactive")
	}
	args = append(args, "--", opts.Action)
	if opts.Action == "init" {
		args = append(args, "-upgrade")
	}

	slog.Info("running terragrunt run --all",
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

	tail := newTailBuffer(outputTailLimit)
	cmd.Stdout = io.MultiWriter(writer, tail)
	cmd.Stderr = cmd.Stdout

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("terragrunt run --all %s timed out (context: %w); last output:\n%s",
				opts.Action, ctx.Err(), lastLines(tail.String(), 20))
		}
		return commandError(fmt.Sprintf("terragrunt run --all %s failed", opts.Action), err, tail.String(), opts.TerragruntBinary)
	}
	return nil
}

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
	if opts.Action == "init" {
		args = append(args, "-upgrade")
	}
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
		env = append(env, "TG_TF_PATH="+opts.TerraformBinary)
	}
	if len(opts.ExtraEnv) > 0 {
		env = append(env, opts.ExtraEnv...)
	}
	return env
}

type tailBuffer struct {
	limit int
	buf   []byte
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit}
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	written := len(p)
	if b.limit <= 0 {
		return written, nil
	}
	if len(p) >= b.limit {
		b.buf = append(b.buf[:0], p[len(p)-b.limit:]...)
		return written, nil
	}

	overflow := len(b.buf) + len(p) - b.limit
	if overflow > 0 {
		copy(b.buf, b.buf[overflow:])
		b.buf = b.buf[:len(b.buf)-overflow]
	}
	b.buf = append(b.buf, p...)
	return written, nil
}

func (b *tailBuffer) String() string {
	return string(b.buf)
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

func commandError(message string, runErr error, output, terragruntBinary string) error {
	lockID := stateLockID(output)
	if lockID == "" {
		return fmt.Errorf("%s: %w", message, runErr)
	}
	if terragruntBinary == "" {
		terragruntBinary = "terragrunt"
	}
	return fmt.Errorf("%s: %w\nTerraform state lock detected (ID: %s). "+
		"After confirming no other operation is running, run this from the locked module directory:\n  %s force-unlock %s",
		message, runErr, lockID, shellQuote(terragruntBinary), lockID)
}

func shellQuote(value string) string {
	if shellSafeArgRE.MatchString(value) {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func stateLockID(output string) string {
	clean := ansiEscapeRE.ReplaceAllString(output, "")
	lockAt := stateLockErrorRE.FindStringIndex(clean)
	if lockAt == nil {
		return ""
	}
	lockOutput := clean[lockAt[0]:]
	if infoAt := strings.Index(strings.ToLower(lockOutput), "lock info:"); infoAt >= 0 {
		lockOutput = lockOutput[infoAt:]
	}
	match := stateLockIDRE.FindStringSubmatch(lockOutput)
	if len(match) != 2 {
		return ""
	}
	return match[1]
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
