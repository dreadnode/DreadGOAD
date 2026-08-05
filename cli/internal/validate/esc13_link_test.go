package validate

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

// esc13.ps1 sets the template's msPKI-Certificate-Policy before it links the
// issuance policy OID to a group, so a run that dies in between leaves the lab
// looking configured while ESC13 is not actually exploitable. The group-link
// probe is what distinguishes those states.
func TestCheckESC13GroupLink(t *testing.T) {
	tests := []struct {
		name       string
		stdout     string
		wantStatus string
		wantDetail string
	}{
		{
			name:       "linked single OID passes",
			stdout:     "COUNT=1 LINKED=1 GROUP=CN=greatmaster,OU=Groups,DC=essos,DC=local\n",
			wantStatus: "PASS",
		},
		{
			name:       "OID present but unlinked fails",
			stdout:     "COUNT=1 LINKED=0 GROUP=\n",
			wantStatus: "FAIL",
			wantDetail: "no msDS-OIDToGroupLink",
		},
		{
			name:       "no OID object at all fails",
			stdout:     "NO_OID\n",
			wantStatus: "FAIL",
			wantDetail: "has not run",
		},
		{
			name:       "duplicate OID objects warn",
			stdout:     "COUNT=2 LINKED=1 GROUP=CN=greatmaster,OU=Groups,DC=essos,DC=local\n",
			wantStatus: "WARN",
			wantDetail: "Duplicate",
		},
		{
			// One stale OID accrues per re-provision, so a long-lived lab reaches
			// double digits. "COUNT=10" contains "COUNT=1", which a substring test
			// would read as the healthy single-object case.
			name:       "ten duplicate OID objects still warn",
			stdout:     "COUNT=10 LINKED=1 GROUP=CN=greatmaster,OU=Groups,DC=essos,DC=local\n",
			wantStatus: "WARN",
			wantDetail: "Duplicate",
		},
		{
			name:       "unparsable probe output warns rather than passing",
			stdout:     "Get-ADObject : A referral was returned from the server\n",
			wantStatus: "WARN",
			wantDetail: "Unreadable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, _ := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
				return &provider.CommandResult{Status: "Success", Stdout: tt.stdout}, nil
			})
			v.silent = true
			v.hosts["DC03"] = "i-dc03"

			v.checkESC13GroupLink(context.Background(), io.Discard, "DC03")

			if len(v.report.Results) != 1 {
				t.Fatalf("expected exactly 1 result, got %d", len(v.report.Results))
			}
			got := v.report.Results[0]
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (detail: %q)", got.Status, tt.wantStatus, got.Detail)
			}
			if tt.wantDetail != "" && !strings.Contains(got.Name, tt.wantDetail) {
				t.Errorf("message %q does not mention %q", got.Name, tt.wantDetail)
			}
		})
	}
}

// A transport failure must not be reported as a missing link: an unreachable DC
// is unknown, not broken. The context is canceled up front so the shared
// transport retry (a hardcoded 2s backoff) short-circuits instead of sleeping.
func TestCheckESC13GroupLink_TransportErrorWarns(t *testing.T) {
	v, _ := newStubValidator(t, func(_ int, _ string) (*provider.CommandResult, error) {
		return nil, context.DeadlineExceeded
	})
	v.silent = true
	v.hosts["DC03"] = "i-dc03"

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v.checkESC13GroupLink(ctx, io.Discard, "DC03")

	if len(v.report.Results) != 1 {
		t.Fatalf("expected exactly 1 result, got %d", len(v.report.Results))
	}
	if got := v.report.Results[0].Status; got != "WARN" {
		t.Errorf("status = %q, want WARN", got)
	}
}

// The GROUP= field is a DN carrying both spaces and "=", so field-splitting the
// probe line must not let DN components masquerade as the integer keys.
func TestParseESC13GroupLink(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantCount  int
		wantLinked int
		wantOK     bool
	}{
		{"single linked", "COUNT=1 LINKED=1 GROUP=CN=greatmaster,DC=essos,DC=local", 1, 1, true},
		{"double digit count", "COUNT=10 LINKED=1 GROUP=CN=greatmaster,DC=essos,DC=local", 10, 1, true},
		{"unlinked empty group", "COUNT=1 LINKED=0 GROUP=", 1, 0, true},
		{"DN with spaces does not shadow keys", "COUNT=2 LINKED=2 GROUP=CN=Domain Admins,CN=Users,DC=essos,DC=local", 2, 2, true},
		{"missing keys", "NO_OID", 0, 0, false},
		{"non-numeric values", "COUNT=many LINKED=some", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, linked, ok := parseESC13GroupLink(tt.output)
			if ok != tt.wantOK || count != tt.wantCount || linked != tt.wantLinked {
				t.Errorf("parseESC13GroupLink(%q) = (%d, %d, %v), want (%d, %d, %v)",
					tt.output, count, linked, ok, tt.wantCount, tt.wantLinked, tt.wantOK)
			}
		})
	}
}

// The probe must render to valid PowerShell with the OID DisplayName quoted.
func TestESC13GroupLinkProbe_Renders(t *testing.T) {
	got, err := renderScript(`-Filter {DisplayName -eq {{psq .Name}}}`, map[string]any{"Name": esc13IssuanceName})
	if err != nil {
		t.Fatalf("renderScript: %v", err)
	}
	want := `-Filter {DisplayName -eq 'IssuancePolicyESC13'}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
