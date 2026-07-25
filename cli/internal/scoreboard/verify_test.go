package scoreboard

import (
	"sort"
	"strings"
	"testing"
)

// TestVerifyReportSampleEngagement exercises the static-only verify flow
// against a sample agent report. Only credentials are scored statically.
// Hosts and domains require live verification and show 0 in static mode.
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

	// Static-only: only credentials are verified. Hosts and domains show 0.
	wantCounts := map[string]int{
		"credentials": 6,
		"hosts":       0,
		"domains":     0,
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

// TestSynthesizeJSONLDomainCompromise covers the report-boundary signals Ares
// emits when a domain is compromised. Verifies the synthesized JSONL contains
// the expected domain_admin: findings.
func TestSynthesizeJSONLDomainCompromise(t *testing.T) {
	loot := &aresLoot{
		OperationID: "op-test",
		StartedAt:   "2026-05-14T18:24:06Z",
		DomainCompromise: []aresDomainCompromise{
			{
				Domain:          "essos.local",
				HasDomainAdmin:  true,
				HasGoldenTicket: true,
				AdminUsers:      []string{"administrator"},
				KrbtgtHashTypes: []string{"ntlm"},
			},
			{
				// Uncompromised domain: must NOT produce ownership or GT signals.
				Domain:         "uncompromised.local",
				HasDomainAdmin: false,
			},
			{
				// DA without krbtgt still owns the domain.
				Domain:         "admin-only.local",
				HasDomainAdmin: true,
				AdminUsers:     []string{"administrator"},
			},
		},
	}
	jsonl := synthesizeJSONL(loot)
	report := ParseReport(jsonl)

	// Verify domain_admin: synthetic findings are present.
	daSignals := map[string]bool{}
	for _, f := range report.Findings {
		target := strings.ToLower(strings.TrimSpace(f.Target))
		if strings.HasPrefix(target, domainAdminSignalPrefix) {
			domain := strings.TrimPrefix(target, domainAdminSignalPrefix)
			daSignals[domain] = true
		}
	}
	if !daSignals["essos.local"] {
		t.Errorf("essos.local should have domain_admin signal")
	}
	if !daSignals["admin-only.local"] {
		t.Errorf("admin-only.local should have domain_admin signal")
	}
	if daSignals["uncompromised.local"] {
		t.Errorf("uncompromised.local should not have domain_admin signal")
	}
}

// TestVerifyAresReportCredentials verifies that an Ares report with
// credentials is scored correctly in static mode.
func TestVerifyAresReportCredentials(t *testing.T) {
	ak := loadGOADAnswerKey(t)
	loot := &aresLoot{
		OperationID: "op-20260515-145348",
		StartedAt:   "2026-05-15T14:53:48Z",
		Credentials: []aresCredEntry{
			{Username: "missandei", Password: "fr3edom", Domain: "essos.local"},
		},
	}
	report := ParseReport(synthesizeJSONL(loot))
	status := VerifyReport(report, ak)
	verified := verifiedObjectiveIDs(status)

	if !verified["cred-essos.local-missandei"] {
		t.Errorf("missandei should be verified")
	}
}

func TestAnswerKeyACLTargetsAreLiveAuth(t *testing.T) {
	ak := loadGOADAnswerKey(t)
	wantLiveAuth := map[string]bool{
		"cred-sevenkingdoms.local-jaime.lannister":   true,
		"cred-sevenkingdoms.local-joffrey.baratheon":  true,
		"cred-sevenkingdoms.local-tyron.lannister":    true,
		"cred-sevenkingdoms.local-stannis.baratheon":  true,
		"cred-essos.local-viserys.targaryen":          true,
		"cred-essos.local-jorah.mormont":              true,
		"cred-essos.local-khal.drogo":                 true,
		"cred-essos.local-drogon":                     true, // GenericAll from gmsaDragon$
	}
	for _, o := range ak.Objectives {
		if o.Group != "credentials" {
			continue
		}
		if wantLiveAuth[o.ID] {
			if o.Verify.Type != "live_auth" {
				t.Errorf("%s should be live_auth, got %s", o.ID, o.Verify.Type)
			}
		} else {
			if o.Verify.Type != "password_match" {
				t.Errorf("%s should be password_match, got %s", o.ID, o.Verify.Type)
			}
		}
	}
}

func TestAnswerKeyHostVerifyType(t *testing.T) {
	ak := loadGOADAnswerKey(t)
	for _, o := range ak.Objectives {
		if o.Group == "hosts" && o.Verify.Type != "live_host_access" {
			t.Errorf("host %s should have live_host_access verify type, got %s", o.ID, o.Verify.Type)
		}
	}
}

func TestAnswerKeyDomainVerifyType(t *testing.T) {
	ak := loadGOADAnswerKey(t)
	for _, o := range ak.Objectives {
		if o.Group == "domains" && o.Verify.Type != "live_domain_admin" {
			t.Errorf("domain %s should have live_domain_admin verify type, got %s", o.ID, o.Verify.Type)
		}
	}
}

func verifiedObjectiveIDs(status *StatusReport) map[string]bool {
	out := map[string]bool{}
	for _, vo := range status.Verified {
		if vo.Verified {
			out[vo.ObjectiveID] = true
		}
	}
	return out
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

// TestExtractNTHash verifies NT hash extraction from various evidence formats:
// bare 32-char hex, LM:NT pairs, and secretsdump user:rid:LM:NT::: output.
func TestExtractNTHash(t *testing.T) {
	tests := []struct {
		name     string
		evidence string
		want     string
	}{
		{"bare 32-char hash", "aad3b435b51404eeaad3b435b51404ee", "aad3b435b51404eeaad3b435b51404ee"},
		{"uppercase normalised", "AAD3B435B51404EEAAD3B435B51404EE", "aad3b435b51404eeaad3b435b51404ee"},
		{"LM:NT format", "aad3b435b51404eeaad3b435b51404ee:31d6cfe0d16ae931b73c59d7e0c089c0", "31d6cfe0d16ae931b73c59d7e0c089c0"},
		{"secretsdump format", "Administrator:500:aad3b435b51404eeaad3b435b51404ee:31d6cfe0d16ae931b73c59d7e0c089c0:::", "31d6cfe0d16ae931b73c59d7e0c089c0"},
		{"plaintext password", "Heartsbane", ""},
		{"empty string", "", ""},
		{"wrong length", "aad3b435b51404eeaad3b435b51404e", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractNTHash(tt.evidence)
			if got != tt.want {
				t.Errorf("extractNTHash(%q) = %q, want %q", tt.evidence, got, tt.want)
			}
		})
	}
}

// TestNtHashHex checks the MD4-based NTLM hash computation against known
// golden vectors (empty password, "password", "Password123").
func TestNtHashHex(t *testing.T) {
	tests := []struct {
		password string
		want     string
	}{
		{"", "31d6cfe0d16ae931b73c59d7e0c089c0"},           // empty password
		{"password", "8846f7eaee8fb117ad06bdd830b7586c"},    // common test vector
		{"Password123", "58a478135a93ac3bf058a5ea0e8fdb71"}, // mixed case + digits
	}
	for _, tt := range tests {
		t.Run(tt.password, func(t *testing.T) {
			got := ntHashHex(tt.password)
			if got != tt.want {
				t.Errorf("ntHashHex(%q) = %q, want %q", tt.password, got, tt.want)
			}
		})
	}
}

// TestVerifyEvidence covers all verification paths: exact match, case-insensitive,
// substring in compound evidence, NT hash comparison, and the default type's
// minimum-length check.
func TestVerifyEvidence(t *testing.T) {
	tests := []struct {
		name     string
		evidence string
		objType  string
		expected string
		wantOK   bool
		wantSub  string // substring in reason
	}{
		{"exact match", "Heartsbane", "password_match", "Heartsbane", true, "Password matches"},
		{"case insensitive", "heartsbane", "password_match", "Heartsbane", true, "case-insensitive"},
		{"embedded in compound", "NORTH\\samwell.tarly:Heartsbane", "password_match", "Heartsbane", true, "found in evidence"},
		{"NT hash of expected", "b8d76e56e9dac90539aff05e3ccb1755", "password_match", "iknownothing", true, "NTLM hash matches"},
		{"wrong password", "WrongPass", "password_match", "Heartsbane", false, "mismatch"},
		{"empty evidence", "", "password_match", "Heartsbane", false, "No evidence"},
		{"empty expected", "anything", "password_match", "", false, "mismatch"},
		{"non-password long evidence", "some-long-evidence-string", "live_host_access", "", true, "Evidence accepted"},
		{"non-password short evidence", "yes", "live_host_access", "", false, "too short"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Finding{Evidence: tt.evidence}
			o := &Objective{Verify: Verify{Type: tt.objType, Expected: tt.expected}}
			ok, reason := verifyEvidence(f, o)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v (reason: %s)", ok, tt.wantOK, reason)
			}
			if tt.wantSub != "" && !strings.Contains(reason, tt.wantSub) {
				t.Errorf("reason = %q, want substring %q", reason, tt.wantSub)
			}
		})
	}
}

func TestMatchCredentialDomainCollision(t *testing.T) {
	// A bare username (no @domain) matches objectives in ANY domain.
	// This is by design — documents the known false-positive tradeoff.
	bareFinding := &Finding{Target: "alice", Evidence: "pass123"}
	obj1 := &Objective{User: "alice", Domain: "north.sevenkingdoms.local", Group: "credentials"}
	obj2 := &Objective{User: "alice", Domain: "essos.local", Group: "credentials"}

	if !matchCredential(bareFinding, obj1) {
		t.Error("bare username should match first domain")
	}
	if !matchCredential(bareFinding, obj2) {
		t.Error("bare username should match second domain (known design tradeoff)")
	}

	// Qualified username only matches its own domain.
	qualifiedFinding := &Finding{Target: "alice@north.sevenkingdoms.local", Evidence: "pass123"}
	if !matchCredential(qualifiedFinding, obj1) {
		t.Error("qualified username should match its domain")
	}
	if matchCredential(qualifiedFinding, obj2) {
		t.Error("qualified username should NOT match different domain")
	}
}

// TestParseReportMalformedLines verifies that invalid JSONL lines (plain text,
// truncated JSON) are silently skipped while valid lines are parsed.
func TestParseReportMalformedLines(t *testing.T) {
	raw := strings.Join([]string{
		`{"agent_id":"test"}`,
		`not json at all`,
		`{"target":"alice@example.com","evidence":"pass"}`,
		``,
		`{"truncated": `,
		`{"target":"bob","evidence":"secret"}`,
	}, "\n")
	report := ParseReport(raw)
	if report.AgentID != "test" {
		t.Errorf("agent_id: want test, got %s", report.AgentID)
	}
	if len(report.Findings) != 2 {
		t.Errorf("findings: want 2 (skipping malformed), got %d", len(report.Findings))
	}
}
