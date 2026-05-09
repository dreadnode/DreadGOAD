package provider

import (
	"context"
	"strings"
	"time"
)

// RetryCommandOptions configures retry behavior for RunCommandWithRetry.
type RetryCommandOptions struct {
	MaxRetries int           // number of additional attempts after the first (0 = no retries)
	RetryDelay time.Duration // pause between retries
}

// RunCommandWithRetry executes a command via the provider and retries on
// transient connection-level failures (WinRM unreachable, SSM timeouts, etc.).
// It returns the result, the 1-based attempt number that produced it, and any
// error. The optional onRetry callback is invoked before each retry with the
// 1-based retry number (1 = first retry, 2 = second, etc.).
func RunCommandWithRetry(
	ctx context.Context,
	prov Provider,
	instanceID, command string,
	timeout time.Duration,
	opts RetryCommandOptions,
	onRetry func(attempt int),
) (*CommandResult, int, error) {
	var result *CommandResult
	var lastErr error

	for attempt := range opts.MaxRetries + 1 {
		if attempt > 0 {
			if onRetry != nil {
				onRetry(attempt)
			}
			select {
			case <-ctx.Done():
				return nil, attempt, ctx.Err()
			case <-time.After(opts.RetryDelay):
			}
		}

		result, lastErr = prov.RunCommand(ctx, instanceID, command, timeout)
		if !IsTransientFailure(lastErr, result) {
			return result, attempt + 1, lastErr
		}
	}

	return result, opts.MaxRetries + 1, lastErr
}

// IsTransientFailure reports whether a RunCommand result looks like a
// connection-level failure (WinRM unreachable, SSM timeout, etc.) that is
// worth retrying, as opposed to a command that executed but returned a bad
// result.
func IsTransientFailure(err error, result *CommandResult) bool {
	if err != nil {
		return true
	}
	if result == nil || result.Status == "Success" {
		return false
	}
	combined := strings.ToLower(result.Stdout + " " + result.Stderr)
	for _, pattern := range transientPatterns {
		if strings.Contains(combined, pattern) {
			return true
		}
	}
	return false
}

// transientPatterns are lowercase substrings that indicate a connection-level
// failure rather than a genuine command failure.
var transientPatterns = []string{
	"unreachable",
	"targetnotconnected",
	"is not connected",
	"connection refused",
	"connection timed out",
	"connection reset",
	"connection timeout",
	"winrm",
	"kerberos",
	"httpsconnectionpool",
	"urlopen error",
	"no route to host",
	"network is unreachable",
	"timed out",
}
