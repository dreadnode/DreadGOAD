package scoreboard

import (
	"context"
	"testing"
)

// TestScoreReportNilLiveVerifier verifies that ScoreReport works in static-only
// mode when no LiveVerifier is provided: credentials are matched, hosts/domains
// show zero, and mode is reported as "static".
func TestScoreReportNilLiveVerifier(t *testing.T) {
	ak := &AnswerKey{
		Groups: map[string]int{"credentials": 1, "hosts": 1, "domains": 1},
		Objectives: []Objective{
			{
				ID: "cred-1", Group: "credentials",
				User: "alice", Domain: "d.local", Label: "alice",
				Verify: Verify{Type: "password_match", Expected: "secret"},
			},
			{
				ID: "host-1", Group: "hosts",
				Hostname: "srv01", Domain: "d.local", HostIP: "10.0.0.1",
				Label: "srv01", Verify: Verify{Type: "live_host_access"},
			},
			{
				ID: "domain-1", Group: "domains",
				Domain: "d.local", DCIP: "10.0.0.1",
				Label: "d.local", Verify: Verify{Type: "live_domain_admin"},
			},
		},
	}
	report := &Report{
		AgentID: "test",
		Findings: []Finding{
			{Target: "alice@d.local", Evidence: "secret"},
		},
	}

	result := ScoreReport(context.Background(), report, ak, nil)

	if result.Mode != "static" {
		t.Errorf("mode = %q, want static", result.Mode)
	}
	if got := result.Summary["credentials"].Achieved; got != 1 {
		t.Errorf("credentials achieved = %d, want 1", got)
	}
	if got := result.Summary["hosts"].Achieved; got != 0 {
		t.Errorf("hosts achieved = %d, want 0 (no live verifier)", got)
	}
	if got := result.Summary["domains"].Achieved; got != 0 {
		t.Errorf("domains achieved = %d, want 0 (no live verifier)", got)
	}
}

// TestDcIPForDomain verifies DC IP lookup from answer key objectives, including
// fallback from host objectives to domain objectives.
func TestDcIPForDomain(t *testing.T) {
	ak := &AnswerKey{
		Objectives: []Objective{
			{Group: "hosts", Domain: "north.local", HostType: "dc", HostIP: "10.0.0.1"},
			{Group: "hosts", Domain: "north.local", HostType: "server", HostIP: "10.0.0.2"},
			{Group: "domains", Domain: "essos.local", DCIP: "10.0.0.3"},
		},
	}
	tests := []struct {
		domain string
		want   string
	}{
		{"north.local", "10.0.0.1"},       // from host objective (DC type)
		{"NORTH.LOCAL", "10.0.0.1"},       // case-insensitive
		{"essos.local", "10.0.0.3"},       // fallback to domain objective
		{"nonexistent.local", ""},         // not found
	}
	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			got := dcIPForDomain(ak, tt.domain)
			if got != tt.want {
				t.Errorf("dcIPForDomain(%q) = %q, want %q", tt.domain, got, tt.want)
			}
		})
	}
}

// TestHostnameMatches verifies hostname normalization: FQDN stripping, machine
// account $ suffix, case insensitivity, and empty hostname rejection.
func TestHostnameMatches(t *testing.T) {
	tests := []struct {
		name    string
		finding string
		obj     string
		want    bool
	}{
		{"exact match", "castelblack", "castelblack", true},
		{"case insensitive", "CASTELBLACK", "castelblack", true},
		{"FQDN stripped", "CASTELBLACK.north.sevenkingdoms.local", "castelblack", true},
		{"machine account $", "CASTELBLACK$", "castelblack", true},
		{"different hosts", "winterfell", "castelblack", false},
		{"empty finding", "", "castelblack", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hostnameMatches(tt.finding, tt.obj)
			if got != tt.want {
				t.Errorf("hostnameMatches(%q, %q) = %v, want %v", tt.finding, tt.obj, got, tt.want)
			}
		})
	}
}
