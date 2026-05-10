package scoreboard

import (
	"math/rand"
	"time"
)

// DemoFindings returns a sample set of agent findings used by the demo command
// to render a representative status board without a real engagement.
func DemoFindings() []Finding {
	return []Finding{
		{Target: "samwell.tarly@north.sevenkingdoms.local", Evidence: "Heartsbane",
			Description: "Found password in AD user description field"},
		{Target: "hodor@north.sevenkingdoms.local", Evidence: "hodor",
			Description: "Username equals password"},
		{Target: "brandon.stark@north.sevenkingdoms.local", Evidence: "iseedeadpeople",
			Description: "AS-REP roasted the account (no preauth), cracked hash with hashcat"},
		{Target: "jon.snow@north.sevenkingdoms.local", Evidence: "iknownothing",
			Description: "Kerberoasted via SPN HTTP/thewall, cracked TGS-REP hash"},
		{Target: "eddard.stark@north.sevenkingdoms.local", Evidence: "FightP3aceAndHonor!",
			Description: "Obtained Domain Admin credentials for the north domain"},
		{Target: "castelblack.north.sevenkingdoms.local", Evidence: "Sup1_sa_P@ssw0rd!",
			Description: "Got MSSQL SA password, executed xp_cmdshell for shell access",
			Hostname:    "castelblack"},
		{Target: "castelblack.north.sevenkingdoms.local", Evidence: "whoami: NT AUTHORITY\\SYSTEM",
			Description: "Escalated from IIS AppPool to SYSTEM via PrintSpoofer (SeImpersonate)",
			Hostname:    "castelblack"},
		{Target: "winterfell.north.sevenkingdoms.local", Evidence: "robb.stark::NORTH:aad3b435b51404ee:NetNTLMv2 hash captured",
			Description: "Ran Responder, captured hash via LLMNR poisoning",
			Hostname:    "winterfell"},
		{Target: "sevenkingdoms.local", Evidence: "Forged golden ticket with ExtraSid for parent domain",
			Description: "Used golden ticket + ExtraSid to escalate from child to parent domain"},
		{Target: "daenerys.targaryen@essos.local", Evidence: "BurnThemAll!",
			Description: "Found Domain Admin password via secretsdump on DC"},
		{Target: "viserys.targaryen@essos.local", Evidence: "Shadow credentials set, authenticated with PKINIT",
			Description: "Abused GenericAll ACL to set shadow credentials on viserys"},
	}
}

// BuildDemoReport returns a Report with a random subset of demo findings,
// timestamped to look like a recent engagement.
func BuildDemoReport() (*Report, time.Time) {
	findings := DemoFindings()
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	count := 4 + r.Intn(len(findings)-4+1)
	if count > len(findings) {
		count = len(findings)
	}
	selected := findings[:count]
	start := time.Now().UTC().Add(-90 * time.Minute)
	for i := range selected {
		ts := start.Add(time.Duration(i*8) * time.Minute)
		selected[i].Timestamp = ts.Format(time.RFC3339)
	}
	return &Report{
		AgentID:   "dreadnode-agent",
		StartTime: start.Format(time.RFC3339),
		Findings:  selected,
	}, start
}
