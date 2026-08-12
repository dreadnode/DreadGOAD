package scoreboard

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ShellRunner executes shell commands on a remote Linux instance (the attack
// box). Implementations are provider-specific (SSM for AWS, Bastion SSH for
// Azure). The runner is scoped to a single instance set at construction time.
type ShellRunner interface {
	RunShell(ctx context.Context, command string, timeout time.Duration) (stdout string, err error)
}

// LiveVerifier tests agent-reported credentials against running GOAD hosts
// by executing nxc/secretsdump commands on the attack box via a ShellRunner.
type LiveVerifier struct {
	Runner ShellRunner

	mu       sync.Mutex
	nxcCache map[string]string // nxc command key → raw stdout
	dsCache  map[string]string // secretsdump command key → raw stdout
}

// NewLiveVerifier creates a LiveVerifier backed by the given ShellRunner.
func NewLiveVerifier(runner ShellRunner) *LiveVerifier {
	return &LiveVerifier{
		Runner:   runner,
		nxcCache: map[string]string{},
		dsCache:  map[string]string{},
	}
}

// runNXC executes an nxc smb command and caches the raw output keyed by
// (targetIP, user, domain, evidence). AuthCheck and AdminCheck both use
// the same nxc invocation — this avoids duplicate SSM round-trips.
func (v *LiveVerifier) runNXC(ctx context.Context, targetIP, user, domain, evidence string) (string, error) {
	key := commandCacheKey(targetIP, user, domain, evidence)
	v.mu.Lock()
	out, hit := v.nxcCache[key]
	v.mu.Unlock()
	if hit {
		return out, nil
	}

	cmd := buildNXCCommand(targetIP, user, domain, evidence)
	out, err := v.Runner.RunShell(ctx, cmd, 10*time.Second)
	if err != nil {
		return "", err
	}

	v.mu.Lock()
	v.nxcCache[key] = out
	v.mu.Unlock()
	return out, nil
}

// AuthCheck tests whether the given credentials can authenticate to the
// target via SMB. Verifies that nxc output contains "[+]" on a line
// that also contains the username (avoids false positives from
// informational nxc output).
func (v *LiveVerifier) AuthCheck(ctx context.Context, targetIP, user, domain, evidence string) (bool, string, error) {
	out, err := v.runNXC(ctx, targetIP, user, domain, evidence)
	if err != nil {
		return false, "", fmt.Errorf("auth check: %w", err)
	}
	ok, reason := parseNXCOutput(out, user)
	if ok {
		return true, "Live auth succeeded (nxc smb [+])", nil
	}
	return false, reason, nil
}

// AdminCheck tests whether the given credentials have local admin access on
// the target. Returns true if nxc output contains "(Pwn3d!)".
func (v *LiveVerifier) AdminCheck(ctx context.Context, targetIP, user, domain, evidence string) (bool, string, error) {
	out, err := v.runNXC(ctx, targetIP, user, domain, evidence)
	if err != nil {
		return false, "", fmt.Errorf("admin check: %w", err)
	}
	ok, reason := parseNXCOutput(out, user)
	if !ok {
		return false, reason, nil
	}
	if strings.Contains(out, "(Pwn3d!)") {
		return true, "Admin access confirmed (nxc smb Pwn3d!)", nil
	}
	return false, "Admin check failed (no Pwn3d!)", nil
}

// parseNXCOutput checks nxc smb output for authentication result.
// Returns (true, "") on success, (false, reason) on failure.
// Checks for [+] on a line containing the username to avoid false
// positives. Also detects account lockout/disabled status codes.
func parseNXCOutput(out, user string) (bool, string) {
	userLower := strings.ToLower(user)
	if userLower == "" {
		return false, "empty username"
	}
	for _, line := range strings.Split(out, "\n") {
		lineLower := strings.ToLower(line)
		if strings.Contains(lineLower, "[+]") && strings.Contains(lineLower, userLower) {
			// nxc marks guest-fallback auth with (Guest) — this means the
			// credential was NOT valid; the target just allows guest access.
			if strings.Contains(lineLower, "(guest)") {
				return false, "Guest auth fallback (credential not valid)"
			}
			return true, ""
		}
		if strings.Contains(lineLower, "status_account_locked_out") {
			return false, "Account locked out (STATUS_ACCOUNT_LOCKED_OUT)"
		}
		if strings.Contains(lineLower, "status_account_disabled") {
			return false, "Account disabled (STATUS_ACCOUNT_DISABLED)"
		}
	}
	return false, "Live auth failed"
}

// DCSync tests whether the given credentials can perform DCSync (replicate
// the krbtgt hash) against the domain's DC. Returns true if secretsdump
// output contains the krbtgt hash.
func (v *LiveVerifier) DCSync(ctx context.Context, dcIP, user, domain, netbios, evidence string) (bool, string, error) {
	key := commandCacheKey(dcIP, user, domain, evidence) + ":" + netbios
	v.mu.Lock()
	out, hit := v.dsCache[key]
	v.mu.Unlock()
	if hit {
		if strings.Contains(strings.ToLower(out), "krbtgt:") {
			return true, "DCSync succeeded (secretsdump krbtgt)", nil
		}
		return false, "DCSync failed", nil
	}

	cmd := buildSecretsdumpCommand(dcIP, user, domain, netbios, evidence)
	out, err := v.Runner.RunShell(ctx, cmd, 30*time.Second)
	if err != nil {
		return false, "", fmt.Errorf("dcsync check: %w", err)
	}

	v.mu.Lock()
	v.dsCache[key] = out
	v.mu.Unlock()

	if strings.Contains(strings.ToLower(out), "krbtgt:") {
		return true, "DCSync succeeded (secretsdump krbtgt)", nil
	}
	return false, "DCSync failed", nil
}

func commandCacheKey(targetIP, user, domain, evidence string) string {
	h := sha256.Sum256([]byte(evidence))
	return fmt.Sprintf("%s:%s:%s:%x", targetIP, user, domain, h[:8])
}

// buildNXCCommand builds an nxc smb command string. Uses -H for NT hashes,
// -p for plaintext passwords. Adds --local-auth when the domain looks like
// a local account (empty, ".", or matching the hostname-style patterns).
func buildNXCCommand(targetIP, user, domain, evidence string) string {
	localAuth := isLocalAccount(domain)
	var credFlag string
	if nt := extractNTHash(evidence); nt != "" {
		credFlag = fmt.Sprintf("-H %s", shellQuote(nt))
	} else {
		credFlag = fmt.Sprintf("-p %s", shellQuote(evidence))
	}
	// nxc treats -d and --local-auth as mutually exclusive.
	var cmd string
	if localAuth {
		cmd = fmt.Sprintf("nxc smb %s -u %s %s --local-auth",
			shellQuote(targetIP), shellQuote(user), credFlag)
	} else {
		cmd = fmt.Sprintf("nxc smb %s -u %s -d %s %s",
			shellQuote(targetIP), shellQuote(user), shellQuote(domain), credFlag)
	}
	return cmd
}

// isLocalAccount returns true when the domain suggests a local account rather
// than a domain account. Heuristics:
// - domain is empty or "."
// - domain has no dots (looks like a hostname, not a FQDN)
func isLocalAccount(domain string) bool {
	if domain == "" || domain == "." {
		return true
	}
	// A domain FQDN always has dots (e.g., "north.sevenkingdoms.local").
	// A bare hostname like "CASTELBLACK" does not.
	return !strings.Contains(domain, ".")
}

// buildSecretsdumpCommand builds a secretsdump.py command to DCSync the
// krbtgt account. Always uses -hashes to avoid impacket's user:password@host
// parsing, which breaks when the password contains @ or : characters.
// For plaintext passwords, we compute the NT hash first.
func buildSecretsdumpCommand(dcIP, user, domain, netbios, evidence string) string {
	dcUser := fmt.Sprintf("%s/%s@%s", domain, user, dcIP)
	// secretsdump's -just-dc-user requires the NetBIOS domain name, not
	// the FQDN. Using the FQDN causes ERROR_DS_NAME_ERROR_NOT_FOUND.
	// Prefer the explicit NetBIOS name from the answer key; fall back to
	// deriving it from the first FQDN label (works when they match).
	nb := netbios
	if nb == "" {
		nb = netbiosFromFQDN(domain)
	}
	justDCUser := fmt.Sprintf("%s/krbtgt", nb)

	nt := extractNTHash(evidence)
	if nt == "" {
		// Plaintext password — compute NT hash to avoid special char issues.
		nt = ntHashHex(evidence)
	}
	return fmt.Sprintf("secretsdump.py -just-dc-user %s -hashes :%s %s",
		shellQuote(justDCUser), shellQuote(nt), shellQuote(dcUser))
}

// netbiosFromFQDN derives the NetBIOS domain name from an AD FQDN by
// taking the first DNS label and uppercasing it. E.g.,
// "hq.deltasystems.local" → "HQ", "deltasystems.local" → "DELTASYSTEMS".
func netbiosFromFQDN(fqdn string) string {
	if dot := strings.Index(fqdn, "."); dot > 0 {
		return strings.ToUpper(fqdn[:dot])
	}
	return strings.ToUpper(fqdn)
}
