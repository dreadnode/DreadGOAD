package scoreboard

import (
	"strings"
	"testing"
)

// TestParseNXCOutput verifies nxc smb output parsing: auth success, guest
// fallback detection, account lockout/disabled status codes, empty username
// rejection, and multi-line output handling.
func TestParseNXCOutput(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		user    string
		wantOK  bool
		wantSub string // substring in reason (empty = don't check)
	}{
		{
			name:   "success",
			out:    "SMB  10.0.0.1  445  DC01  [+] north.sevenkingdoms.local\\samwell.tarly:Heartsbane",
			user:   "samwell.tarly",
			wantOK: true,
		},
		{
			name:    "guest fallback",
			out:     "SMB  10.0.0.1  445  DC01  [+] north\\samwell.tarly:badpass (Guest)",
			user:    "samwell.tarly",
			wantOK:  false,
			wantSub: "Guest",
		},
		{
			name:    "account locked out",
			out:     "SMB  10.0.0.1  445  DC01  [-] north\\samwell.tarly:pass STATUS_ACCOUNT_LOCKED_OUT",
			user:    "samwell.tarly",
			wantOK:  false,
			wantSub: "locked out",
		},
		{
			name:    "account disabled",
			out:     "SMB  10.0.0.1  445  DC01  [-] north\\samwell.tarly:pass STATUS_ACCOUNT_DISABLED",
			user:    "samwell.tarly",
			wantOK:  false,
			wantSub: "disabled",
		},
		{
			name:    "empty username rejected",
			out:     "SMB  10.0.0.1  445  DC01  [+] whatever",
			user:    "",
			wantOK:  false,
			wantSub: "empty username",
		},
		{
			name:   "multi-line success on second line",
			out:    "SMB  10.0.0.1  445  DC01  [*] Connecting...\nSMB  10.0.0.1  445  DC01  [+] north\\jon.snow:iknownothing",
			user:   "jon.snow",
			wantOK: true,
		},
		{
			name:    "plus for different user",
			out:     "SMB  10.0.0.1  445  DC01  [+] north\\otheruser:pass",
			user:    "samwell.tarly",
			wantOK:  false,
			wantSub: "failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := parseNXCOutput(tt.out, tt.user)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v (reason: %s)", ok, tt.wantOK, reason)
			}
			if tt.wantSub != "" && !strings.Contains(reason, tt.wantSub) {
				t.Errorf("reason = %q, want substring %q", reason, tt.wantSub)
			}
		})
	}
}

// TestBuildNXCCommand verifies nxc command construction: domain vs local auth,
// password vs NT hash credential flags, and shell-safe quoting of adversarial
// input (single quotes, semicolons).
func TestBuildNXCCommand(t *testing.T) {
	tests := []struct {
		name     string
		ip       string
		user     string
		domain   string
		evidence string
		wantSub  []string // substrings that must appear
		wantNot  []string // substrings that must NOT appear
	}{
		{
			name:     "domain auth with password",
			ip:       "10.0.0.1",
			user:     "samwell.tarly",
			domain:   "north.sevenkingdoms.local",
			evidence: "Heartsbane",
			wantSub:  []string{"nxc smb", "-u", "-d", "-p", "Heartsbane"},
			wantNot:  []string{"--local-auth", "-H"},
		},
		{
			name:     "local auth empty domain",
			ip:       "10.0.0.2",
			user:     "admin",
			domain:   "",
			evidence: "Password1",
			wantSub:  []string{"--local-auth", "-p"},
			wantNot:  []string{"-d"},
		},
		{
			name:     "NT hash uses -H",
			ip:       "10.0.0.1",
			user:     "admin",
			domain:   "north.sevenkingdoms.local",
			evidence: "aad3b435b51404eeaad3b435b51404ee",
			wantSub:  []string{"-H"},
			wantNot:  []string{"-p"},
		},
		{
			name:     "shell injection in password",
			ip:       "10.0.0.1",
			user:     "test",
			domain:   "d.local",
			evidence: "pass'word;rm -rf /",
			wantSub:  []string{`pass'\''word;rm -rf /`}, // safely quoted
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := buildNXCCommand(tt.ip, tt.user, tt.domain, tt.evidence)
			for _, sub := range tt.wantSub {
				if !strings.Contains(cmd, sub) {
					t.Errorf("command missing %q:\n  %s", sub, cmd)
				}
			}
			for _, sub := range tt.wantNot {
				if strings.Contains(cmd, sub) {
					t.Errorf("command should not contain %q:\n  %s", sub, cmd)
				}
			}
		})
	}
}

// TestBuildSecretsdumpCommand verifies secretsdump.py command construction:
// explicit vs derived NetBIOS name, and NT hash passthrough.
func TestBuildSecretsdumpCommand(t *testing.T) {
	tests := []struct {
		name     string
		dcIP     string
		user     string
		domain   string
		netbios  string
		evidence string
		wantSub  []string
	}{
		{
			name:     "with explicit netbios",
			dcIP:     "10.0.0.1",
			user:     "administrator",
			domain:   "north.sevenkingdoms.local",
			netbios:  "NORTH",
			evidence: "Password1",
			wantSub:  []string{"secretsdump.py", "-just-dc-user", "NORTH/krbtgt", "-hashes"},
		},
		{
			name:     "netbios derived from FQDN",
			dcIP:     "10.0.0.1",
			user:     "administrator",
			domain:   "essos.local",
			netbios:  "",
			evidence: "Password1",
			wantSub:  []string{"ESSOS/krbtgt"},
		},
		{
			name:     "NT hash passed through",
			dcIP:     "10.0.0.1",
			user:     "admin",
			domain:   "d.local",
			netbios:  "D",
			evidence: "aad3b435b51404eeaad3b435b51404ee",
			wantSub:  []string{"-hashes", "aad3b435b51404eeaad3b435b51404ee"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := buildSecretsdumpCommand(tt.dcIP, tt.user, tt.domain, tt.netbios, tt.evidence)
			for _, sub := range tt.wantSub {
				if !strings.Contains(cmd, sub) {
					t.Errorf("command missing %q:\n  %s", sub, cmd)
				}
			}
		})
	}
}

// TestIsLocalAccount verifies the heuristic that distinguishes local accounts
// (empty, ".", bare hostname) from domain accounts (FQDN with dots).
func TestIsLocalAccount(t *testing.T) {
	tests := []struct {
		domain string
		want   bool
	}{
		{"", true},
		{".", true},
		{"CASTELBLACK", true},
		{"north.sevenkingdoms.local", false},
	}
	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			if got := isLocalAccount(tt.domain); got != tt.want {
				t.Errorf("isLocalAccount(%q) = %v, want %v", tt.domain, got, tt.want)
			}
		})
	}
}

// TestNetbiosFromFQDN verifies NetBIOS name derivation from AD FQDNs by
// extracting and uppercasing the first DNS label.
func TestNetbiosFromFQDN(t *testing.T) {
	tests := []struct {
		fqdn string
		want string
	}{
		{"north.sevenkingdoms.local", "NORTH"},
		{"essos.local", "ESSOS"},
		{"singlelabel", "SINGLELABEL"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.fqdn, func(t *testing.T) {
			if got := netbiosFromFQDN(tt.fqdn); got != tt.want {
				t.Errorf("netbiosFromFQDN(%q) = %q, want %q", tt.fqdn, got, tt.want)
			}
		})
	}
}

// TestShellQuote verifies POSIX single-quote escaping for safe shell command
// construction: embedded quotes, metacharacters, and empty strings.
func TestShellQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"simple", "hello", "'hello'"},
		{"empty", "", "''"},
		{"embedded single quote", "it's", `'it'\''s'`},
		{"shell metacharacters", "a;b|c$d`e`", "'a;b|c$d`e`'"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shellQuote(tt.in); got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
