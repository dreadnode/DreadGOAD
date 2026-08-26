package cmd

import (
	"testing"

	"github.com/dreadnode/dreadgoad/internal/provider"
)

func TestSummarizeSecurityResults(t *testing.T) {
	results := []provider.SecurityCheckResult{
		{Status: "OK"},
		{Status: "OK"},
		{Status: "FAIL"},
		{Status: "WARN"},
		{Status: "SKIP"},
	}

	report := summarizeSecurityResults(results)
	if report.Passed != 2 || report.Failed != 1 || report.Warned != 1 || report.Skipped != 1 {
		t.Fatalf("unexpected counts: %+v", report)
	}
	if len(report.Checks) != len(results) {
		t.Fatalf("checks = %d, want %d", len(report.Checks), len(results))
	}
}
