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
	cfg := config.Get()
	result := &RunResult{}

	args := buildArgs(opts, cfg)
	env := buildEnv(opts, cfg)

	slog.Info("running playbook", "playbook", opts.Playbook, "args", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "ansible-playbook", args...)
	cmd.Env = env
	cmd.Dir = cfg.ProjectRoot
	// Set process group so we can kill the entire tree
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Capture output while streaming to stdout and log file
	var outputBuf bytes.Buffer
	writers := []io.Writer{&outputBuf, os.Stdout}

	if opts.LogFile != "" {
		if f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			writers = append(writers, f)
			defer func() { _ = f.Close() }()
		}
	}

	multiW := io.MultiWriter(writers...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		result.ExitCode = 1
		result.Output = fmt.Sprintf("failed to create stdout pipe: %v", err)
		return result
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout

	if err := cmd.Start(); err != nil {
		result.ExitCode = 1
		result.Output = fmt.Sprintf("failed to start ansible-playbook: %v", err)
		return result
	}

	// Monitor output with idle timeout
	var bytesWritten atomic.Int64
	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = time.Duration(cfg.IdleTimeout) * time.Second
	}

	// Stream output in a goroutine
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
		"-e", "ansible_facts_gathering_timeout=60",
		playbookPath,
	}

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

func buildEnv(opts RunOptions, cfg *config.Config) []string {
	env := os.Environ()

	for k, v := range cfg.AnsibleEnv() {
		env = append(env, k+"="+v)
	}

	for k, v := range opts.ExtraEnv {
		env = append(env, k+"="+v)
	}

	return env
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
