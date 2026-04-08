package ansible

import (
	"testing"
)

func TestCheckAnsibleSuccess(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name: "all hosts ok",
			output: `PLAY RECAP *********************************************************************
DC01                       : ok=15   changed=3    unreachable=0    failed=0    skipped=2    rescued=0    ignored=0
DC02                       : ok=12   changed=1    unreachable=0    failed=0    skipped=1    rescued=0    ignored=0`,
			want: true,
		},
		{
			name: "host with failures in recap",
			output: `PLAY RECAP *********************************************************************
DC01                       : ok=10   changed=2    unreachable=0    failed=3    skipped=1    rescued=0    ignored=0`,
			want: false,
		},
		{
			name: "host unreachable in recap",
			output: `PLAY RECAP *********************************************************************
DC01                       : ok=0    changed=0    unreachable=1    failed=0    skipped=0    rescued=0    ignored=0`,
			want: false,
		},
		{
			name: "fatal error followed by ignoring",
			output: `TASK [some task]
fatal: [DC01]: FAILED! => {"msg": "non-critical error"}
...ignoring
PLAY RECAP *********************************************************************
DC01                       : ok=10   changed=2    unreachable=0    failed=0    skipped=1    rescued=0    ignored=1`,
			want: true,
		},
		{
			name: "fatal error not ignored",
			output: `TASK [some task]
fatal: [DC01]: FAILED! => {"msg": "critical error"}
NO MORE HOSTS LEFT *************************************************************`,
			want: false,
		},
		{
			name: "retry indicator present",
			output: `PLAY RECAP *********************************************************************
DC01                       : ok=5    changed=0    unreachable=0    failed=0    skipped=0    rescued=0    ignored=0
to retry, use: --limit @/path/to/retry.yml`,
			want: false,
		},
		{
			name:   "empty output",
			output: "",
			want:   true,
		},
		{
			name:   "no recap section",
			output: "TASK [Gathering Facts]\nok: [DC01]",
			want:   true,
		},
		{
			name: "failed=10 double digits",
			output: `PLAY RECAP *********************************************************************
DC01                       : ok=5    changed=0    unreachable=0    failed=10   skipped=0    rescued=0    ignored=0`,
			want: false,
		},
		{
			name: "failed=0 should pass",
			output: `PLAY RECAP *********************************************************************
DC01                       : ok=5    changed=0    unreachable=0    failed=0    skipped=0    rescued=0    ignored=0`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckAnsibleSuccess(tt.output)
			if got != tt.want {
				t.Errorf("CheckAnsibleSuccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractFailedHosts(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   []string
	}{
		{
			name: "single failed host",
			output: `PLAY RECAP *********************************************************************
DC01                       : ok=10   changed=2    unreachable=0    failed=3    skipped=1    rescued=0    ignored=0
DC02                       : ok=15   changed=3    unreachable=0    failed=0    skipped=2    rescued=0    ignored=0`,
			want: []string{"DC01"},
		},
		{
			name: "multiple failed hosts",
			output: `PLAY RECAP *********************************************************************
DC01                       : ok=10   changed=2    unreachable=0    failed=3    skipped=1    rescued=0    ignored=0
DC02                       : ok=5    changed=1    unreachable=0    failed=1    skipped=0    rescued=0    ignored=0
SRV01                      : ok=15   changed=3    unreachable=0    failed=0    skipped=2    rescued=0    ignored=0`,
			want: []string{"DC01", "DC02"},
		},
		{
			name: "no failed hosts",
			output: `PLAY RECAP *********************************************************************
DC01                       : ok=10   changed=2    unreachable=0    failed=0    skipped=1    rescued=0    ignored=0`,
			want: nil,
		},
		{
			name:   "empty output",
			output: "",
			want:   nil,
		},
		{
			name: "deduplicated hosts",
			output: `DC01                       : ok=10   changed=2    unreachable=0    failed=3    skipped=1    rescued=0    ignored=0
DC01                       : ok=5    changed=0    unreachable=0    failed=1    skipped=0    rescued=0    ignored=0`,
			want: []string{"DC01"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractFailedHosts(tt.output)
			if len(got) != len(tt.want) {
				t.Fatalf("ExtractFailedHosts() returned %d hosts %v, want %d hosts %v", len(got), got, len(tt.want), tt.want)
			}
			for i, host := range got {
				if host != tt.want[i] {
					t.Errorf("ExtractFailedHosts()[%d] = %q, want %q", i, host, tt.want[i])
				}
			}
		})
	}
}
