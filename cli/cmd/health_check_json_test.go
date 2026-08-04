package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHealthReportJSON(t *testing.T) {
	report := healthReport{
		Passed:  1,
		Failed:  1,
		Skipped: 1,
		Checks: []healthCheckResult{
			{Name: "DC01 AD Domain Controller", Host: "DC01", Status: "OK", Detail: "DC01"},
			{Name: "DC02 AD Replication", Host: "DC02", Status: "FAIL", Detail: "replication errors detected"},
			{Name: "SRV01 MSSQL", Host: "SRV01", Status: "SKIP", Detail: "instance not found"},
		},
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded healthReport
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded.Passed != 1 || decoded.Failed != 1 || decoded.Skipped != 1 {
		t.Fatalf("counts wrong: %+v", decoded)
	}
	if len(decoded.Checks) != 3 {
		t.Fatalf("want 3 checks, got %d", len(decoded.Checks))
	}
	if decoded.Checks[0].Host != "DC01" || decoded.Checks[0].Status != "OK" {
		t.Fatalf("check[0] mapping wrong: %+v", decoded.Checks[0])
	}
	if decoded.Checks[1].Status != "FAIL" {
		t.Fatalf("check[1] should be FAIL: %+v", decoded.Checks[1])
	}
	// field names must match what the web app hook parses
	for _, key := range []string{`"name"`, `"host"`, `"status"`, `"detail"`, `"passed"`, `"failed"`, `"skipped"`, `"checks"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("JSON missing key %s:\n%s", key, b)
		}
	}
}

func TestHealthReportEmptyChecksIsArray(t *testing.T) {
	// The command builds results with make([]..., 0, n); an all-clean/no-check
	// run must serialize checks as [] (not null) so the hook can range over it.
	report := healthReport{Checks: make([]healthCheckResult, 0)}
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(b), `"checks":[]`) {
		t.Fatalf("empty checks must render as [], got %s", b)
	}
}
