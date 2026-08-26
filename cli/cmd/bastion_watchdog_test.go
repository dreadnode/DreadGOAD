package cmd

import (
	"os"
	"strings"
	"testing"
)

func TestOpenBastionParentLifetimeRejectsClosedDescriptor(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "closed-descriptor")
	if err != nil {
		t.Fatalf("create temporary file: %v", err)
	}
	fd := file.Fd()
	if err := file.Close(); err != nil {
		t.Fatalf("close temporary file: %v", err)
	}

	parentLifetime, err := openBastionParentLifetime(fd)
	if err == nil {
		if parentLifetime != nil {
			_ = parentLifetime.Close()
		}
		t.Fatal("openBastionParentLifetime() error = nil, want invalid descriptor error")
	}
	if !strings.Contains(err.Error(), "validate parent lifetime descriptor") {
		t.Fatalf("openBastionParentLifetime() error = %q, want validation error", err)
	}
}
