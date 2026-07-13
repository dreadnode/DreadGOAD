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
func NewAresTransport(ctx context.Context, instanceID, binaryPath, region, profile string) (*AresTransport, error) {
	if instanceID == "" {
		return nil, fmt.Errorf("instance ID is required")
	}
	c, err := awsclient.NewClient(ctx, region, profile)
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
// Golden Ticket detection rather than tracked as a cred objective"), and some
// DA paths use built-in users that are not answer-key credential objectives.
// This metadata is therefore the authoritative report-boundary signal for
// domain ownership.
type aresDomainCompromise struct {
	Domain          string   `json:"domain"`
	HasDomainAdmin  bool     `json:"has_domain_admin"`
	HasGoldenTicket bool     `json:"has_golden_ticket"`
	AdminUsers      []string `json:"admin_users"`
	KrbtgtHashTypes []string `json:"krbtgt_hash_types"`
}

// FetchReport runs `ares ops loot --latest --json` on the remote instance.
// The payload is gzip+base64-encoded to sidestep SSM's 24KB stdout cap.
// Returns ErrNoReport when the operation hasn't produced any state yet.
func (t *AresTransport) FetchReport(ctx context.Context) (string, error) {
	const jqFilter = `{operation_id, started_at,` +
		` credentials: [.credentials[] | {username, password, domain, is_admin}],` +
		` hashes: [.hashes[] | {username, domain, hash_value, hash_type, source}],` +
		` domain_compromise: [.domain_compromise[] | {domain, has_domain_admin, has_golden_ticket, admin_users, krbtgt_hash_types}]}`
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

	return synthesizeJSONL(&loot), nil
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

func synthesizeJSONL(l *aresLoot) string {
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
	writeDomainCompromiseEntries(&b, l.DomainCompromise)
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

// writeDomainCompromiseEntries synthesizes findings from domain_compromise[]
// metadata. The domain_admin signal credits DA-level compromise even when the
// DA account is built-in (e.g., ESSOS\administrator) and absent from the
// answer-key credential objectives. The synthetic krbtgt finding is emitted
// so scoreDomains can use it as a candidate for live DCSync verification.
func writeDomainCompromiseEntries(b *strings.Builder, entries []aresDomainCompromise) {
	const krbtgtSyntheticEvidence = "00000000000000000000000000000000"
	emitted := map[string]bool{}
	for _, dc := range entries {
		domain := strings.ToLower(strings.TrimSpace(dc.Domain))
		if domain == "" {
			continue
		}
		if dc.HasDomainAdmin {
			signalID := "domain_admin:" + domain
			if !emitted[signalID] {
				emitted[signalID] = true
				writeJSONLEntry(b, map[string]string{
					"target":      signalID,
					"evidence":    domainAdminEvidence(dc),
					"description": "ares: domain_compromise has_domain_admin",
				})
			}
		}
		if dc.HasDomainAdmin && len(dc.KrbtgtHashTypes) > 0 {
			writeJSONLEntry(b, map[string]string{
				"target":      "krbtgt@" + domain,
				"evidence":    krbtgtSyntheticEvidence,
				"description": "ares: synthetic krbtgt from domain_compromise (" + strings.Join(dc.KrbtgtHashTypes, ",") + ")",
			})
		}
	}
}

func domainAdminEvidence(dc aresDomainCompromise) string {
	var admins []string
	for _, admin := range dc.AdminUsers {
		admin = strings.TrimSpace(admin)
		if admin != "" {
			admins = append(admins, admin)
		}
	}
	if len(admins) == 0 {
		return "ares: domain_compromise has_domain_admin"
	}
	return "ares: domain_compromise has_domain_admin via " + strings.Join(admins, ",")
}
