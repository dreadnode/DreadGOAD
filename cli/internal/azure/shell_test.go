package azure

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSplitCWDMarker(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		wantBody string
		wantCWD  string
	}{
		{
			name:     "no marker",
			stdout:   "hello world\n",
			wantBody: "hello world\n",
			wantCWD:  "",
		},
		{
			name:     "marker at end",
			stdout:   "PSChildName: Foo\n__DG_CWD__:C:\\Users\\Administrator\n",
			wantBody: "PSChildName: Foo",
			wantCWD:  "C:\\Users\\Administrator",
		},
		{
			name:     "marker only",
			stdout:   "__DG_CWD__:C:\\\n",
			wantBody: "",
			wantCWD:  "C:\\",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, cwd := splitCWDMarker(tt.stdout)
			if body != tt.wantBody {
				t.Errorf("body: got %q, want %q", body, tt.wantBody)
			}
			if cwd != tt.wantCWD {
				t.Errorf("cwd:  got %q, want %q", cwd, tt.wantCWD)
			}
		})
	}
}

func TestEscapePSDoubleQuoted(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`C:\Users\Foo`, `C:\Users\Foo`},
		{`weird"path`, "weird`\"path"},
		{`$env:Path`, "`$env:Path"},
		{"back`tick", "back``tick"},
	}
	for _, tt := range tests {
		if got := escapePSDoubleQuoted(tt.in); got != tt.want {
			t.Errorf("escape(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBuildScriptIncludesMarkerAndCWD(t *testing.T) {
	script := buildScript(`C:\Users\Administrator`, "Get-Process")
	if !strings.Contains(script, `Set-Location -LiteralPath "C:\Users\Administrator"`) {
		t.Errorf("expected Set-Location prefix, got: %s", script)
	}
	if !strings.HasSuffix(strings.TrimSpace(script), `Write-Output ("__DG_CWD__:" + $PWD.Path)`) {
		t.Errorf("expected trailing CWD marker emit, got: %s", script)
	}
	if !strings.Contains(script, "Get-Process") {
		t.Errorf("expected user line in script, got: %s", script)
	}
}

func TestBuildScriptNoCWDOmitsSetLocation(t *testing.T) {
	script := buildScript("", "Get-Process")
	if strings.Contains(script, "Set-Location") {
		t.Errorf("expected no Set-Location when cwd is empty, got: %s", script)
	}
}

// fakeOpener captures invocations and returns canned results in order.
type fakeOpener struct {
	calls   []string
	results []*CommandResult
}

func (f *fakeOpener) RunPowerShellCommand(_ context.Context, _, script string, _ time.Duration) (*CommandResult, error) {
	f.calls = append(f.calls, script)
	if len(f.calls) > len(f.results) {
		return &CommandResult{Status: "Success", Stdout: "__DG_CWD__:C:\\\n"}, nil
	}
	return f.results[len(f.calls)-1], nil
}

func TestRunShell_PersistsCWDBetweenLines(t *testing.T) {
	fake := &fakeOpener{
		results: []*CommandResult{
			{Status: "Success", Stdout: "directory listing here\n__DG_CWD__:C:\\Windows\n"},
			{Status: "Success", Stdout: "second output\n__DG_CWD__:C:\\Windows\\System32\n"},
		},
	}
	in := strings.NewReader("dir\ncd System32\nexit\n")
	var out, errOut bytes.Buffer

	err := runShell(context.Background(), fake, "/subscriptions/x/.../virtualMachines/dc01-vm", in, &out, &errOut)
	if err != nil {
		t.Fatalf("runShell error: %v", err)
	}

	if len(fake.calls) != 2 {
		t.Fatalf("expected 2 invocations, got %d: %v", len(fake.calls), fake.calls)
	}

	// First call: no Set-Location yet (we hadn't learned the CWD).
	if strings.Contains(fake.calls[0], "Set-Location") {
		t.Errorf("first call should not have Set-Location; got: %s", fake.calls[0])
	}
	// Second call: must Set-Location to whatever the first call reported.
	if !strings.Contains(fake.calls[1], `Set-Location -LiteralPath "C:\Windows"`) {
		t.Errorf("second call missing Set-Location to C:\\Windows; got: %s", fake.calls[1])
	}

	// Output should contain both bodies but not the marker lines.
	got := out.String()
	if !strings.Contains(got, "directory listing here") {
		t.Errorf("missing first cmd output in: %q", got)
	}
	if !strings.Contains(got, "second output") {
		t.Errorf("missing second cmd output in: %q", got)
	}
	if strings.Contains(got, "__DG_CWD__") {
		t.Errorf("marker leaked to user output: %q", got)
	}

	// Prompt should advance from VM name → C:\Windows after the first response.
	if !strings.Contains(got, "PS dc01-vm>") {
		t.Errorf("expected initial prompt with VM name, got: %q", got)
	}
	if !strings.Contains(got, "PS C:\\Windows>") {
		t.Errorf("expected updated prompt after first cmd, got: %q", got)
	}
}

func TestShortVMName(t *testing.T) {
	got := shortVMName("/subscriptions/abc/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/test-goad-dc01-vm")
	want := "test-goad-dc01-vm"
	if got != want {
		t.Errorf("shortVMName: got %q, want %q", got, want)
	}
}

// Ensure compile-time interface assertions trigger if names drift.
var _ shellOpener = (*Client)(nil)
var _ = fmt.Sprintf
