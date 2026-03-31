package ansible

import (
	"regexp"
	"strings"
)

// ErrorType classifies Ansible failures for error-specific retry strategies.
type ErrorType string

const (
	ErrFactGathering      ErrorType = "fact_gathering"
	ErrNetworkAdapter     ErrorType = "network_adapter"
	ErrSSMTransfer        ErrorType = "ssm_transfer_error"
	ErrSSMReconnection    ErrorType = "ssm_reconnection_needed"
	ErrPowerShell         ErrorType = "powershell_interactive"
	ErrSSMUserAccount     ErrorType = "ssm_user_account_issue"
	ErrMSIInstaller       ErrorType = "msi_installer_error"
	ErrUnclassified       ErrorType = "unclassified"
)

var fatalMsgRe = regexp.MustCompile(`(?m)msg:|rc:|stderr:`)

// DetectErrorType analyzes Ansible output and classifies the failure.
func DetectErrorType(output string) (ErrorType, string) {
	switch {
	case containsAny(output,
		"FAILED! => .* setup",
		"Invalid control character",
		"modules failed to execute: ansible.legacy.setup",
		"Module result deserialization failed"):
		return ErrFactGathering, "fact gathering/module deserialization failure"

	case strings.Contains(output, "No MSFT_NetAdapter objects found with property 'Name' equal to 'Ethernet3'"):
		return ErrNetworkAdapter, "network adapter Ethernet3 not found"

	case strings.Contains(output, "failed to transfer file"):
		return ErrSSMTransfer, "SSM file transfer error"

	case containsAny(output, "TargetNotConnected", "is not connected",
		"Timed out waiting for last boot time", "timeout waiting for system to reboot"):
		return ErrSSMReconnection, "SSM target not connected / reboot timeout"

	case strings.Contains(output, "Windows PowerShell is in NonInteractive mode"):
		return ErrPowerShell, "PowerShell interactive mode issue"

	case containsAny(output, "ssm-user.*disabled", "SSM.*account.*issue", "Windows Local SAM"):
		return ErrSSMUserAccount, "SSM user account disabled/destroyed"

	case containsAny(output, "rc: 1603", "rc: 3010"):
		return ErrMSIInstaller, "MSI installer error (rc 1603/3010)"

	default:
		detail := extractFatalContext(output)
		return ErrUnclassified, detail
	}
}

func containsAny(s string, patterns ...string) bool {
	for _, p := range patterns {
		if strings.Contains(p, ".*") || strings.Contains(p, "[") {
			if re, err := regexp.Compile(p); err == nil && re.MatchString(s) {
				return true
			}
		} else if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func extractFatalContext(output string) string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "fatal:") {
			// Grab up to 5 lines after for context
			end := i + 6
			if end > len(lines) {
				end = len(lines)
			}
			context := strings.Join(lines[i:end], "\n")
			// Extract msg/rc/stderr lines
			matches := fatalMsgRe.FindAllString(context, -1)
			if len(matches) > 0 {
				return strings.TrimSpace(context)
			}
			// Truncate to 120 chars
			if len(line) > 120 {
				return line[:120]
			}
			return line
		}
	}
	// Fallback: last FAILED/fatal line
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "FAILED") || strings.Contains(lines[i], "fatal") {
			line := lines[i]
			if len(line) > 120 {
				return line[:120]
			}
			return line
		}
	}
	return "unknown error"
}
