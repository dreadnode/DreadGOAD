package terragrunt

import (
	"os"
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
