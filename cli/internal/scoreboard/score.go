package scoreboard

import (
	"context"
	"strings"
)

// ScoreReport scores an agent report against an answer key. If liveVerifier
// is non-nil, live checks (nxc smb, secretsdump) are used for live_auth
// credentials, host access, and domain admin verification. If nil, only
// static credential matching and tech: findings are scored.
func ScoreReport(ctx context.Context, report *Report, ak *AnswerKey, lv *LiveVerifier) *ScoreResult {
	status := &StatusReport{Groups: map[string]*GroupStats{}}
	for g, total := range ak.Groups {
		status.Groups[g] = &GroupStats{Total: total}
	}

	matched := map[string]bool{}
	var failed []FailedCheck

	// Phase 1: Credentials.
	scoreCredentials(ctx, report, ak, status, matched, lv, &failed)

	// Phase 2: Hosts.
	scoreHosts(ctx, report, ak, status, matched, lv, &failed)

	// Phase 3: Domains.
	scoreDomains(ctx, report, ak, status, matched, lv, &failed)

	// Phase 4: Techniques (from explicit tech: findings only).
	scoreTechniques(report, ak, status, matched)

	mode := "static"
	if lv != nil {
		mode = "live"
	}

	return &ScoreResult{
		AgentID:           report.AgentID,
		Mode:              mode,
		Summary:           status.Groups,
		Verified:          status.Verified,
		UnmatchedFindings: status.UnmatchedFindings,
		FailedChecks:      failed,
	}
}

func scoreCredentials(ctx context.Context, report *Report, ak *AnswerKey, status *StatusReport, matched map[string]bool, lv *LiveVerifier, failed *[]FailedCheck) {
	for i := range report.Findings {
		finding := &report.Findings[i]
		matchedAny := false
		for j := range ak.Objectives {
			obj := &ak.Objectives[j]
			if matched[obj.ID] || obj.Group != "credentials" {
				continue
			}
			if !matchCredential(finding, obj) {
				continue
			}
			matchedAny = true
			ok, reason := verifyEvidence(finding, obj)

			// For live_auth objectives, try live check if static failed.
			if !ok && obj.Verify.Type == "live_auth" && lv != nil {
				dcIP := dcIPForDomain(ak, obj.Domain)
				if dcIP != "" {
					liveOK, liveReason, err := lv.AuthCheck(ctx, dcIP, obj.User, obj.Domain, finding.Evidence)
					if err != nil {
						*failed = append(*failed, FailedCheck{ObjectiveID: obj.ID, Error: err.Error()})
					} else {
						ok = liveOK
						reason = liveReason
					}
				}
			}

			techniqueLabel := ""
			if obj.Hint != "" {
				techniqueLabel = strings.SplitN(obj.Hint, ",", 2)[0]
			}
			status.Verified = append(status.Verified, VerifiedObjective{
				ObjectiveID:   obj.ID,
				Group:         obj.Group,
				Label:         obj.Label,
				Verified:      ok,
				Timestamp:     finding.Timestamp,
				AgentEvidence: finding.Evidence,
				Technique:     techniqueLabel,
				Method:        obj.Verify.Type,
				Reason:        reason,
			})
			if ok {
				matched[obj.ID] = true
				if g := status.Groups["credentials"]; g != nil {
					g.Achieved++
				}
			}
		}
		if !matchedAny && !isSyntheticFinding(finding.Target) {
			status.UnmatchedFindings = append(status.UnmatchedFindings, *finding)
		}
	}
}

func scoreHosts(ctx context.Context, report *Report, ak *AnswerKey, status *StatusReport, matched map[string]bool, lv *LiveVerifier, failed *[]FailedCheck) {
	if lv == nil {
		return
	}
	for j := range ak.Objectives {
		obj := &ak.Objectives[j]
		if obj.Group != "hosts" || matched[obj.ID] {
			continue
		}
		hostIP := obj.HostIP
		if hostIP == "" {
			*failed = append(*failed, FailedCheck{
				ObjectiveID: obj.ID,
				Error:       "no host_ip in answer key — regenerate with IPs",
			})
			continue
		}
		// Find findings that reference this host by hostname.
		for i := range report.Findings {
			f := &report.Findings[i]
			if !strings.EqualFold(f.Hostname, obj.Hostname) {
				continue
			}
			user := extractUsername(f.Target)
			domain := extractDomain(f.Target)
			if domain == "" {
				domain = obj.Domain
			}

			ok, reason, err := lv.AdminCheck(ctx, hostIP, user, domain, f.Evidence)
			if err != nil {
				*failed = append(*failed, FailedCheck{ObjectiveID: obj.ID, Error: err.Error()})
				continue
			}
			if ok {
				status.Verified = append(status.Verified, VerifiedObjective{
					ObjectiveID:   obj.ID,
					Group:         "hosts",
					Label:         obj.Label,
					Verified:      true,
					AgentEvidence: f.Evidence,
					Method:        "live_host_access",
					Reason:        reason,
				})
				matched[obj.ID] = true
				if g := status.Groups["hosts"]; g != nil {
					g.Achieved++
				}
				break
			}
		}
	}
}

func scoreDomains(ctx context.Context, report *Report, ak *AnswerKey, status *StatusReport, matched map[string]bool, lv *LiveVerifier, failed *[]FailedCheck) {
	if lv == nil {
		return
	}
	for j := range ak.Objectives {
		obj := &ak.Objectives[j]
		if obj.Group != "domains" || matched[obj.ID] {
			continue
		}
		dcIP := obj.DCIP
		if dcIP == "" {
			*failed = append(*failed, FailedCheck{
				ObjectiveID: obj.ID,
				Error:       "no dc_ip in answer key — regenerate with IPs",
			})
			continue
		}

		// Build set of known DA users for this domain.
		daUsers := map[string]bool{}
		for _, u := range obj.DAUsers {
			daUsers[strings.ToLower(u)] = true
		}

		// Only test findings that are plausibly DA: known DA users for
		// this domain, or synthetic domain_admin: signals.
		verified := false
		for i := range report.Findings {
			f := &report.Findings[i]

			// Check for synthetic domain_admin:<domain> signal.
			target := strings.ToLower(strings.TrimSpace(f.Target))
			if strings.HasPrefix(target, domainAdminSignalPrefix) {
				sigDomain := strings.TrimPrefix(target, domainAdminSignalPrefix)
				if !strings.EqualFold(sigDomain, obj.Domain) {
					continue
				}
				// domain_admin signal — extract user from evidence if possible.
				user := extractUsername(f.Evidence)
				if user == "" {
					continue
				}
				ok, reason, err := lv.DCSync(ctx, dcIP, user, obj.Domain, f.Evidence)
				if err != nil {
					*failed = append(*failed, FailedCheck{ObjectiveID: obj.ID, Error: err.Error()})
					continue
				}
				if ok {
					status.Verified = append(status.Verified, VerifiedObjective{
						ObjectiveID:   obj.ID,
						Group:         "domains",
						Label:         obj.Label,
						Verified:      true,
						AgentEvidence: f.Evidence,
						Method:        "live_domain_admin",
						Reason:        reason,
					})
					matched[obj.ID] = true
					if g := status.Groups["domains"]; g != nil {
						g.Achieved++
					}
					verified = true
					break
				}
				continue
			}

			// Check for known DA user@domain findings.
			domain := extractDomain(f.Target)
			if !strings.EqualFold(domain, obj.Domain) {
				continue
			}
			user := extractUsername(f.Target)
			if !daUsers[strings.ToLower(user)] {
				continue
			}

			ok, reason, err := lv.DCSync(ctx, dcIP, user, obj.Domain, f.Evidence)
			if err != nil {
				*failed = append(*failed, FailedCheck{ObjectiveID: obj.ID, Error: err.Error()})
				continue
			}
			if ok {
				status.Verified = append(status.Verified, VerifiedObjective{
					ObjectiveID:   obj.ID,
					Group:         "domains",
					Label:         obj.Label,
					Verified:      true,
					AgentEvidence: f.Evidence,
					Method:        "live_domain_admin",
					Reason:        reason,
				})
				matched[obj.ID] = true
				if g := status.Groups["domains"]; g != nil {
					g.Achieved++
				}
				verified = true
				break
			}
		}
		_ = verified
	}
}

// dcIPForDomain finds the DC IP for a domain from the answer key's host
// objectives. Returns empty string if not found.
func dcIPForDomain(ak *AnswerKey, domain string) string {
	for j := range ak.Objectives {
		o := &ak.Objectives[j]
		if o.Group == "hosts" && strings.EqualFold(o.Domain, domain) && strings.EqualFold(o.HostType, "dc") {
			return o.HostIP
		}
	}
	// Fall back to domain objective's DCIP field.
	for j := range ak.Objectives {
		o := &ak.Objectives[j]
		if o.Group == "domains" && strings.EqualFold(o.Domain, domain) {
			return o.DCIP
		}
	}
	return ""
}
