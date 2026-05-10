// Package scoreboard implements the DreadGOAD live status board: it parses
// a GOAD lab config into a checklist of objectives ("answer key"), polls an
// agent's JSONL report from local disk or a remote EC2 instance via SSM, and
// renders verification progress as a live TUI.
package scoreboard

// Verify describes how an objective is checked against agent evidence.
type Verify struct {
	Type     string `json:"type"`
	Expected string `json:"expected,omitempty"`
}

// Objective is a single milestone in the answer key (a credential to find,
// a host to compromise, a domain to own, or a technique to use).
type Objective struct {
	ID         string   `json:"id"`
	Group      string   `json:"group"`
	User       string   `json:"user,omitempty"`
	Domain     string   `json:"domain,omitempty"`
	Role       string   `json:"role,omitempty"`
	Hint       string   `json:"hint,omitempty"`
	Label      string   `json:"label"`
	Hostname   string   `json:"hostname,omitempty"`
	HostType   string   `json:"type,omitempty"`
	Services   []string `json:"services,omitempty"`
	AdminUsers []string `json:"admin_users,omitempty"`
	DAUsers    []string `json:"da_users,omitempty"`
	Technique  string   `json:"technique,omitempty"`
	Category   string   `json:"category,omitempty"`
	Verify     Verify   `json:"verify"`
}

// AnswerKey is the full set of objectives derived from a GOAD config.
type AnswerKey struct {
	Version         string         `json:"version"`
	Lab             string         `json:"lab"`
	TotalObjectives int            `json:"total_objectives"`
	Groups          map[string]int `json:"groups"`
	Objectives      []Objective    `json:"objectives"`
}

// Finding is a single line the agent appends to the JSONL report.
type Finding struct {
	Target      string `json:"target,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
	Description string `json:"description,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
}

// Report is the agent's full report (header + findings).
type Report struct {
	AgentID   string    `json:"agent_id,omitempty"`
	StartTime string    `json:"start_time,omitempty"`
	Findings  []Finding `json:"findings"`
}

// VerifiedObjective is a single matched/verified entry produced during verification.
type VerifiedObjective struct {
	ObjectiveID   string
	Group         string
	Label         string
	Verified      bool
	Timestamp     string
	AgentEvidence string
	Technique     string
	Reason        string
}

// GroupStats tracks achieved/total for one milestone group.
type GroupStats struct {
	Achieved int
	Total    int
}

// StatusReport is the verified state derived from a report against an answer key.
type StatusReport struct {
	Verified          []VerifiedObjective
	UnmatchedFindings []Finding
	Groups            map[string]*GroupStats
}
