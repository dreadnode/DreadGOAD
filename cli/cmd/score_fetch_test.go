package cmd

import "testing"

func TestShellSingleQuote(t *testing.T) {
	cases := map[string]string{
		"/root/report.jsonl":       `'/root/report.jsonl'`,
		"/tmp/my report.jsonl":     `'/tmp/my report.jsonl'`,
		"":                         `''`,
		"a'b":                      `'a'\''b'`,
		"$(rm -rf /)":              `'$(rm -rf /)'`, // metacharacters stay literal inside quotes
		"; cat /etc/shadow":        `'; cat /etc/shadow'`,
	}
	for in, want := range cases {
		if got := shellSingleQuote(in); got != want {
			t.Errorf("shellSingleQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
