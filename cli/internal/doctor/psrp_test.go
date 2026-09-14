package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckPyPSRPUsesAnsiblePython(t *testing.T) {
	tests := []struct {
		name       string
		pythonExit int
		wantStatus string
		wantText   string
	}{
		{name: "dependencies installed", wantStatus: "pass", wantText: "installed for"},
		{name: "dependency missing", pythonExit: 1, wantStatus: "fail", wantText: "requests[socks]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := filepath.Join(t.TempDir(), "bin (with spaces)")
			if err := os.MkdirAll(binDir, 0o755); err != nil {
				t.Fatal(err)
			}
			pythonPath := filepath.Join(binDir, "ansible-python")
			pythonScript := fmt.Sprintf(
				"#!/bin/sh\nif [ \"$1\" != \"-c\" ] || [ \"$2\" != \"import pypsrp; import socks\" ]; then exit 2; fi\nexit %d\n",
				tt.pythonExit,
			)
			if err := os.WriteFile(pythonPath, []byte(pythonScript), 0o755); err != nil {
				t.Fatal(err)
			}
			ansibleScript := "#!/bin/sh\nprintf '%s\\n' 'ansible-playbook [core 2.20.8]' " +
				"'  python version = 3.14.7 (main, build) [Clang] (" + pythonPath + ")'\n"
			if err := os.WriteFile(filepath.Join(binDir, "ansible-playbook"), []byte(ansibleScript), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", binDir)

			result := checkPyPSRP()
			if result.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q; message: %s", result.Status, tt.wantStatus, result.Message)
			}
			if !strings.Contains(result.Message, tt.wantText) {
				t.Errorf("message = %q, want it to contain %q", result.Message, tt.wantText)
			}
			if !strings.Contains(result.Message, pythonPath) {
				t.Errorf("message = %q, want Ansible Python path %q", result.Message, pythonPath)
			}
		})
	}
}

func TestAnsiblePythonExecutableRejectsUnparseableOutput(t *testing.T) {
	binDir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' 'ansible-playbook version unavailable'\n"
	if err := os.WriteFile(filepath.Join(binDir, "ansible-playbook"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	_, err := ansiblePythonExecutable()
	if err == nil || !strings.Contains(err.Error(), "python executable not found") {
		t.Fatalf("error = %v, want missing Python executable error", err)
	}
}
