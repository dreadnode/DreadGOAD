package ansible

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
)

// RunOptions configures a single ansible-playbook execution.
type RunOptions struct {
	Playbook    string
	Env         string
	Inventories []string // additional inventory paths (appended after the default env inventory)
	Limit       string
	Forks       int
	ExtraVars   map[string]string
	ExtraEnv    map[string]string
	Debug       bool
	IdleTimeout time.Duration
	LogFile     string
}

// RunResult holds the outcome of an ansible-playbook execution.
type RunResult struct {
	ExitCode    int
	Output      string
	Success     bool
	FailedHosts []string
	ErrorType   ErrorType
	ErrorDetail string
	TimedOut    bool
}

// RunPlaybook executes ansible-playbook with idle timeout monitoring.
func RunPlaybook(ctx context.Context, opts RunOptions) *RunResult {
	cfg, err := config.Get()
	if err != nil {
		return &RunResult{ExitCode: 1, Output: fmt.Sprintf("config error: %v", err)}
	}
	result := &RunResult{}

	args := buildArgs(opts, cfg)
	env, err := buildEnv(opts, cfg)
	if err != nil {
		result.ErrorType = ErrUnclassified
		result.ErrorDetail = err.Error()
		return result
	}

	slog.Info("running playbook", "playbook", opts.Playbook, "args", strings.Join(args, " "))
	logRunOptions(opts)

	cmd := exec.CommandContext(ctx, "ansible-playbook", args...)
	cmd.Env = env
	cmd.Dir = cfg.ProjectRoot
	// Set process group so we can kill the entire tree
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var outputBuf bytes.Buffer
	writers := []io.Writer{&outputBuf, os.Stdout}

	if opts.LogFile != "" {
		if f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			writers = append(writers, f)
			defer func() {
				if err := f.Close(); err != nil {
					slog.Warn("failed to close log file", "path", opts.LogFile, "error", err)
				}
			}()
		}
	}

	multiW := io.MultiWriter(writers...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.ExitCode = 1
		result.Output = fmt.Sprintf("failed to create stdout pipe: %v", err)
		return result
	}
	cmd.Stderr = multiW // merge stderr into the same output stream

	if err := cmd.Start(); err != nil {
		result.ExitCode = 1
		result.Output = fmt.Sprintf("failed to start ansible-playbook: %v", err)
		return result
	}

	var bytesWritten atomic.Int64
	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = time.Duration(cfg.IdleTimeout) * time.Second
	}

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			_, _ = fmt.Fprintln(multiW, line)
			bytesWritten.Add(int64(len(line)))
		}
	}()

	timedOut := monitorIdleTimeout(ctx, &bytesWritten, idleTimeout, cmd.Process.Pid, doneCh)

	<-doneCh
	err = cmd.Wait()

	output := outputBuf.String()
	result.Output = output
	result.TimedOut = *timedOut

	if *timedOut {
		result.ExitCode = 124
		return result
	}

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	}

	result.Success = result.ExitCode == 0 && CheckAnsibleSuccess(output)

	if !result.Success {
		result.FailedHosts = ExtractFailedHosts(output)
		result.ErrorType, result.ErrorDetail = DetectErrorType(output)
	}

	return result
}

// monitorIdleTimeout watches for output stalls and kills the process if idle too long.
// Returns a pointer to a bool that is set to true if the process was killed.
func monitorIdleTimeout(ctx context.Context, bytesWritten *atomic.Int64, timeout time.Duration, pid int, doneCh <-chan struct{}) *bool {
	timedOut := new(bool)
	go func() {
		lastBytes := bytesWritten.Load()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		lastActivity := time.Now()

		for {
			select {
			case <-doneCh:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				current := bytesWritten.Load()
				if current > lastBytes {
					lastBytes = current
					lastActivity = time.Now()
				} else if time.Since(lastActivity) > timeout {
					slog.Error("idle timeout reached, killing playbook",
						"timeout", timeout, "pid", pid)
					*timedOut = true
					killProcessGroup(pid)
					return
				}
			}
		}
	}()
	return timedOut
}

func buildArgs(opts RunOptions, cfg *config.Config) []string {
	inventoryPath := filepath.Join(cfg.ProjectRoot, opts.Env+"-inventory")
	playbookPath := filepath.Join(cfg.ProjectRoot, "ansible", "playbooks", opts.Playbook)

	args := []string{
		"-i", inventoryPath,
	}

	for _, inv := range opts.Inventories {
		if !filepath.IsAbs(inv) {
			inv = filepath.Join(cfg.ProjectRoot, inv)
		}
		args = append(args, "-i", inv)
	}

	// Pass the lab config JSON directly so Ansible doesn't need the vars
	// plugin to resolve Jinja template paths from the inventory.
	if labConfig := cfg.LabConfigPath(); fileExists(labConfig) {
		args = append(args, "-e", "@"+labConfig)
	}

	args = append(args, "-e", "ansible_facts_gathering_timeout=60", playbookPath)

	if opts.Debug {
		args = append([]string{"-vvv"}, args...)
	}

	if opts.Limit != "" {
		args = append(args, "--limit", opts.Limit)
	}

	if opts.Forks > 0 {
		args = append(args, "--forks", fmt.Sprintf("%d", opts.Forks))
	}

	for k, v := range opts.ExtraVars {
		args = append(args, "-e", k+"="+v)
	}

	return args
}

func buildEnv(opts RunOptions, cfg *config.Config) ([]string, error) {
	env := os.Environ()

	ansibleEnv, err := cfg.AnsibleEnv()
	if err != nil {
		return nil, fmt.Errorf("ansible env: %w", err)
	}

	for k, v := range ansibleEnv {
		env = append(env, k+"="+v)
	}

	for k, v := range opts.ExtraEnv {
		env = append(env, k+"="+v)
	}

	return env, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// logRunOptions emits debug-level logs for extra-vars and env vars being
// passed to ansible-playbook, making it easy to trace variable sources.
func logRunOptions(opts RunOptions) {
	if len(opts.ExtraVars) > 0 {
		slog.Debug("ansible extra-vars from Go", "vars", opts.ExtraVars)
	}
	if len(opts.ExtraEnv) > 0 {
		slog.Debug("ansible extra env vars from Go", "vars", opts.ExtraEnv)
	}
}

func killProcessGroup(pid int) {
	pgid, err := syscall.Getpgid(pid)
	if err == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		time.Sleep(2 * time.Second)
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
