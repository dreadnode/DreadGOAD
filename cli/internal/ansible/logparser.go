package ansible

import (
	"regexp"
	"strings"
)

var (
	failedRe          = regexp.MustCompile(`failed=[1-9][0-9]*`)
	unreachableRe     = regexp.MustCompile(`unreachable=[1-9][0-9]*`)
	failedHostRe      = regexp.MustCompile(`(?m)^([a-zA-Z0-9_-]+)\s+:.*failed=[1-9]`)
	unreachableHostRe = regexp.MustCompile(`(?m)^([a-zA-Z0-9_-]+)\s+:.*unreachable=[1-9]`)
)

// CheckAnsibleSuccess analyzes Ansible output to determine if the run succeeded.
// Returns true if no failures detected.
func CheckAnsibleSuccess(output string) bool {
	// Primary: check PLAY RECAP for failures
	if idx := strings.Index(output, "PLAY RECAP"); idx >= 0 {
		recap := output[idx:]
		if failedRe.MatchString(recap) || unreachableRe.MatchString(recap) {
			return false
		}
	}

	// Secondary: check for fatal errors not followed by "...ignoring"
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "fatal:") {
			// Check next 20 lines for "...ignoring" (multi-line YAML output can be long)
			end := i + 21
			if end > len(lines) {
				end = len(lines)
			}
			context := strings.Join(lines[i:end], "\n")
			if !strings.Contains(context, "...ignoring") {
				return false
			}
		}
	}

	// Check for retry indicator
	if strings.Contains(output, "to retry, use:") {
		return false
	}

	return true
}

// ExtractFailedHosts parses PLAY RECAP to find hosts with failures or unreachable status.
func ExtractFailedHosts(output string) []string {
	seen := make(map[string]bool)
	var hosts []string

	for _, re := range []*regexp.Regexp{failedHostRe, unreachableHostRe} {
		for _, m := range re.FindAllStringSubmatch(output, -1) {
			host := m[1]
			if !seen[host] {
				seen[host] = true
				hosts = append(hosts, host)
			}
		}
	}
	return hosts
}
