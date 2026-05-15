package scoreboard

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	awsclient "github.com/dreadnode/dreadgoad/internal/aws"

	ssmtypes "github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

// AresTransport sources findings from a running ares operation by invoking
// `ares ops loot --latest --json` on the target instance via SSM, then
// translating the structured loot snapshot into synthetic JSONL findings the
// existing parser understands.
type AresTransport struct {
	InstanceID string
	Region     string
	BinaryPath string
	Client     *awsclient.Client
}

// NewAresTransport constructs an AresTransport. binaryPath defaults to
// /usr/local/bin/ares when empty.
func NewAresTransport(ctx context.Context, instanceID, binaryPath, region string) (*AresTransport, error) {
	if instanceID == "" {
		return nil, fmt.Errorf("instance ID is required")
	}
	c, err := awsclient.NewClient(ctx, region)
	if err != nil {
		return nil, err
	}
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/ares"
	}
	return &AresTransport{
		InstanceID: instanceID,
		Region:     region,
		BinaryPath: binaryPath,
		Client:     c,
	}, nil
}

type aresLoot struct {
	OperationID      string                 `json:"operation_id"`
	StartedAt        string                 `json:"started_at"`
	Credentials      []aresCredEntry        `json:"credentials"`
	Hashes           []aresHashEntry        `json:"hashes"`
	DomainCompromise []aresDomainCompromise `json:"domain_compromise"`
}

type aresCredEntry struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Domain   string `json:"domain"`
	IsAdmin  bool   `json:"is_admin"`
}

type aresHashEntry struct {
	Username  string `json:"username"`
	Domain    string `json:"domain"`
	HashValue string `json:"hash_value"`
	HashType  string `json:"hash_type"`
	Source    string `json:"source"`
}

// aresDomainCompromise mirrors entries in the ares loot JSON's
// `domain_compromise[]` array. Ares filters krbtgt rows out of `hashes[]` by
// design (see ares-cli `report_filter.rs`: krbtgt is "consumed internally by
// Golden Ticket detection rather than tracked as a cred objective"), so this
// metadata field is the only signal that survives the report boundary when a
// domain was compromised via krbtgt extraction without cracking a DA cleartext.
type aresDomainCompromise struct {
	Domain          string   `json:"domain"`
	HasDomainAdmin  bool     `json:"has_domain_admin"`
	HasGoldenTicket bool     `json:"has_golden_ticket"`
	KrbtgtHashTypes []string `json:"krbtgt_hash_types"`
}

// FetchReport runs `ares ops loot --latest --json` on the remote instance and,
// if successful, also fetches the `ares:op:<id>:exploited` Redis set so
// technique objectives can be credited directly. Both payloads are
// gzip+base64-encoded to sidestep SSM's 24KB stdout cap. Returns ErrNoReport
// when the operation hasn't produced any state yet.
func (t *AresTransport) FetchReport(ctx context.Context) (string, error) {
	const jqFilter = `{operation_id, started_at,` +
		` credentials: [.credentials[] | {username, password, domain, is_admin}],` +
		` hashes: [.hashes[] | {username, domain, hash_value, hash_type, source}],` +
		` domain_compromise: [.domain_compromise[] | {domain, has_domain_admin, has_golden_ticket, krbtgt_hash_types}]}`
	cmd := fmt.Sprintf("%s ops loot --latest --json | jq -c %s | gzip -c | base64 -w0",
		shellQuote(t.BinaryPath), shellQuote(jqFilter))
	out, status, stderr, err := runSSMShell(ctx, t.Client, t.InstanceID, cmd)
	if err != nil {
		return "", err
	}
	if status != ssmtypes.CommandInvocationStatusSuccess {
		if strings.Contains(stderr, "No state found") || strings.Contains(stderr, "No operations") {
			return "", ErrNoReport
		}
		return "", fmt.Errorf("ares ops loot: %s: %s", status, strings.TrimSpace(stderr))
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "", ErrNoReport
	}
	raw, err := decodeGzipBase64(out)
	if err != nil {
		return "", fmt.Errorf("decode ares loot: %w", err)
	}
	var loot aresLoot
	if err := json.Unmarshal(raw, &loot); err != nil {
		return "", fmt.Errorf("parse ares loot json: %w", err)
	}

	exploited := t.fetchExploited(ctx, loot.OperationID)
	return synthesizeJSONL(&loot, exploited), nil
}

// fetchExploited reads the `ares:op:<op>:exploited` Redis set; failures are
// non-fatal (just means no technique findings get emitted this poll).
func (t *AresTransport) fetchExploited(ctx context.Context, opID string) []string {
	if opID == "" {
		return nil
	}
	cmd := fmt.Sprintf("redis-cli SMEMBERS %s", shellQuote(fmt.Sprintf("ares:op:%s:exploited", opID)))
	out, status, _, err := runSSMShell(ctx, t.Client, t.InstanceID, cmd)
	if err != nil || status != ssmtypes.CommandInvocationStatusSuccess {
		return nil
	}
	var entries []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			entries = append(entries, line)
		}
	}
	return entries
}

func decodeGzipBase64(s string) ([]byte, error) {
	gz, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("base64: %w", err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	body, readErr := io.ReadAll(gr)
	closeErr := gr.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close gzip reader: %w", closeErr)
	}
	return body, nil
}

// DeleteReport is a no-op: ares state lives in Redis, not a file the
// scoreboard should clobber. Restarting an ares operation is a separate
// workflow (`task ec2:launch ...`).
func (t *AresTransport) DeleteReport(_ context.Context) (bool, error) {
	return false, nil
}

// aresExploitedToTechniqueIDs maps an entry from `ares:op:<id>:exploited` to
// the answer-key technique IDs it represents. Returns nil for entries that
// don't correspond to any answer-key technique. The exploited set uses prefix
// names like `mssql_linked_server_<ip>_<svc>` or bare names like
// `constrained_delegation_<user>`; we match on the prefix.
func aresExploitedToTechniqueIDs(entry string) []string {
	prefixes := []struct {
		prefix string
		ids    []string
	}{
		{"mssql_linked_server_", []string{"mssql_linked_server"}},
		{"mssql_impersonation_", []string{"mssql_exploit"}},
		{"mssql_", []string{"mssql_exploit"}},
		{"constrained_delegation_", []string{"constrained_delegation"}},
		{"unconstrained_delegation_", []string{"unconstrained_delegation"}},
		{"forest_trust_", []string{"cross_forest_trust"}},
		{"child_to_parent_", []string{"child_to_parent"}},
		{"acl_abuse_", []string{"acl_abuse"}},
		{"asrep_roast_", []string{"asrep_roast"}},
		{"kerberoast_", []string{"kerberoast"}},
		{"llmnr_", []string{"llmnr_nbtns_poisoning"}},
		{"ntlm_relay_", []string{"ntlm_relay"}},
		{"ntlmv1_", []string{"ntlmv1_downgrade"}},
		{"seimpersonate_", []string{"seimpersonate"}},
		{"adcs_esc1_", []string{"adcs_esc1"}},
		{"adcs_esc2_", []string{"adcs_esc2"}},
		{"adcs_esc3_", []string{"adcs_esc3"}}, // collapses ESC3 + ESC3-CRA
		{"adcs_esc4_", []string{"adcs_esc4"}},
		{"adcs_esc6_", []string{"adcs_esc6"}},
		{"adcs_esc7_", []string{"adcs_esc7"}},
		{"adcs_esc9_", []string{"adcs_esc9"}},
		{"adcs_esc10_case1_", []string{"adcs_esc10_case1"}},
		{"adcs_esc10_case2_", []string{"adcs_esc10_case2"}},
		{"adcs_esc11_", []string{"adcs_esc11"}},
		{"adcs_esc13_", []string{"adcs_esc13"}},
		{"adcs_esc15_", []string{"adcs_esc15"}},
		{"gpo_abuse_", []string{"gpo_abuse"}},
		{"gmsa_", []string{"gmsa_password_read"}},
		{"laps_", []string{"laps_password_read"}},
		{"sid_history_", []string{"sid_history_abuse"}},
		{"rbcd_", []string{"rbcd"}},
		{"shadow_credentials_", []string{"shadow_credentials"}},
	}
	// Per-domain golden ticket: `golden_ticket_<domain>` → `golden_ticket-<domain>`.
	// One scoreboard objective per domain because forging requires that domain's
	// krbtgt hash; a multi-domain forest can have a separate GT per domain.
	if strings.HasPrefix(entry, "golden_ticket_") {
		domain := strings.ToLower(strings.TrimPrefix(entry, "golden_ticket_"))
		if domain != "" {
			return []string{"golden_ticket-" + domain}
		}
	}
	for _, p := range prefixes {
		if strings.HasPrefix(entry, p.prefix) || entry == strings.TrimSuffix(p.prefix, "_") {
			return p.ids
		}
	}
	return nil
}

func synthesizeJSONL(l *aresLoot, exploited []string) string {
	var b strings.Builder
	writeJSONLEntry(&b, map[string]string{
		"agent_id":   "ares:" + l.OperationID,
		"start_time": l.StartedAt,
	})
	for _, c := range l.Credentials {
		writeCredentialEntry(&b, c)
	}
	for _, h := range l.Hashes {
		writeHashEntry(&b, h)
	}
	emitted := map[string]bool{}
	writeExploitedEntries(&b, exploited, emitted)
	writeDomainCompromiseEntries(&b, l.DomainCompromise, emitted)
	return b.String()
}

func writeJSONLEntry(b *strings.Builder, entry map[string]string) {
	eb, _ := json.Marshal(entry)
	b.Write(eb)
	b.WriteByte('\n')
}

func writeCredentialEntry(b *strings.Builder, c aresCredEntry) {
	if c.Username == "" || c.Password == "" {
		return
	}
	target := c.Username
	if c.Domain != "" {
		target = c.Username + "@" + c.Domain
	}
	desc := "ares loot"
	if c.IsAdmin {
		desc = "ares loot (admin)"
	}
	writeJSONLEntry(b, map[string]string{
		"target":      target,
		"evidence":    c.Password,
		"description": desc,
	})
}

func writeHashEntry(b *strings.Builder, h aresHashEntry) {
	if h.Username == "" || h.HashValue == "" {
		return
	}
	target := h.Username
	if h.Domain != "" {
		target = h.Username + "@" + strings.ToLower(h.Domain)
	}
	htype := h.HashType
	if htype == "" {
		htype = "hash"
	}
	writeJSONLEntry(b, map[string]string{
		"target":      target,
		"evidence":    h.HashValue,
		"description": "ares: " + strings.ToLower(htype) + " (" + h.Source + ")",
	})
}

func writeExploitedEntries(b *strings.Builder, exploited []string, emitted map[string]bool) {
	for _, ex := range exploited {
		for _, techID := range aresExploitedToTechniqueIDs(ex) {
			if emitted[techID] {
				continue
			}
			emitted[techID] = true
			writeJSONLEntry(b, map[string]string{
				"target":      "tech:" + techID,
				"evidence":    "ares: " + ex,
				"description": "exploited",
			})
		}
	}
}

// writeDomainCompromiseEntries synthesizes findings from domain_compromise[]
// metadata. Ares filters krbtgt rows out of hashes[] (see aresDomainCompromise
// doc), so without this step DreadGOAD's domainsFromKrbtgt never sees the
// krbtgt extraction and the "DOMAINS OWNED" panel stays empty for domains
// compromised solely via krbtgt (no cracked DA cleartext). The placeholder
// evidence is a 32-zero hex string so extractNTHash accepts it as an
// NT-hash-shaped signal; the actual hash value is intentionally not exposed
// in the loot JSON, so the evidence is symbolic.
func writeDomainCompromiseEntries(b *strings.Builder, entries []aresDomainCompromise, emitted map[string]bool) {
	const krbtgtSyntheticEvidence = "00000000000000000000000000000000"
	for _, dc := range entries {
		domain := strings.ToLower(strings.TrimSpace(dc.Domain))
		if domain == "" {
			continue
		}
		if dc.HasDomainAdmin && len(dc.KrbtgtHashTypes) > 0 {
			writeJSONLEntry(b, map[string]string{
				"target":      "krbtgt@" + domain,
				"evidence":    krbtgtSyntheticEvidence,
				"description": "ares: synthetic krbtgt from domain_compromise (" + strings.Join(dc.KrbtgtHashTypes, ",") + ")",
			})
		}
		if dc.HasGoldenTicket {
			techID := "golden_ticket-" + domain
			if !emitted[techID] {
				emitted[techID] = true
				writeJSONLEntry(b, map[string]string{
					"target":      "tech:" + techID,
					"evidence":    "ares: domain_compromise has_golden_ticket",
					"description": "exploited",
				})
			}
		}
	}
}
