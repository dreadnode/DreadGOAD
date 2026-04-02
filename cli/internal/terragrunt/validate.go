package terragrunt

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fatih/color"
)

// ValidationResult holds the outcome of environment validation.
type ValidationResult struct {
	Errors   []string
	Warnings []string
}

// OK returns true if there are no errors.
func (v *ValidationResult) OK() bool {
	return len(v.Errors) == 0
}

// ValidateEnvironment checks that the infra environment directory is correctly
// structured and configured. basePath is the deployment dir (infra/goad-deployment),
// env is the environment name, and region is the AWS region.
func ValidateEnvironment(basePath, env, region string) *ValidationResult {
	result := &ValidationResult{}

	envPath := filepath.Join(basePath, env)
	regionPath := filepath.Join(envPath, region)

	// Check directory structure
	requiredFiles := []string{
		filepath.Join(basePath, "host.hcl"),
		filepath.Join(basePath, "host-registry.yaml"),
		filepath.Join(envPath, "env.hcl"),
		filepath.Join(regionPath, "region.hcl"),
		filepath.Join(regionPath, "network", "terragrunt.hcl"),
	}

	for _, f := range requiredFiles {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			result.Errors = append(result.Errors, fmt.Sprintf("missing required file: %s", f))
		}
	}

	// Check GOAD host directories
	goadHosts := []string{"dc01", "dc02", "dc03", "srv02", "srv03"}
	for _, host := range goadHosts {
		hclPath := filepath.Join(regionPath, "goad", host, "terragrunt.hcl")
		if _, err := os.Stat(hclPath); os.IsNotExist(err) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("GOAD host config missing: %s", hclPath))
		}
	}

	// Validate env.hcl content
	envHCL := filepath.Join(envPath, "env.hcl")
	if content, err := os.ReadFile(envHCL); err == nil {
		s := string(content)
		validateHCLField(s, "deployment_name", result)
		validateHCLField(s, "aws_account_id", result)
		validateHCLField(s, "env", result)

		// Check for CHANGE_ME placeholders
		if strings.Contains(s, "CHANGE_ME") {
			result.Errors = append(result.Errors, "env.hcl contains CHANGE_ME placeholder(s) - update with your values")
		}

		// Validate env name matches
		envPattern := regexp.MustCompile(`env\s*=\s*"([^"]*)"`)
		if matches := envPattern.FindStringSubmatch(s); len(matches) > 1 {
			if matches[1] != env {
				result.Errors = append(result.Errors, fmt.Sprintf("env.hcl env=%q does not match --env=%q", matches[1], env))
			}
		}
	}

	// Validate region.hcl content
	regionHCL := filepath.Join(regionPath, "region.hcl")
	if content, err := os.ReadFile(regionHCL); err == nil {
		s := string(content)
		regionPattern := regexp.MustCompile(`aws_region\s*=\s*"([^"]*)"`)
		if matches := regionPattern.FindStringSubmatch(s); len(matches) > 1 {
			if matches[1] != region {
				result.Errors = append(result.Errors, fmt.Sprintf("region.hcl aws_region=%q does not match --region=%q", matches[1], region))
			}
		}
	}

	// Check for CHANGE_ME in GOAD host configs
	for _, host := range goadHosts {
		hclPath := filepath.Join(regionPath, "goad", host, "terragrunt.hcl")
		if content, err := os.ReadFile(hclPath); err == nil {
			if strings.Contains(string(content), "CHANGE_ME") {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("goad/%s/terragrunt.hcl has CHANGE_ME placeholder(s) - update AMI IDs and passwords", host))
			}
		}
	}

	return result
}

// PrintValidationResult prints the result with colored output.
func PrintValidationResult(result *ValidationResult, env, region string) {
	fmt.Printf("Validating environment: %s (%s)\n\n", env, region)

	if len(result.Errors) > 0 {
		color.Red("Errors:")
		for _, e := range result.Errors {
			fmt.Printf("  %s %s\n", color.RedString("x"), e)
		}
		fmt.Println()
	}

	if len(result.Warnings) > 0 {
		color.Yellow("Warnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  %s %s\n", color.YellowString("!"), w)
		}
		fmt.Println()
	}

	switch {
	case result.OK() && len(result.Warnings) == 0:
		color.Green("Environment validation passed.")
	case result.OK():
		color.Green("Environment validation passed with %d warning(s).", len(result.Warnings))
	default:
		color.Red("Environment validation failed with %d error(s).", len(result.Errors))
	}
}

func validateHCLField(content, field string, result *ValidationResult) {
	pattern := regexp.MustCompile(field + `\s*=\s*"[^"]*"`)
	if !pattern.MatchString(content) {
		result.Errors = append(result.Errors, fmt.Sprintf("env.hcl missing required field: %s", field))
	}
}
