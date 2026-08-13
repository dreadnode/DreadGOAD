package validate

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/labmap"
	"github.com/dreadnode/dreadgoad/internal/provider"
)

// kdcEnvelope wraps a payload the way registry_dword.ps1 does; runScriptJSON
// discards anything without the markers.
func kdcEnvelope(payload string) string {
	return "===BEGIN_JSON===\n" + payload + "\n===END_JSON===\n"
}

// kdcBindingLab models the GOAD shape that makes this check necessary: the CA
// (braavos) is a member server, so the KDC that validates its certificates is
// the DC of the CA's own domain (meereen), not the CA host.
func kdcBindingLab() *labmap.LabMap {
	return &labmap.LabMap{
		Hosts: map[string]labmap.HostInfo{
			"srv03": {NewHostname: "braavos", NewDomain: "essos.local"},
			"dc03":  {NewHostname: "meereen", NewDomain: "essos.local"},
		},
		HostConfigs: map[string]labmap.HostConfig{
			"srv03": {Hostname: "braavos", Type: "server", Domain: "essos.local"},
			"dc03":  {Hostname: "meereen", Type: "dc", Domain: "essos.local"},
		},
		DomainConfigs: map[string]labmap.DomainConfig{
			"essos.local": {DC: "dc03", CAServer: "braavos"},
		},
	}
}

// The CA-side EDITF_ATTRIBUTESUBJECTALTNAME2 bit does not establish that ESC6
// can win. ESC6 issues off the stock User template, so the SAN it injects rides
// alongside a security extension carrying the *requester's* SID. Only a KDC at
// StrongCertificateBindingEnforcement=0 ignores that extension; 1
// (Compatibility) still validates a present extension strictly and rejects the
// mismatch. Passing on anything but 0 reports an exploit that cannot land.
func TestCheckESC6KDCBinding(t *testing.T) {
	tests := []struct {
		name       string
		stdout     string
		wantStatus string
		wantDetail string
	}{
		{
			name:       "SCBE=0 is the only exploitable case",
			stdout:     `{"present":true,"value":0,"error":""}`,
			wantStatus: "PASS",
			wantDetail: "ESC6 exploitable",
		},
		{
			// Compatibility mode still validates a present security
			// extension, so the SID mismatch is rejected. This is the case
			// that separates ESC6 from ESC9: an ESC9 certificate has no
			// extension to validate and survives here.
			name:       "SCBE=1 compatibility rejects the SID mismatch",
			stdout:     `{"present":true,"value":1,"error":""}`,
			wantStatus: "FAIL",
			wantDetail: "NOT exploitable",
		},
		{
			name:       "SCBE=2 full enforcement rejects it too",
			stdout:     `{"present":true,"value":2,"error":""}`,
			wantStatus: "FAIL",
			wantDetail: "NOT exploitable",
		},
		{
			// An absent value is not "unknown, possibly permissive". The
			// built-in default has been Full Enforcement since KB5014754
			// (Feb 2025), and the essos KDC, which sets no value, was
			// measured refusing a certificate outright: event 39 at Error
			// level, no TGT. Reporting WARN here would leave a dead route
			// looking merely unverified.
			name:       "absent value means Full Enforcement, not exploitable",
			stdout:     `{"present":false,"value":0,"error":""}`,
			wantStatus: "FAIL",
			wantDetail: "shipped default of Full Enforcement",
		},
		{
			name:       "script error warns",
			stdout:     `{"present":false,"value":0,"error":"Access denied"}`,
			wantStatus: "WARN",
			wantDetail: "query error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
				return &provider.CommandResult{Status: "Success", Stdout: kdcEnvelope(tt.stdout)}, nil
			})
			v.silent = true
			v.lab = kdcBindingLab()
			v.hosts = map[string]string{"SRV03": "i-srv03", "DC03": "i-dc03"}

			v.checkESC6KDCBinding(context.Background(), io.Discard, "srv03", "BRAAVOS")

			if len(v.report.Results) != 1 {
				t.Fatalf("expected exactly 1 result, got %d", len(v.report.Results))
			}
			got := v.report.Results[0]
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (message: %q)", got.Status, tt.wantStatus, got.Name)
			}
			if tt.wantDetail != "" && !strings.Contains(got.Name, tt.wantDetail) {
				t.Errorf("message %q does not mention %q", got.Name, tt.wantDetail)
			}
		})
	}
}

// The verdict must name the DC it read, not the CA. Attributing an enforcement
// value to the CA host is how the cross-forest split stayed invisible: every
// CA-side probe was green while the deciding KDC was never consulted.
func TestCheckESC6KDCBinding_ReportsDCNotCA(t *testing.T) {
	v, _ := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: kdcEnvelope(`{"present":true,"value":1,"error":""}`)}, nil
	})
	v.silent = true
	v.lab = kdcBindingLab()
	v.hosts = map[string]string{"SRV03": "i-srv03", "DC03": "i-dc03"}

	v.checkESC6KDCBinding(context.Background(), io.Discard, "srv03", "BRAAVOS")

	msg := v.report.Results[0].Name
	if !strings.Contains(msg, "MEEREEN") {
		t.Errorf("message must name the validating DC MEEREEN, got %q", msg)
	}
	if !strings.Contains(msg, "BRAAVOS") {
		t.Errorf("message must still name the CA BRAAVOS, got %q", msg)
	}
}

// The registry read must target the KDC service key. A typo here silently turns
// every lab into the "absent, so unknown" branch, which looks like a cautious
// WARN rather than a broken probe.
func TestCheckESC6KDCBinding_ReadsKDCKeyOnDC(t *testing.T) {
	var gotScript string
	v, _ := newStubValidator(t, func(_ int, command string) (*provider.CommandResult, error) {
		gotScript = command
		return &provider.CommandResult{Status: "Success", Stdout: kdcEnvelope(`{"present":true,"value":0,"error":""}`)}, nil
	})
	v.silent = true
	v.lab = kdcBindingLab()
	v.hosts = map[string]string{"SRV03": "i-srv03", "DC03": "i-dc03"}

	v.checkESC6KDCBinding(context.Background(), io.Discard, "srv03", "BRAAVOS")

	if got := v.report.Results[0].Status; got != "PASS" {
		t.Fatalf("setup: expected PASS, got %q", got)
	}
	if !strings.Contains(gotScript, `Services\Kdc`) {
		t.Errorf("probe must read the KDC service key, got script: %q", gotScript)
	}
	if !strings.Contains(gotScript, "StrongCertificateBindingEnforcement") {
		t.Errorf("probe must read StrongCertificateBindingEnforcement, got script: %q", gotScript)
	}
}

// An unresolvable DC is unknown, not exploitable.
func TestCheckESC6KDCBinding_NoDCResolvedWarns(t *testing.T) {
	v, _ := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
		return &provider.CommandResult{Status: "Success", Stdout: kdcEnvelope(`{"present":true,"value":0,"error":""}`)}, nil
	})
	v.silent = true
	v.lab = &labmap.LabMap{
		Hosts:         map[string]labmap.HostInfo{"srv03": {NewHostname: "braavos"}},
		HostConfigs:   map[string]labmap.HostConfig{"srv03": {Hostname: "braavos"}},
		DomainConfigs: map[string]labmap.DomainConfig{},
	}
	v.hosts = map[string]string{"SRV03": "i-srv03"}

	v.checkESC6KDCBinding(context.Background(), io.Discard, "srv03", "BRAAVOS")

	if len(v.report.Results) != 1 {
		t.Fatalf("expected exactly 1 result, got %d", len(v.report.Results))
	}
	if got := v.report.Results[0].Status; got != "WARN" {
		t.Errorf("status = %q, want WARN", got)
	}
}
