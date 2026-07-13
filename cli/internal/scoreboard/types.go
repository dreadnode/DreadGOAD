// Package scoreboard implements GOAD agent scoring and the live status board.
// It parses a GOAD lab config into a checklist of objectives ("answer key"),
// scores agent reports against the key (with optional live credential
// verification via nxc/secretsdump), and renders progress as a TUI.
package scoreboard

// Verify describes how an objective is checked against agent evidence.
type Verify struct {
	Type     string `json:"type"`
	Expected string `json:"expected,omitempty"`
}

// Objective is a single milestone in the answer key (a credential to find,
// a host to compromise, or a domain to own).
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
	HostIP     string   `json:"host_ip,omitempty"`
	DCIP       string   `json:"dc_ip,omitempty"`
	Services   []string `json:"services,omitempty"`
	AdminUsers []string `json:"admin_users,omitempty"`
	DAUsers    []string `json:"da_users,omitempty"`
	NetBIOS    string   `json:"netbios,omitempty"`
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
	ObjectiveID   string `json:"objective_id"`
	Group         string `json:"group"`
	Label         string `json:"label"`
	Verified      bool   `json:"verified"`
	Timestamp     string `json:"timestamp,omitempty"`
	AgentEvidence string `json:"agent_evidence,omitempty"`
	Technique     string `json:"technique,omitempty"`
	Method        string `json:"method,omitempty"`
	Reason        string `json:"reason"`
}

// GroupStats tracks achieved/total for one milestone group.
type GroupStats struct {
	Achieved int `json:"achieved"`
	Total    int `json:"total"`
}

// StatusReport is the internal verified state used by the TUI. Not serialized
// to JSON — use ScoreResult for the CLI output format.
type StatusReport struct {
	Verified          []VerifiedObjective
	UnmatchedFindings []Finding
	Groups            map[string]*GroupStats
}

// ScoreResult is the JSON output of `dreadgoad score`.
type ScoreResult struct {
	AgentID           string                `json:"agent_id"`
	Mode              string                `json:"mode"`
	Summary           map[string]*GroupStats `json:"summary"`
	Verified          []VerifiedObjective   `json:"verified"`
	UnmatchedFindings []Finding             `json:"unmatched_findings"`
	FailedChecks      []FailedCheck         `json:"failed_checks"`
}

// FailedCheck records a live verification attempt that could not confirm an
// objective — either due to an error (timeout, SSM failure, missing IPs) or
// a clean rejection (no credential achieved admin/DCSync).
type FailedCheck struct {
	ObjectiveID string `json:"objective_id"`
	Error       string `json:"error"`
}
