package validate

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/labmap"
	"github.com/dreadnode/dreadgoad/internal/provider"
)

// esc9TemplateFlag is the msPKI-Enrollment-Flag the shipped ESC9.json carries:
// 0x80029, so CT_FLAG_NO_SECURITY_EXTENSION (0x80000) plus the publish and
// auto-enrollment bits.
const esc9TemplateFlag = "524329"

// esc9Stub answers both probes the check makes from one stub: the template
// attribute query (a Get-ADObject over pKICertificateTemplate) and the KDC
// registry read.
func esc9Stub(templateOut, registryJSON string) func(int, string) (*provider.CommandResult, error) {
	return func(_ int, command string) (*provider.CommandResult, error) {
		if strings.Contains(command, "pKICertificateTemplate") {
			return &provider.CommandResult{Status: "Success", Stdout: templateOut + "\n"}, nil
		}
		return &provider.CommandResult{Status: "Success", Stdout: kdcEnvelope(registryJSON)}, nil
	}
}

// ESC6 and ESC9 read the same registry value and fail at different thresholds,
// which is exactly the kind of pair a shared helper invites collapsing onto one
// condition. CT_FLAG_NO_SECURITY_EXTENSION strips the SID extension from the
// issued certificate, so Compatibility mode has nothing to validate strictly and
// falls back to the weak UPN mapping the attack spoofs: ESC9 survives at 1 where
// ESC6 dies. Only Full Enforcement, which refuses any certificate it cannot map
// strongly, closes it.
func TestCheckESC9Enforcement_KDCBinding(t *testing.T) {
	tests := []struct {
		name       string
		stdout     string
		wantStatus string
		wantDetail string
	}{
		{
			name:       "SCBE=0 disabled ignores the missing extension",
			stdout:     `{"present":true,"value":0,"error":""}`,
			wantStatus: "PASS",
			wantDetail: "ESC9 exploitable",
		},
		{
			// The case that separates ESC9 from ESC6. Passing only on 0 here
			// would report a live route as dead.
			name:       "SCBE=1 compatibility still permits the weak mapping",
			stdout:     `{"present":true,"value":1,"error":""}`,
			wantStatus: "PASS",
			wantDetail: "ESC9 exploitable",
		},
		{
			// Measured on staging: enrolled as an ordinary domain user with
			// no UPN spoof and no -sid, certificate issued with no object
			// SID, AS-REQ reached the KDC and no TGT came back.
			name:       "SCBE=2 full enforcement refuses a certificate with no SID",
			stdout:     `{"present":true,"value":2,"error":""}`,
			wantStatus: "FAIL",
			wantDetail: "NOT exploitable",
		},
		{
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
			v, _ := newStubValidator(t, esc9Stub(esc9TemplateFlag, tt.stdout))
			v.silent = true
			v.lab = kdcBindingLab()
			v.hosts = map[string]string{"SRV03": "i-srv03", "DC03": "i-dc03"}

			v.checkESC9Enforcement(context.Background(), io.Discard)

			if len(v.report.Results) != 1 {
				t.Fatalf("expected exactly 1 result, got %d", len(v.report.Results))
			}
			got := v.report.Results[0]
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (message: %q)", got.Status, tt.wantStatus, got.Name)
			}
			if !strings.Contains(got.Name, tt.wantDetail) {
				t.Errorf("message %q does not mention %q", got.Name, tt.wantDetail)
			}
		})
	}
}

// GOAD-Light, GOAD-Mini and NHA install a CA but publish no ESC9 template. A
// KDC verdict there would fail a lab for a route it never shipped, so the
// template gates the enforcement read.
func TestCheckESC9Enforcement_NoTemplateSkipsKDCVerdict(t *testing.T) {
	v, _ := newStubValidator(t, esc9Stub("TEMPLATE_NOT_FOUND", `{"present":false,"value":0,"error":""}`))
	v.silent = true
	v.lab = kdcBindingLab()
	v.hosts = map[string]string{"SRV03": "i-srv03", "DC03": "i-dc03"}

	v.checkESC9Enforcement(context.Background(), io.Discard)

	if len(v.report.Results) != 1 {
		t.Fatalf("expected exactly 1 result, got %d", len(v.report.Results))
	}
	got := v.report.Results[0]
	if got.Status != "INFO" {
		t.Errorf("status = %q, want INFO (message: %q)", got.Status, got.Name)
	}
	if strings.Contains(got.Name, "exploitable") {
		t.Errorf("a lab with no ESC9 template must not get an exploitability verdict, got %q", got.Name)
	}
}

// The template being present is not the same as the template being an ESC9
// template. Without CT_FLAG_NO_SECURITY_EXTENSION the certificate carries a SID
// like any other and the KDC binding is beside the point.
func TestCheckESC9Enforcement_TemplateMissingFlag(t *testing.T) {
	v, _ := newStubValidator(t, esc9Stub("41", `{"present":true,"value":0,"error":""}`))
	v.silent = true
	v.lab = kdcBindingLab()
	v.hosts = map[string]string{"SRV03": "i-srv03", "DC03": "i-dc03"}

	v.checkESC9Enforcement(context.Background(), io.Discard)

	if len(v.report.Results) != 1 {
		t.Fatalf("expected exactly 1 result, got %d", len(v.report.Results))
	}
	got := v.report.Results[0]
	if got.Status != "FAIL" {
		t.Errorf("status = %q, want FAIL (message: %q)", got.Status, got.Name)
	}
	if !strings.Contains(got.Name, "CT_FLAG_NO_SECURITY_EXTENSION") {
		t.Errorf("message %q does not name the missing flag", got.Name)
	}
}

// The DC that decides ESC9 is the one for the domain the certificate is issued
// in. GOAD pins StrongCertificateBindingEnforcement=0 on kingslanding, in a
// forest with no CA and no vulnerable templates, so a check that finds any
// permissive KDC in the lab reports a route that cannot be walked.
func TestCheckESC9Enforcement_ReadsCertificateDomainDC(t *testing.T) {
	v, _ := newStubValidator(t, esc9Stub(esc9TemplateFlag, `{"present":true,"value":2,"error":""}`))
	v.silent = true
	v.lab = &labmap.LabMap{
		Hosts: map[string]labmap.HostInfo{
			"dc01":  {NewHostname: "kingslanding", NewDomain: "sevenkingdoms.local"},
			"dc03":  {NewHostname: "meereen", NewDomain: "essos.local"},
			"srv03": {NewHostname: "braavos", NewDomain: "essos.local"},
		},
		HostConfigs: map[string]labmap.HostConfig{
			"dc01":  {Hostname: "kingslanding", Type: "dc", Domain: "sevenkingdoms.local", Vulns: []string{"adcs_esc10_case1"}},
			"dc03":  {Hostname: "meereen", Type: "dc", Domain: "essos.local"},
			"srv03": {Hostname: "braavos", Type: "server", Domain: "essos.local"},
		},
		DomainConfigs: map[string]labmap.DomainConfig{
			"essos.local":         {DC: "dc03", CAServer: "braavos"},
			"sevenkingdoms.local": {DC: "dc01"},
		},
	}
	v.hosts = map[string]string{"DC01": "i-dc01", "DC03": "i-dc03", "SRV03": "i-srv03"}

	v.checkESC9Enforcement(context.Background(), io.Discard)

	if len(v.report.Results) != 1 {
		t.Fatalf("expected exactly 1 result, got %d", len(v.report.Results))
	}
	msg := v.report.Results[0].Name
	if !strings.Contains(msg, "MEEREEN") {
		t.Errorf("verdict must name the essos KDC MEEREEN, got %q", msg)
	}
	if strings.Contains(msg, "KINGSLANDING") {
		t.Errorf("verdict must not be drawn from the permissive KDC in another forest, got %q", msg)
	}
}
