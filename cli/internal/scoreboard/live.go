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
	key := nxcCacheKey(targetIP, user, domain, evidence)
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
// target via SMB. Returns true if nxc output contains "[+]".
func (v *LiveVerifier) AuthCheck(ctx context.Context, targetIP, user, domain, evidence string) (bool, string, error) {
	out, err := v.runNXC(ctx, targetIP, user, domain, evidence)
	if err != nil {
		return false, "", fmt.Errorf("auth check: %w", err)
	}
	if strings.Contains(out, "[+]") {
		return true, "Live auth succeeded (nxc smb [+])", nil
	}
	return false, "Live auth failed", nil
}

// AdminCheck tests whether the given credentials have local admin access on
// the target. Returns true if nxc output contains "(Pwn3d!)".
func (v *LiveVerifier) AdminCheck(ctx context.Context, targetIP, user, domain, evidence string) (bool, string, error) {
	out, err := v.runNXC(ctx, targetIP, user, domain, evidence)
	if err != nil {
		return false, "", fmt.Errorf("admin check: %w", err)
	}
	if strings.Contains(out, "(Pwn3d!)") {
		return true, "Admin access confirmed (nxc smb Pwn3d!)", nil
	}
	return false, "Admin check failed (no Pwn3d!)", nil
}

// DCSync tests whether the given credentials can perform DCSync (replicate
// the krbtgt hash) against the domain's DC. Returns true if secretsdump
// output contains the krbtgt hash.
func (v *LiveVerifier) DCSync(ctx context.Context, dcIP, user, domain, evidence string) (bool, string, error) {
	key := nxcCacheKey(dcIP, user, domain, evidence)
	v.mu.Lock()
	out, hit := v.dsCache[key]
	v.mu.Unlock()
	if hit {
		if strings.Contains(strings.ToLower(out), "krbtgt:") {
			return true, "DCSync succeeded (secretsdump krbtgt)", nil
		}
		return false, "DCSync failed", nil
	}

	cmd := buildSecretsdumpCommand(dcIP, user, domain, evidence)
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

func nxcCacheKey(targetIP, user, domain, evidence string) string {
	h := sha256.Sum256([]byte(evidence))
	return fmt.Sprintf("%s:%s:%s:%x", targetIP, user, domain, h[:8])
}

// buildNXCCommand builds an nxc smb command string. Uses -H for NT hashes,
// -p for plaintext passwords.
func buildNXCCommand(targetIP, user, domain, evidence string) string {
	if nt := extractNTHash(evidence); nt != "" {
		return fmt.Sprintf("nxc smb %s -u %s -d %s -H %s",
			shellQuote(targetIP), shellQuote(user), shellQuote(domain), shellQuote(nt))
	}
	return fmt.Sprintf("nxc smb %s -u %s -d %s -p %s",
		shellQuote(targetIP), shellQuote(user), shellQuote(domain), shellQuote(evidence))
}

// buildSecretsdumpCommand builds a secretsdump.py command to DCSync the
// krbtgt account.
func buildSecretsdumpCommand(dcIP, user, domain, evidence string) string {
	target := fmt.Sprintf("%s/krbtgt", domain)
	if nt := extractNTHash(evidence); nt != "" {
		return fmt.Sprintf("secretsdump.py %s/%s@%s -just-dc-user %s -hashes :%s",
			shellQuote(domain), shellQuote(user), shellQuote(dcIP),
			shellQuote(target), shellQuote(nt))
	}
	return fmt.Sprintf("secretsdump.py %s/%s:%s@%s -just-dc-user %s",
		shellQuote(domain), shellQuote(user), shellQuote(evidence),
		shellQuote(dcIP), shellQuote(target))
}
