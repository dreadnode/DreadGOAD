package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestGet_ReturnsDefault(t *testing.T) {
	// Reset global logger so Get() falls back to slog.Default().
	logger = nil
	got := Get()
	if got == nil {
		t.Fatal("Get() returned nil")
	}
}

func TestGet_ReturnsSameInstance(t *testing.T) {
	// After Init, Get() should return the configured logger.
	Init(false, "", "test")
	first := Get()
	second := Get()
	if first != second {
		t.Error("Get() returned different instances on successive calls")
	}
}

func TestInit_DebugLevel(t *testing.T) {
	Init(true, "", "test")
	got := Get()
	if got == nil {
		t.Fatal("Init(debug=true) produced nil logger")
	}
}

func TestInit_InfoLevel(t *testing.T) {
	Init(false, "", "test")
	got := Get()
	if got == nil {
		t.Fatal("Init(debug=false) produced nil logger")
	}
}

func TestInit_WithLogDir(t *testing.T) {
	dir := t.TempDir()
	Init(false, dir, "myenv")

	// A log file should have been created in the temp dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected a log file to be created in logDir, got none")
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".log" {
			t.Errorf("unexpected file %q in logDir", e.Name())
		}
	}
}

func TestInit_SetsDefault(t *testing.T) {
	Init(false, "", "test")
	// slog.Default() should now be the logger we configured.
	if slog.Default() == nil {
		t.Fatal("slog.Default() is nil after Init")
	}
}
