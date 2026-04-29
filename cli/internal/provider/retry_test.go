package provider

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// mockProvider implements Provider for testing RunCommandWithRetry.
// Only RunCommand is exercised; other methods panic if called.
type mockProvider struct {
	calls   int
	results []mockResult
}

type mockResult struct {
	result *CommandResult
	err    error
}

func (m *mockProvider) RunCommand(_ context.Context, _, _ string, _ time.Duration) (*CommandResult, error) {
	if m.calls >= len(m.results) {
		return nil, fmt.Errorf("unexpected call %d", m.calls)
	}
	r := m.results[m.calls]
	m.calls++
	return r.result, r.err
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) VerifyCredentials(context.Context) (string, error) {
	return "", nil
}
func (m *mockProvider) DiscoverInstances(context.Context, string) ([]Instance, error) {
	return nil, nil
}
func (m *mockProvider) DiscoverAllInstances(context.Context, string) ([]Instance, error) {
	return nil, nil
}
func (m *mockProvider) FindInstanceByHostname(context.Context, string, string) (*Instance, error) {
	return nil, nil
}
func (m *mockProvider) StartInstances(context.Context, []string) error       { return nil }
func (m *mockProvider) StopInstances(context.Context, []string) error        { return nil }
func (m *mockProvider) WaitForInstanceStopped(context.Context, string) error { return nil }
func (m *mockProvider) DestroyInstances(context.Context, []string) error     { return nil }
func (m *mockProvider) RunCommandOnMultiple(context.Context, []string, string, time.Duration) (map[string]*CommandResult, error) {
	return nil, nil
}

func TestIsTransientFailure(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		result *CommandResult
		want   bool
	}{
		{
			name: "go error is transient",
			err:  fmt.Errorf("send command to i-123: connection refused"),
			want: true,
		},
		{
			name:   "nil result is transient",
			result: nil,
			err:    fmt.Errorf("timeout"),
			want:   true,
		},
		{
			name:   "success is not transient",
			result: &CommandResult{Status: "Success", Stdout: "ok"},
			want:   false,
		},
		{
			name:   "failed with UNREACHABLE is transient",
			result: &CommandResult{Status: "Failed", Stdout: "dc01 | UNREACHABLE! =>"},
			want:   true,
		},
		{
			name:   "failed with WinRM error is transient",
			result: &CommandResult{Status: "Failed", Stderr: "winrm connection timeout"},
			want:   true,
		},
		{
			name:   "failed with TargetNotConnected is transient",
			result: &CommandResult{Status: "Failed", Stderr: "TargetNotConnected"},
			want:   true,
		},
		{
			name:   "failed with kerberos error is transient",
			result: &CommandResult{Status: "Failed", Stderr: "kerberos auth failed"},
			want:   true,
		},
		{
			name:   "failed with HTTPSConnectionPool is transient",
			result: &CommandResult{Status: "Failed", Stderr: "HTTPSConnectionPool: max retries exceeded"},
			want:   true,
		},
		{
			name:   "failed with real command error is not transient",
			result: &CommandResult{Status: "Failed", Stdout: "dc01 | FAILED | rc=1 >>\nGet-ADDomainController: cannot find object"},
			want:   false,
		},
		{
			name:   "failed with empty output is not transient",
			result: &CommandResult{Status: "Failed"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTransientFailure(tt.err, tt.result)
			if got != tt.want {
				t.Errorf("IsTransientFailure() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunCommandWithRetry_SuccessFirstAttempt(t *testing.T) {
	mp := &mockProvider{results: []mockResult{
		{result: &CommandResult{Status: "Success", Stdout: "ok"}},
	}}

	result, attempts, err := RunCommandWithRetry(
		context.Background(), mp, "i-1", "echo hi", 10*time.Second,
		RetryCommandOptions{MaxRetries: 3, RetryDelay: time.Millisecond},
		nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if result.Stdout != "ok" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "ok")
	}
	if mp.calls != 1 {
		t.Errorf("provider called %d times, want 1", mp.calls)
	}
}

func TestRunCommandWithRetry_TransientThenSuccess(t *testing.T) {
	mp := &mockProvider{results: []mockResult{
		{err: fmt.Errorf("connection refused")},
		{result: &CommandResult{Status: "Failed", Stdout: "dc01 | UNREACHABLE!"}},
		{result: &CommandResult{Status: "Success", Stdout: "ok"}},
	}}

	retryCount := 0
	result, attempts, err := RunCommandWithRetry(
		context.Background(), mp, "i-1", "echo hi", 10*time.Second,
		RetryCommandOptions{MaxRetries: 3, RetryDelay: time.Millisecond},
		func(attempt int) { retryCount = attempt },
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if result.Stdout != "ok" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "ok")
	}
	if retryCount != 2 {
		t.Errorf("last retry callback = %d, want 2", retryCount)
	}
}

func TestRunCommandWithRetry_AllRetriesExhausted(t *testing.T) {
	mp := &mockProvider{results: []mockResult{
		{err: fmt.Errorf("connection refused")},
		{err: fmt.Errorf("connection refused")},
		{err: fmt.Errorf("connection refused")},
	}}

	_, attempts, err := RunCommandWithRetry(
		context.Background(), mp, "i-1", "echo hi", 10*time.Second,
		RetryCommandOptions{MaxRetries: 2, RetryDelay: time.Millisecond},
		nil,
	)

	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	if mp.calls != 3 {
		t.Errorf("provider called %d times, want 3", mp.calls)
	}
}

func TestRunCommandWithRetry_NonTransientFailureNoRetry(t *testing.T) {
	mp := &mockProvider{results: []mockResult{
		{result: &CommandResult{Status: "Failed", Stdout: "Get-Service: not found"}},
	}}

	result, attempts, err := RunCommandWithRetry(
		context.Background(), mp, "i-1", "Get-Service foo", 10*time.Second,
		RetryCommandOptions{MaxRetries: 3, RetryDelay: time.Millisecond},
		nil,
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (should not retry non-transient)", attempts)
	}
	if result.Status != "Failed" {
		t.Errorf("status = %q, want %q", result.Status, "Failed")
	}
	if mp.calls != 1 {
		t.Errorf("provider called %d times, want 1", mp.calls)
	}
}

func TestRunCommandWithRetry_ZeroRetries(t *testing.T) {
	mp := &mockProvider{results: []mockResult{
		{err: fmt.Errorf("connection refused")},
	}}

	_, attempts, err := RunCommandWithRetry(
		context.Background(), mp, "i-1", "echo hi", 10*time.Second,
		RetryCommandOptions{MaxRetries: 0, RetryDelay: time.Millisecond},
		nil,
	)

	if err == nil {
		t.Fatal("expected error with zero retries")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
}

func TestRunCommandWithRetry_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mp := &mockProvider{results: []mockResult{
		{err: fmt.Errorf("connection refused")},
		{result: &CommandResult{Status: "Success", Stdout: "ok"}},
	}}

	// Cancel before the retry delay elapses.
	cancel()

	_, _, err := RunCommandWithRetry(
		ctx, mp, "i-1", "echo hi", 10*time.Second,
		RetryCommandOptions{MaxRetries: 3, RetryDelay: time.Hour},
		nil,
	)

	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
