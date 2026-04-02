package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/fatih/color"
)

// CheckResult holds a single doctor check result.
type CheckResult struct {
	Name    string
	Status  string // pass, fail, warn
	Message string
}

// RunChecks executes all pre-flight checks and returns results.
func RunChecks(inventoryPath, projectRoot string) []CheckResult {
	var results []CheckResult

	results = append(results, checkAnsibleVersion())
	results = append(results, checkCommand("aws", "AWS CLI"))
	results = append(results, checkCommand("python3", "Python 3"))
	results = append(results, checkCommand("jq", "jq"))
	results = append(results, checkCommand("zip", "zip"))
	results = append(results, checkAWSCredentials())
	results = append(results, checkInventoryFile(inventoryPath))
	results = append(results, checkTerragrunt())
	results = append(results, checkTerraformOrTofu())
	results = append(results, checkAnsibleCollections()...)

	return results
}

// PrintResults displays check results with color.
func PrintResults(results []CheckResult) {
	passed, failed, warned := 0, 0, 0

	fmt.Println("DreadGOAD Pre-flight Checks")
	fmt.Println(strings.Repeat("=", 40))

	for _, r := range results {
		switch r.Status {
		case "pass":
			color.Green("  [pass] %s: %s", r.Name, r.Message)
			passed++
		case "fail":
			color.Red("  [fail] %s: %s", r.Name, r.Message)
			failed++
		case "warn":
			color.Yellow("  [warn] %s: %s", r.Name, r.Message)
			warned++
		}
	}

	fmt.Println(strings.Repeat("=", 40))
	fmt.Printf("Results: %d passed, %d failed, %d warnings\n", passed, failed, warned)
}

// CheckAnsibleCoreVersion verifies ansible-core is installed and within the
// compatible version range (<2.19). Returns an error if the version is
// incompatible or ansible-core is not found. This is used as a pre-flight
// gate before running playbooks.
func CheckAnsibleCoreVersion() error {
	result := checkAnsibleVersion()
	if result.Status == "fail" {
		return fmt.Errorf("%s", result.Message)
	}
	return nil
}

func checkAnsibleVersion() CheckResult {
	out, err := exec.Command("ansible", "--version").CombinedOutput()
	if err != nil {
		return CheckResult{
			Name:    "ansible-core",
			Status:  "fail",
			Message: "ansible-core not found. Install: pip install 'ansible-core>=2.17.0,<2.18.0'",
		}
	}

	re := regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)
	m := re.FindStringSubmatch(string(out))
	if m == nil {
		return CheckResult{Name: "ansible-core", Status: "fail", Message: "could not parse version"}
	}

	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	version := fmt.Sprintf("%s.%s.%s", m[1], m[2], m[3])

	if major > 2 || (major == 2 && minor >= 19) {
		return CheckResult{
			Name:   "ansible-core",
			Status: "fail",
			Message: fmt.Sprintf("v%s detected. Versions >=2.19 break Windows SSM. "+
				"Fix: pip install 'ansible-core>=2.17.0,<2.18.0'", version),
		}
	}

	if major == 2 && minor >= 17 && minor < 19 {
		return CheckResult{
			Name:    "ansible-core",
			Status:  "pass",
			Message: fmt.Sprintf("v%s (compatible)", version),
		}
	}

	return CheckResult{
		Name:    "ansible-core",
		Status:  "warn",
		Message: fmt.Sprintf("v%s (untested, recommend 2.17.x)", version),
	}
}

func checkCommand(name, label string) CheckResult {
	path, err := exec.LookPath(name)
	if err != nil {
		return CheckResult{Name: label, Status: "fail", Message: "not found in PATH"}
	}
	return CheckResult{Name: label, Status: "pass", Message: path}
}

func checkAWSCredentials() CheckResult {
	out, err := exec.Command("aws", "sts", "get-caller-identity", "--query", "Account", "--output", "text").CombinedOutput()
	if err != nil {
		return CheckResult{
			Name:    "AWS Credentials",
			Status:  "fail",
			Message: "invalid or not configured. Run: aws configure",
		}
	}
	return CheckResult{
		Name:    "AWS Credentials",
		Status:  "pass",
		Message: fmt.Sprintf("account %s", strings.TrimSpace(string(out))),
	}
}

func checkInventoryFile(path string) CheckResult {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return CheckResult{
			Name:    "Inventory",
			Status:  "fail",
			Message: fmt.Sprintf("%s not found", path),
		}
	}
	return CheckResult{Name: "Inventory", Status: "pass", Message: path}
}

func checkTerragrunt() CheckResult {
	out, err := exec.Command("terragrunt", "--version").CombinedOutput()
	if err != nil {
		return CheckResult{
			Name:    "Terragrunt",
			Status:  "warn",
			Message: "not found in PATH (required for infra commands)",
		}
	}
	version := strings.TrimSpace(string(out))
	// Extract just the version line
	for _, line := range strings.Split(version, "\n") {
		if strings.Contains(line, "terragrunt version") || strings.HasPrefix(line, "v") {
			version = strings.TrimSpace(line)
			break
		}
	}
	return CheckResult{Name: "Terragrunt", Status: "pass", Message: version}
}

func checkTerraformOrTofu() CheckResult {
	// Check for tofu first (preferred), then terraform
	if path, err := exec.LookPath("tofu"); err == nil {
		return CheckResult{Name: "Terraform/Tofu", Status: "pass", Message: fmt.Sprintf("tofu: %s", path)}
	}
	if path, err := exec.LookPath("terraform"); err == nil {
		return CheckResult{Name: "Terraform/Tofu", Status: "pass", Message: fmt.Sprintf("terraform: %s", path)}
	}
	return CheckResult{
		Name:    "Terraform/Tofu",
		Status:  "warn",
		Message: "neither tofu nor terraform found in PATH (required for infra commands)",
	}
}

func checkAnsibleCollections() []CheckResult {
	required := []string{
		"ansible.windows",
		"community.general",
		"community.windows",
		"amazon.aws",
		"microsoft.ad",
		"chocolatey.chocolatey",
	}

	out, _ := exec.Command("ansible-galaxy", "collection", "list", "--format", "yaml").CombinedOutput()
	output := string(out)

	var results []CheckResult
	for _, col := range required {
		if strings.Contains(output, col) {
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("Collection: %s", col),
				Status:  "pass",
				Message: "installed",
			})
		} else {
			results = append(results, CheckResult{
				Name:    fmt.Sprintf("Collection: %s", col),
				Status:  "fail",
				Message: "not installed. Run: ansible-galaxy install -r ansible/requirements.yml",
			})
		}
	}
	return results
}
