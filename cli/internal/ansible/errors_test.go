package ansible

import (
	"testing"
)

func TestDetectErrorType(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantType ErrorType
		wantMsg  string
	}{
		{
			name:     "fact gathering with setup failure",
			output:   `FAILED! => {"msg": "MODULE FAILURE", "module_stdout": ""} setup`,
			wantType: ErrFactGathering,
			wantMsg:  "fact gathering",
		},
		{
			name:     "fact gathering with invalid control character",
			output:   "Invalid control character at: line 1 column 2",
			wantType: ErrFactGathering,
			wantMsg:  "fact gathering",
		},
		{
			name:     "fact gathering with module deserialization",
			output:   "Module result deserialization failed for some task",
			wantType: ErrFactGathering,
			wantMsg:  "fact gathering",
		},
		{
			name:     "fact gathering with legacy setup failure",
			output:   "modules failed to execute: ansible.legacy.setup",
			wantType: ErrFactGathering,
			wantMsg:  "fact gathering",
		},
		{
			name:     "network adapter not found",
			output:   "No MSFT_NetAdapter objects found with property 'Name' equal to 'Ethernet3'",
			wantType: ErrNetworkAdapter,
			wantMsg:  "network adapter",
		},
		{
			name:     "SSM file transfer error",
			output:   "failed to transfer file to remote host",
			wantType: ErrSSMTransfer,
			wantMsg:  "SSM file transfer",
		},
		{
			name:     "SSM target not connected",
			output:   "An error occurred (TargetNotConnected) when calling the SendCommand operation",
			wantType: ErrSSMReconnection,
			wantMsg:  "SSM target not connected",
		},
		{
			name:     "SSM host not connected",
			output:   "host is not connected to SSM",
			wantType: ErrSSMReconnection,
			wantMsg:  "not connected",
		},
		{
			name:     "reboot timeout",
			output:   "Timed out waiting for last boot time",
			wantType: ErrSSMReconnection,
			wantMsg:  "reboot timeout",
		},
		{
			name:     "timeout waiting for reboot",
			output:   "timeout waiting for system to reboot",
			wantType: ErrSSMReconnection,
			wantMsg:  "reboot timeout",
		},
		{
			name:     "PowerShell non-interactive mode",
			output:   "Windows PowerShell is in NonInteractive mode. Read and Prompt",
			wantType: ErrPowerShell,
			wantMsg:  "PowerShell interactive",
		},
		{
			name:     "SSM user disabled",
			output:   "The ssm-user account is disabled on this instance",
			wantType: ErrSSMUserAccount,
			wantMsg:  "SSM user account",
		},
		{
			name:     "MSI installer rc 1603",
			output:   "fatal: [DC01]: FAILED! => {\"changed\": true, \"rc: 1603\"}",
			wantType: ErrMSIInstaller,
			wantMsg:  "MSI installer",
		},
		{
			name:     "MSI installer rc 3010",
			output:   "fatal: [DC01]: FAILED! => {\"changed\": true, \"rc: 3010\"}",
			wantType: ErrMSIInstaller,
			wantMsg:  "MSI installer",
		},
		{
			name:     "unclassified error with fatal line",
			output:   "fatal: [DC01]: FAILED! => {\"msg\": \"some unknown error\"}",
			wantType: ErrUnclassified,
		},
		{
			name:     "unclassified error no fatal",
			output:   "something went wrong",
			wantType: ErrUnclassified,
			wantMsg:  "unknown error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotMsg := DetectErrorType(tt.output)
			if gotType != tt.wantType {
				t.Errorf("DetectErrorType() type = %q, want %q", gotType, tt.wantType)
			}
			if tt.wantMsg != "" {
				if len(gotMsg) == 0 {
					t.Errorf("DetectErrorType() msg is empty, want to contain %q", tt.wantMsg)
				}
			}
		})
	}
}

func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		patterns []string
		want     bool
	}{
		{
			name:     "plain string match",
			s:        "hello world",
			patterns: []string{"world"},
			want:     true,
		},
		{
			name:     "no match",
			s:        "hello world",
			patterns: []string{"foo", "bar"},
			want:     false,
		},
		{
			name:     "regex pattern match",
			s:        "ssm-user is disabled",
			patterns: []string{"ssm-user.*disabled"},
			want:     true,
		},
		{
			name:     "regex pattern no match",
			s:        "ssm-user is active",
			patterns: []string{"ssm-user.*disabled"},
			want:     false,
		},
		{
			name:     "multiple patterns first matches",
			s:        "error: TargetNotConnected",
			patterns: []string{"TargetNotConnected", "is not connected"},
			want:     true,
		},
		{
			name:     "multiple patterns second matches",
			s:        "host is not connected",
			patterns: []string{"TargetNotConnected", "is not connected"},
			want:     true,
		},
		{
			name:     "empty input",
			s:        "",
			patterns: []string{"foo"},
			want:     false,
		},
		{
			name:     "empty patterns",
			s:        "hello",
			patterns: []string{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsAny(tt.s, tt.patterns...)
			if got != tt.want {
				t.Errorf("containsAny() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractFatalContext(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "no fatal line",
			output: "TASK [some task]\nok: [DC01]\n",
			want:   "unknown error",
		},
		{
			name:   "fatal line with msg",
			output: "TASK [failing]\nfatal: [DC01]: FAILED! => {\"msg\": \"broken\"}\nrc: 1",
			want:   "fatal: [DC01]: FAILED! => {\"msg\": \"broken\"}\nrc: 1",
		},
		{
			name: "fatal line truncated to 120 chars",
			output: "fatal: " + string(make([]byte, 200)),
			want: "fatal: " + string(make([]byte, 113)),
		},
		{
			name:   "FAILED in last line as fallback",
			output: "some output\nTask FAILED with error",
			want:   "Task FAILED with error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFatalContext(tt.output)
			if got != tt.want {
				t.Errorf("extractFatalContext() = %q, want %q", got, tt.want)
			}
		})
	}
}
