package validate

import (
	"strings"
	"testing"
	"time"
)

func sampleReport() *Report {
	rs := []Result{
		{Status: "PASS", Category: "Discovery", Name: "Found DC01"},
		{Status: "PASS", Category: "Discovery", Name: "Found SRV01"},
		{Status: "PASS", Category: "Credentials", Name: "no plaintext creds in sysvol"},
		{Status: "FAIL", Category: "Credentials", Name: "autologon registry"},
		{Status: "PASS", Category: "Kerberos", Name: "kerberoastable spn"},
		{Status: "WARN", Category: "Kerberos", Name: "asrep roastable account"},
		{Status: "PASS", Category: "MSSQL", Name: "linked server"},
		{Status: "PASS", Category: "MSSQL", Name: "xp_cmdshell"},
		{Status: "FAIL", Category: "MSSQL", Name: "sysadmin role"},
		{Status: "PASS", Category: "ADCS", Name: "vulnerable template enabled"},
		{Status: "FAIL", Category: "ADCS-ESC1", Name: "ESC1 template"},
		{Status: "WARN", Category: "ADCS-ESC6", Name: "EDITF flag"},
		{Status: "PASS", Category: "ACL", Name: "WriteOwner on Domain Admins"},
		{Status: "PASS", Category: "Trusts", Name: "forest trust exists"},
		{Status: "PASS", Category: "Services", Name: "unquoted service path"},
		{Status: "WARN", Category: "LLMNR", Name: "LLMNR enabled"},
		{Status: "PASS", Category: "Shares", Name: "open share"},
		{Status: "PASS", Category: "GPO", Name: "GPO abuse path"},
		{Status: "PASS", Category: "gMSA", Name: "gMSA readable"},
		{Status: "PASS", Category: "LAPS", Name: "LAPS readable by group"},
	}
	r := &Report{Results: rs}
	for _, res := range rs {
		switch res.Status {
		case "PASS":
			r.Passed++
		case "FAIL":
			r.Failed++
		case "WARN":
			r.Warnings++
		}
	}
	r.Total = r.Passed + r.Failed + r.Warnings
	return r
}

func TestRenderSummaryPanel_Snapshot(t *testing.T) {
	r := sampleReport()
	out := RenderSummaryPanel(r, "prod-east", "us-east-1", 8*time.Minute+45*time.Second, 120)

	for _, want := range []string{
		"DreadGOAD VALIDATION",
		"PASSED",
		"FAILED",
		"WARNED",
		"TOTAL",
		"Credentials",
		"MSSQL",
		"prod-east",
		"us-east-1",
		"Success rate:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered panel missing %q\n%s", want, out)
		}
	}
	if t.Failed() || testing.Verbose() {
		t.Logf("\n%s", out)
	}
}

func TestRenderSummaryPanel_Empty(t *testing.T) {
	r := &Report{}
	out := RenderSummaryPanel(r, "", "", 0, 100)
	if !strings.Contains(out, "DreadGOAD VALIDATION") {
		t.Errorf("empty panel missing title\n%s", out)
	}
	if strings.Contains(out, "Success rate:") {
		t.Errorf("empty panel should not show success rate\n%s", out)
	}
}

func TestAggregateByCategory(t *testing.T) {
	r := sampleReport()
	cats := aggregateByCategory(r)
	got := map[string]categoryStats{}
	for _, c := range cats {
		got[c.name] = c
	}
	for _, prev := range cats {
		if cats[len(cats)-1].name == "" {
			t.Fatal("found empty category name")
		}
		_ = prev
	}
	if c := got["MSSQL"]; c.pass != 2 || c.fail != 1 || c.total != 3 {
		t.Errorf("MSSQL aggregation wrong: %+v", c)
	}
	if c := got["Kerberos"]; c.pass != 1 || c.warn != 1 || c.fail != 0 {
		t.Errorf("Kerberos aggregation wrong: %+v", c)
	}
	if c := got["Discovery"]; c.pass != 2 || c.total != 2 {
		t.Errorf("Discovery aggregation wrong: %+v", c)
	}
}
