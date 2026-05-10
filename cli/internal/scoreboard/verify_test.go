package scoreboard

import (
	"sort"
	"strings"
	"testing"
)

// TestVerifyReportSampleEngagement exercises the full verify flow against a
// sample agent report. The expected counts and inferred objectives are the
// same set the reference Python implementation produces for the in-tree
// answer key.
func TestVerifyReportSampleEngagement(t *testing.T) {
	ak, err := GenerateAnswerKey("../../../ad/GOAD/data/config.json")
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.Join([]string{
		`{"agent_id":"test-agent","start_time":"2026-05-09T10:00:00Z"}`,
		`{"target":"samwell.tarly@north.sevenkingdoms.local","evidence":"Heartsbane"}`,
		`{"target":"hodor@north.sevenkingdoms.local","evidence":"hodor"}`,
		`{"target":"brandon.stark@north.sevenkingdoms.local","evidence":"iseedeadpeople"}`,
		`{"target":"jon.snow@north.sevenkingdoms.local","evidence":"iknownothing"}`,
		`{"target":"eddard.stark@north.sevenkingdoms.local","evidence":"FightP3aceAndHonor!"}`,
		`{"target":"daenerys.targaryen@essos.local","evidence":"BurnThemAll!"}`,
		`{"target":"sevenkingdoms.local","evidence":"forged golden ticket extrasid"}`,
	}, "\n")
	report := ParseReport(raw)
	if got := len(report.Findings); got != 7 {
		t.Fatalf("findings: want 7, got %d", got)
	}
	if report.AgentID != "test-agent" {
		t.Errorf("agent id: want test-agent, got %s", report.AgentID)
	}

	status := VerifyReport(report, ak)

	wantCounts := map[string]int{
		"credentials": 6,
		"hosts":       3,
		"domains":     2,
		"techniques":  4,
	}
	for g, want := range wantCounts {
		got := status.Groups[g]
		if got == nil {
			t.Errorf("group %s missing", g)
			continue
		}
		if got.Achieved != want {
			t.Errorf("group %s achieved: want %d, got %d", g, want, got.Achieved)
		}
	}

	wantVerified := []string{
		"cred-essos.local-daenerys.targaryen",
		"cred-north.sevenkingdoms.local-brandon.stark",
		"cred-north.sevenkingdoms.local-eddard.stark",
		"cred-north.sevenkingdoms.local-hodor",
		"cred-north.sevenkingdoms.local-jon.snow",
		"cred-north.sevenkingdoms.local-samwell.tarly",
		"domain-essos.local",
		"domain-north.sevenkingdoms.local",
		"host-castelblack",
		"host-meereen",
		"host-winterfell",
		"tech-asrep_roast",
		"tech-kerberoast",
		"tech-llmnr_nbtns_poisoning",
		"tech-mssql_exploit",
	}
	var gotVerified []string
	for _, vo := range status.Verified {
		if vo.Verified {
			gotVerified = append(gotVerified, vo.ObjectiveID)
		}
	}
	sort.Strings(gotVerified)
	if strings.Join(gotVerified, ",") != strings.Join(wantVerified, ",") {
		t.Errorf("verified ids:\n  want %v\n  got  %v", wantVerified, gotVerified)
	}

	if len(status.UnmatchedFindings) != 1 || status.UnmatchedFindings[0].Target != "sevenkingdoms.local" {
		t.Errorf("unmatched: want 1 finding for sevenkingdoms.local, got %+v", status.UnmatchedFindings)
	}
}

func TestParseReportStandardJSON(t *testing.T) {
	raw := `{"agent_id":"a","findings":[{"target":"x","evidence":"y"}]}`
	r := ParseReport(raw)
	if r.AgentID != "a" || len(r.Findings) != 1 || r.Findings[0].Target != "x" {
		t.Errorf("unexpected parse: %+v", r)
	}
}

func TestExtractUsernameFormats(t *testing.T) {
	cases := map[string]string{
		"alice@example.com": "alice",
		"DOMAIN\\bob":       "bob",
		"CN=carol,OU=users": "carol",
		"dave":              "dave",
	}
	for in, want := range cases {
		if got := extractUsername(in); got != want {
			t.Errorf("extractUsername(%q) = %q, want %q", in, got, want)
		}
	}
}
