package terragrunt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestBuildArgs_Init(t *testing.T) {
	opts := Options{Action: "init"}
	args := buildArgs(opts)
	if len(args) < 2 {
		t.Fatalf("expected at least 2 args, got %v", args)
	}
	if args[0] != "init" {
		t.Errorf("args[0] = %q, want %q", args[0], "init")
	}
	found := false
	for _, a := range args {
		if a == "-upgrade" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected -upgrade in args for init, got %v", args)
	}
}

func TestBuildArgs_Apply_AutoApprove(t *testing.T) {
	opts := Options{Action: "apply", AutoApprove: true}
	args := buildArgs(opts)
	found := false
	for _, a := range args {
		if a == "-auto-approve" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected -auto-approve in args, got %v", args)
	}
}

func TestBuildArgs_Apply_NoAutoApprove(t *testing.T) {
	opts := Options{Action: "apply", AutoApprove: false}
	args := buildArgs(opts)
	for _, a := range args {
		if a == "-auto-approve" {
			t.Errorf("unexpected -auto-approve in args: %v", args)
		}
	}
}

func TestBuildArgs_Destroy_AutoApprove(t *testing.T) {
	opts := Options{Action: "destroy", AutoApprove: true}
	args := buildArgs(opts)
	found := false
	for _, a := range args {
		if a == "-auto-approve" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected -auto-approve for destroy with AutoApprove=true, got %v", args)
	}
}

func TestBuildArgs_Plan(t *testing.T) {
	opts := Options{Action: "plan", AutoApprove: true}
	args := buildArgs(opts)
	// plan should NOT get -auto-approve even if AutoApprove=true
	for _, a := range args {
		if a == "-auto-approve" {
			t.Errorf("plan should not have -auto-approve, got %v", args)
		}
	}
}

func TestBuildArgs_NonInteractive(t *testing.T) {
	opts := Options{Action: "apply", NonInteractive: true}
	args := buildArgs(opts)
	found := false
	for _, a := range args {
		if a == "--non-interactive" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected --non-interactive in args, got %v", args)
	}
}

func TestBuildEnv_WithTerraformBinary(t *testing.T) {
	opts := Options{TerraformBinary: "/usr/local/bin/tofu"}
	env := buildEnv(opts)
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, "TG_TF_PATH=") {
			found = true
			if e != "TG_TF_PATH=/usr/local/bin/tofu" {
				t.Errorf("TG_TF_PATH = %q, want %q", e, "TG_TF_PATH=/usr/local/bin/tofu")
			}
		}
	}
	if !found {
		t.Errorf("TG_TF_PATH not set in env, got %v", env)
	}
}

func TestBuildEnv_WithoutTerraformBinary(t *testing.T) {
	opts := Options{}
	env := buildEnv(opts)
	for _, e := range env {
		if strings.HasPrefix(e, "TG_TF_PATH=") {
			t.Errorf("unexpected TG_TF_PATH in env when TerraformBinary is empty: %v", e)
		}
	}
}

func TestOutputWriter_NoLogFile(t *testing.T) {
	w, cleanup, err := outputWriter("")
	if err != nil {
		t.Fatalf("outputWriter: %v", err)
	}
	defer cleanup()
	if w == nil {
		t.Fatal("expected non-nil writer")
	}
}

func TestOutputWriter_WithLogFile(t *testing.T) {
	dir := t.TempDir()
	logFile := dir + "/test.log"
	w, cleanup, err := outputWriter(logFile)
	if err != nil {
		t.Fatalf("outputWriter: %v", err)
	}
	defer cleanup()
	if w == nil {
		t.Fatal("expected non-nil writer")
	}
}

func TestOutputWriter_InvalidDir(t *testing.T) {
	// Create a regular file where a directory is expected, so MkdirAll fails.
	dir := t.TempDir()
	blockingFile := dir + "/notadir"
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, _, err := outputWriter(blockingFile + "/sub/test.log")
	if err == nil {
		t.Fatal("expected error for invalid log path, got nil")
	}
}

func TestStateLockID(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name: "Terraform lock info",
			output: `Error: Error acquiring the state lock

Lock Info:
  ID:        2f9a3fa7-14a9-4e74-a2ef-235a43f1bf00
  Path:      state/prod.tfstate`,
			want: "2f9a3fa7-14a9-4e74-a2ef-235a43f1bf00",
		},
		{
			name: "OpenTofu output with Terragrunt prefix and ANSI",
			output: "\x1b[31mERROR\x1b[0m [goad/dc01] Failed to acquire state lock\n" +
				"[goad/dc01] Lock Info:\n[goad/dc01] │   ID: azure-lease-123 │\n",
			want: "azure-lease-123",
		},
		{
			name:   "unrelated ID is ignored without lock error",
			output: "request failed\nID: request-123\n",
		},
		{
			name:   "lock error without ID",
			output: "Error acquiring the state lock: backend unavailable",
		},
		{
			name:   "unsafe partial ID is rejected",
			output: "Error acquiring the state lock\nLock Info:\n  ID: abc;rm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stateLockID(tc.output); got != tc.want {
				t.Errorf("stateLockID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTailBufferRetainsBoundedSuffix(t *testing.T) {
	tail := newTailBuffer(5)
	for _, chunk := range []string{"abc", "def", "gh"} {
		if n, err := tail.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = (%d, %v)", chunk, n, err)
		}
	}
	if got := tail.String(); got != "defgh" {
		t.Errorf("tail = %q, want %q", got, "defgh")
	}

	if _, err := tail.Write([]byte("0123456789")); err != nil {
		t.Fatalf("large Write() error: %v", err)
	}
	if got := tail.String(); got != "56789" {
		t.Errorf("tail after large write = %q, want %q", got, "56789")
	}
}

func TestCommandErrorAddsSafeUnlockHint(t *testing.T) {
	cause := errors.New("exit status 1")
	output := "Error acquiring the state lock\nLock Info:\n  ID: lock-123\n"
	err := commandError("terragrunt apply failed", cause, output, "/opt/terragrunt tools/terragrunt")

	if !errors.Is(err, cause) {
		t.Error("command error does not unwrap to process failure")
	}
	for _, want := range []string{
		"Terraform state lock detected (ID: lock-123)",
		"confirming no other operation is running",
		"'/opt/terragrunt tools/terragrunt' force-unlock lock-123",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}

	plain := commandError("terragrunt apply failed", cause, "ordinary failure", "terragrunt")
	if plain.Error() != "terragrunt apply failed: exit status 1" {
		t.Errorf("ordinary error changed: %q", plain)
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"/opt/homebrew/bin/terragrunt": "/opt/homebrew/bin/terragrunt",
		"/opt/terragrunt tools/tg":     "'/opt/terragrunt tools/tg'",
		"/tmp/operator's/tg":           `'/tmp/operator'"'"'s/tg'`,
	}
	for input, want := range tests {
		if got := shellQuote(input); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRunnersSurfaceStateLockHint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX shell fixture")
	}
	dir := t.TempDir()
	binary := filepath.Join(dir, "fake-terragrunt")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' 'Error acquiring the state lock' >&2\n" +
		"printf '%s\\n' 'Lock Info:' '  ID: integration-lock-456' >&2\n" +
		"exit 1\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	opts := Options{Action: "apply", WorkDir: dir, TerragruntBinary: binary}
	for _, tc := range []struct {
		name string
		run  func(context.Context, Options) error
	}{
		{name: "single module", run: Run},
		{name: "run all", run: RunAll},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(context.Background(), opts)
			want := binary + " force-unlock integration-lock-456"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("runner error = %v, want state-lock recovery hint", err)
			}
		})
	}
}
