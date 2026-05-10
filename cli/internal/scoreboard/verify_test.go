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

// loadGOADAnswerKey is shared by the ground-truth subtests below.
func loadGOADAnswerKey(t *testing.T) *AnswerKey {
	t.Helper()
	ak, err := GenerateAnswerKey("../../../ad/GOAD/data/config.json")
	if err != nil {
		t.Fatal(err)
	}
	return ak
}

func TestAnswerKeyHasAllExpectedTechniques(t *testing.T) {
	ak := loadGOADAnswerKey(t)
	techIDs := map[string]bool{}
	for _, o := range ak.Objectives {
		if o.Group == "techniques" {
			techIDs[o.Technique] = true
		}
	}
	want := []string{
		"asrep_roast", "kerberoast",
		"adcs_esc1", "adcs_esc2", "adcs_esc3", "adcs_esc4", "adcs_esc6",
		"adcs_esc7", "adcs_esc9", "adcs_esc11", "adcs_esc13", "adcs_esc15",
		"adcs_esc10_case1", "adcs_esc10_case2",
		"golden_ticket-essos.local",
		"golden_ticket-north.sevenkingdoms.local",
		"golden_ticket-sevenkingdoms.local",
		"gmsa_password_read", "gpo_abuse", "laps_password_read",
		"sid_history_abuse", "rbcd", "shadow_credentials",
		"mssql_exploit", "mssql_linked_server",
		"llmnr_nbtns_poisoning", "ntlm_relay", "ntlmv1_downgrade",
		"acl_abuse", "cross_forest_trust", "child_to_parent",
		"constrained_delegation", "unconstrained_delegation",
		"seimpersonate",
	}
	for _, w := range want {
		if !techIDs[w] {
			t.Errorf("missing technique objective: %s", w)
		}
	}
}

func TestAnswerKeyHostAdminsAreAccurate(t *testing.T) {
	ak := loadGOADAnswerKey(t)
	hostAdmins := map[string][]string{}
	for _, o := range ak.Objectives {
		if o.Group == "hosts" {
			hostAdmins[o.Hostname] = o.AdminUsers
		}
	}
	// MSSQL EXECUTE AS LOGIN chains land in admin lists.
	for _, w := range []string{"samwell.tarly", "brandon.stark", "jon.snow", "jeor.mormont"} {
		if !containsString(hostAdmins["castelblack"], w) {
			t.Errorf("castelblack admins missing %s; got %v", w, hostAdmins["castelblack"])
		}
	}
	for _, w := range []string{"jorah.mormont", "khal.drogo"} {
		if !containsString(hostAdmins["braavos"], w) {
			t.Errorf("braavos admins missing %s; got %v", w, hostAdmins["braavos"])
		}
	}
	// Empty-group placeholders (DragonRider, greatmaster) MUST NOT appear as
	// admin "users" — they expand to zero members.
	for _, h := range []string{"kingslanding", "meereen"} {
		for _, bad := range []string{"dragonrider", "greatmaster"} {
			if containsString(hostAdmins[h], bad) {
				t.Errorf("%s admins contains group placeholder %q (must be expanded, not literal)", h, bad)
			}
		}
	}
}

func TestAnswerKeyAsrepCredentialsHaveHint(t *testing.T) {
	ak := loadGOADAnswerKey(t)
	for _, o := range ak.Objectives {
		if o.Group != "credentials" {
			continue
		}
		isAsrep := (o.Domain == "north.sevenkingdoms.local" && o.User == "brandon.stark") ||
			(o.Domain == "essos.local" && o.User == "missandei")
		if isAsrep && !strings.Contains(o.Hint, "AS-REP roastable") {
			t.Errorf("%s should have AS-REP roastable hint, got %q", o.ID, o.Hint)
		}
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
