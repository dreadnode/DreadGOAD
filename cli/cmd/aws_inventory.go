package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
)

type awsInventoryVariable struct {
	key           string
	value         string
	authoritative bool
}

var awsWindowsHostVariables = []awsInventoryVariable{
	{key: "ansible_shell_type", value: "powershell", authoritative: true},
	{key: "ansible_become", value: "false", authoritative: true},
	{key: "ansible_remote_tmp", value: `C:\Windows\Temp`, authoritative: true},
}

var awsHostDefinitionRe = regexp.MustCompile(`^\s*[^;#\s]+\s+.*\bansible_host=`)

// ensureAWSInventoryTransport materializes the connection contract required by
// an AWS range. The configured provider, not a possibly damaged inventory,
// decides whether this contract applies.
func ensureAWSInventoryTransport(cfg *config.Config) error {
	if !cfg.IsAWS() {
		return nil
	}

	region, err := cfg.ResolveRegion()
	if err != nil {
		return err
	}
	runtimePath := cfg.InventoryPath()
	raw, err := os.ReadFile(runtimePath)
	if err != nil {
		return fmt.Errorf("read AWS inventory transport: %w", err)
	}

	updated := string(raw)
	changed := 0
	for _, variable := range awsInventoryTransportVariables(region) {
		_, available := inventoryAllVars(updated)
		assignment, exists := available[variable.key]
		if !variable.authoritative && exists && inventoryAssignmentValue(assignment) != "" {
			continue
		}
		next, replaced := replaceInventoryVariable(
			updated,
			variable.key,
			variable.key+"="+variable.value,
		)
		if replaced {
			updated = next
			changed++
		}
	}
	updated, hostChanges := reconcileAWSWindowsHostVariables(updated)
	changed += hostChanges

	if err := validateAWSInventoryTransportContent(updated, region); err != nil {
		return fmt.Errorf("validate repaired AWS inventory transport: %w", err)
	}
	if changed > 0 {
		if err := writeInventoryAtomically(runtimePath, []byte(updated)); err != nil {
			return err
		}
		slog.Info("reconciled AWS inventory transport",
			"inventory", runtimePath,
			"variables_updated", changed)
	}
	return validateAWSInventoryTransportFile(runtimePath, region)
}

func awsInventoryTransportVariables(region string) []awsInventoryVariable {
	return []awsInventoryVariable{
		{key: "ansible_connection", value: "amazon.aws.aws_ssm", authoritative: true},
		{key: "ansible_aws_ssm_region", value: region, authoritative: true},
		{key: "ansible_aws_ssm_bucket_name", value: "AUTO"},
		{key: "ansible_aws_ssm_s3_addressing_style", value: "virtual"},
		{key: "ansible_aws_ssm_retries", value: "3"},
	}
}

func reconcileAWSWindowsHostVariables(content string) (string, int) {
	keys := make(map[string]bool, len(awsWindowsHostVariables))
	for _, variable := range awsWindowsHostVariables {
		keys[variable.key] = true
	}
	updated, changes := removeInventoryAllVars(content, keys)

	lines := strings.Split(strings.TrimSuffix(updated, "\n"), "\n")
	for i, line := range lines {
		if !awsHostDefinitionRe.MatchString(line) || !usesAWSWindowsShell(line) {
			continue
		}
		for _, variable := range awsWindowsHostVariables {
			var changed bool
			line, changed = upsertInventoryHostVariable(line, variable.key, variable.value)
			if changed {
				changes++
			}
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n") + "\n", changes
}

func removeInventoryAllVars(content string, keys map[string]bool) (string, int) {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	out := make([]string, 0, len(lines))
	inAllVars := false
	changes := 0
	for _, line := range lines {
		if match := inventorySectionRe.FindStringSubmatch(line); match != nil {
			inAllVars = strings.EqualFold(strings.TrimSpace(match[1]), "all:vars")
		}
		if inAllVars {
			trimmed := strings.TrimSpace(line)
			if key, _, found := strings.Cut(trimmed, "="); found && keys[strings.TrimSpace(key)] {
				changes++
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n") + "\n", changes
}

func usesAWSWindowsShell(line string) bool {
	connection := strings.ToLower(inventoryHostVariable(line, "ansible_connection"))
	return connection == "" || strings.Contains(connection, "aws_ssm") ||
		strings.Contains(connection, "winrm") || strings.Contains(connection, "psrp")
}

func inventoryHostVariable(line, key string) string {
	re := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(key) + `=('[^']*'|"[^"]*"|\S*)`)
	match := re.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	return inventoryAssignmentValue(key + "=" + match[1])
}

func upsertInventoryHostVariable(line, key, value string) (string, bool) {
	rendered := value
	if strings.ContainsAny(value, `\ 	`) {
		rendered = "'" + value + "'"
	}
	re := regexp.MustCompile(`(\b` + regexp.QuoteMeta(key) + `=)('[^']*'|"[^"]*"|\S*)`)
	if re.MatchString(line) {
		updated := re.ReplaceAllString(line, "${1}"+rendered)
		return updated, updated != line
	}
	return line + " " + key + "=" + rendered, true
}

func inventoryAssignmentValue(assignment string) string {
	_, value, found := strings.Cut(assignment, "=")
	if !found {
		return ""
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if first == last && (first == '\'' || first == '"') {
			value = value[1 : len(value)-1]
		}
	}
	return value
}

func validateAWSInventoryTransportContent(content, region string) error {
	_, available := inventoryAllVars(content)
	for _, variable := range awsInventoryTransportVariables(region) {
		assignment, exists := available[variable.key]
		value := inventoryAssignmentValue(assignment)
		if !exists || value == "" {
			return fmt.Errorf("inventory is missing required [all:vars] value %s", variable.key)
		}
		if variable.authoritative && value != variable.value {
			return fmt.Errorf("inventory has %s=%q; expected %q", variable.key, value, variable.value)
		}
	}
	return validateAWSWindowsHostVariables(content)
}

func validateAWSWindowsHostVariables(content string) error {
	_, globals := inventoryAllVars(content)
	for _, variable := range awsWindowsHostVariables {
		if _, exists := globals[variable.key]; exists {
			return fmt.Errorf("Windows-only inventory value %s must not be global", variable.key)
		}
	}
	for _, line := range strings.Split(content, "\n") {
		if !awsHostDefinitionRe.MatchString(line) || !usesAWSWindowsShell(line) {
			continue
		}
		for _, variable := range awsWindowsHostVariables {
			if got := inventoryHostVariable(line, variable.key); got != variable.value {
				return fmt.Errorf("AWS Windows host is missing %s=%s", variable.key, variable.value)
			}
		}
	}
	return nil
}

func validateAWSInventoryTransportFile(path, region string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read repaired AWS inventory: %w", err)
	}
	if err := validateAWSInventoryTransportContent(string(raw), region); err != nil {
		return err
	}
	parsed, err := inv.Parse(path)
	if err != nil {
		return fmt.Errorf("parse repaired AWS inventory: %w", err)
	}
	if !parsed.IsSSM() {
		return fmt.Errorf("inventory %s does not resolve to the AWS SSM connection plugin", path)
	}
	return nil
}
