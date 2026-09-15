package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

func TestAzurePreflightRejectsMissingPSRPDependencies(t *testing.T) {
	binDir := filepath.Join(t.TempDir(), "bin with spaces")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pythonPath := filepath.Join(binDir, "ansible-python")
	files := map[string]string{
		"ansible": "#!/bin/sh\nprintf '%s\\n' 'ansible [core 2.20.8]'\n",
		"ansible-playbook": "#!/bin/sh\nprintf '%s\\n' " +
			"'  python version = 3.14.7 (main, build) [Clang] (" + pythonPath + ")'\n",
		"ansible-python": "#!/bin/sh\nexit 1\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(contents), 0o755); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	t.Setenv("PATH", binDir)

	err := preflightChecks(context.Background(), &config.Config{Provider: "azure"}, "")
	if err == nil {
		t.Fatal("preflightChecks() error = nil, want missing PSRP dependency error")
	}
	for _, want := range []string{
		"ansible PSRP dependency check failed",
		pythonPath,
		"requests[socks]",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("preflightChecks() error = %q, want it to contain %q", err, want)
		}
	}
}
