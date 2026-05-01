package azure

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// runCommandTimeout is the per-invocation deadline for an interactive line.
// Azure Run Command can take 5-15s end-to-end; allow some headroom but cap
// to keep a stuck command from hanging the REPL forever.
const runCommandTimeout = 5 * time.Minute

// shellOpener is the dependency seam used by tests to inject a fake REPL
// transport. It mirrors the small slice of *Client we actually need.
type shellOpener interface {
	RunPowerShellCommand(ctx context.Context, vmID, script string, timeout time.Duration) (*CommandResult, error)
}

// StartInteractiveShell opens a Run Command-backed REPL against vmID. Each line
// the user types is sent as a separate invocation. We persist $PWD between
// invocations by prepending `Set-Location <last>`, so navigation behaves like
// a shell session.
//
// The simulation is honest about what it isn't: there is no live stdin, no
// signal forwarding to the remote process, no streaming output. Each command
// is bounded by Run Command's 4096-byte output cap. For real-time interactive
// shells, deploy Azure Bastion and use `az network bastion ssh` directly.
func (c *Client) StartInteractiveShell(ctx context.Context, vmID string) error {
	return runShell(ctx, c, vmID, os.Stdin, os.Stdout, os.Stderr)
}

func runShell(ctx context.Context, opener shellOpener, vmID string, in io.Reader, out, errOut io.Writer) error {
	displayName := shortVMName(vmID)

	if err := writeBanner(out, displayName); err != nil {
		return err
	}

	// Trap SIGINT so a Ctrl+C interrupts the in-flight invocation rather
	// than killing the whole CLI mid-session.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT)
	defer signal.Stop(sigCh)

	cwd := "" // empty == "let the remote default win"
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	for {
		if _, err := fmt.Fprintf(out, "PS %s> ", displayOrBlank(cwd, displayName)); err != nil {
			return fmt.Errorf("write prompt: %w", err)
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			if _, err := fmt.Fprintln(out); err != nil {
				return fmt.Errorf("write newline: %w", err)
			}
			return nil
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			return nil
		}

		nextCWD, err := runOneInvocation(ctx, opener, vmID, cwd, line, sigCh, out, errOut)
		if err != nil {
			return err
		}
		if nextCWD != "" {
			cwd = nextCWD
		}
	}
}

func writeBanner(out io.Writer, displayName string) error {
	banner := []string{
		fmt.Sprintf("Azure Run Command shell on %s", displayName),
		"  - Each line runs as a separate Run Command invocation (~5-15s latency).",
		"  - Output is capped by Azure at 4096 bytes per stream.",
		"  - Type 'exit' or send EOF (Ctrl+D) to leave.",
		"",
	}
	for _, line := range banner {
		if _, err := fmt.Fprintln(out, line); err != nil {
			return fmt.Errorf("write banner: %w", err)
		}
	}
	return nil
}

// runOneInvocation runs a single REPL line as one Run Command invocation,
// honouring SIGINT cancellation, and writes any output. It returns the
// remote $PWD after the line ran (empty if unchanged or unavailable). A
// returned error is fatal to the REPL; per-invocation command errors are
// written to errOut and swallowed.
func runOneInvocation(ctx context.Context, opener shellOpener, vmID, cwd, line string, sigCh <-chan os.Signal, out, errOut io.Writer) (string, error) {
	invokeCtx, cancel := context.WithTimeout(ctx, runCommandTimeout)
	defer cancel()

	type runResult struct {
		res *CommandResult
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		r, e := opener.RunPowerShellCommand(invokeCtx, vmID, buildScript(cwd, line), runCommandTimeout)
		done <- runResult{res: r, err: e}
	}()

	var rr runResult
	select {
	case rr = <-done:
	case <-sigCh:
		cancel()
		if _, err := fmt.Fprintln(out, "^C (cancelling invocation)"); err != nil {
			return "", fmt.Errorf("write cancel notice: %w", err)
		}
		rr = <-done
	case <-ctx.Done():
		return "", ctx.Err()
	}

	if rr.err != nil {
		if _, err := fmt.Fprintf(errOut, "error: %v\n", rr.err); err != nil {
			return "", fmt.Errorf("write error: %w", err)
		}
		return "", nil
	}

	stdout, nextCWD := splitCWDMarker(rr.res.Stdout)
	if stdout != "" {
		if _, err := fmt.Fprintln(out, strings.TrimRight(stdout, "\n")); err != nil {
			return "", fmt.Errorf("write stdout: %w", err)
		}
	}
	if rr.res.Stderr != "" {
		if _, err := fmt.Fprintln(errOut, strings.TrimRight(rr.res.Stderr, "\n")); err != nil {
			return "", fmt.Errorf("write stderr: %w", err)
		}
	}
	return nextCWD, nil
}

// cwdMarker is emitted at the very end of stdout so the REPL can recover the
// remote working directory after each invocation. The leading `__DG_CWD__:`
// is highly unlikely to collide with real output.
const cwdMarker = "__DG_CWD__:"

// buildScript prepends a Set-Location to maintain $PWD across invocations and
// appends a trailing marker so the REPL can capture the resulting CWD. We
// double-quote the prior CWD with PowerShell escaping (` is the escape char).
func buildScript(cwd, userLine string) string {
	var b strings.Builder
	if cwd != "" {
		b.WriteString("Set-Location -LiteralPath \"")
		b.WriteString(escapePSDoubleQuoted(cwd))
		b.WriteString("\"\n")
	}
	b.WriteString(userLine)
	b.WriteString("\n")
	// $PWD.Path is the most reliable accessor; .Provider/.Drive aren't always set.
	b.WriteString("Write-Output (\"")
	b.WriteString(cwdMarker)
	b.WriteString("\" + $PWD.Path)\n")
	return b.String()
}

// splitCWDMarker pulls a trailing __DG_CWD__:<path> line off stdout and returns
// (stdoutWithoutMarker, cwd). If no marker is present, cwd is "".
func splitCWDMarker(stdout string) (string, string) {
	trimmed := strings.TrimRight(stdout, "\r\n")
	idx := strings.LastIndex(trimmed, cwdMarker)
	if idx < 0 {
		return stdout, ""
	}
	cwd := strings.TrimSpace(trimmed[idx+len(cwdMarker):])
	// Drop the marker line (and its trailing newline if any).
	body := trimmed[:idx]
	body = strings.TrimRight(body, "\r\n")
	return body, cwd
}

// escapePSDoubleQuoted escapes a path for inclusion in a PowerShell
// double-quoted string. PowerShell uses backtick as the escape char.
func escapePSDoubleQuoted(s string) string {
	s = strings.ReplaceAll(s, "`", "``")
	s = strings.ReplaceAll(s, "\"", "`\"")
	s = strings.ReplaceAll(s, "$", "`$")
	return s
}

// shortVMName extracts the VM resource name from a full Azure resource ID
// (.../virtualMachines/<name>). Falls back to the input if no slash is found.
func shortVMName(vmID string) string {
	if i := strings.LastIndex(vmID, "/"); i >= 0 && i < len(vmID)-1 {
		return vmID[i+1:]
	}
	return vmID
}

func displayOrBlank(cwd, fallback string) string {
	if cwd == "" {
		return fallback
	}
	return cwd
}
