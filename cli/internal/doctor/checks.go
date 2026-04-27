package doctor

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/sshconfig"
	"github.com/fatih/color"
)

// CheckResult holds a single doctor check result.
type CheckResult struct {
	Name    string
	Status  string // pass, fail, warn
	Message string
}

// LudusOptions describes the Ludus connection settings doctor needs to probe.
// SSHHost being non-empty toggles SSH-mode; otherwise the ludus CLI is expected
// locally.
//
// When ResolveAlias is true, SSHHost is treated as an ssh_config alias and
// passed through `ssh -G` to derive the real hostname/port for the TCP probe.
type LudusOptions struct {
	APIKey       string
	SSHHost      string
	SSHUser      string
	SSHKeyPath   string
	SSHPassword  string
	SSHPort      int
	ResolveAlias bool
}

// Options configures which checks RunChecks runs.
type Options struct {
	InventoryPath string
	ProjectRoot   string
	Provider      string // aws | ludus | proxmox (empty defaults to aws)
	Ludus         LudusOptions
}

// RunChecks executes pre-flight checks tailored to the configured provider and
// returns the results.
func RunChecks(opts Options) []CheckResult {
	var results []CheckResult

	results = append(results, checkAnsibleVersion())
	results = append(results, checkCommand("python3", "Python 3"))
	results = append(results, checkCommand("jq", "jq"))
	results = append(results, checkCommand("zip", "zip"))
	results = append(results, checkInventoryFile(opts.InventoryPath))
	results = append(results, checkAnsibleCollections()...)

	switch opts.Provider {
	case "ludus":
		results = append(results, runLudusChecks(opts.Ludus)...)
	default:
		// AWS is the historical default; proxmox currently uses the same
		// terraform/terragrunt toolchain so it falls through here too.
		results = append(results, checkCommand("aws", "AWS CLI"))
		results = append(results, checkAWSCredentials())
		results = append(results, checkTerragrunt())
		results = append(results, checkTerraformOrTofu())
	}

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

// runLudusChecks runs Ludus-specific pre-flight checks, dispatching on whether
// the CLI is invoked locally on the Ludus host or remotely over SSH.
func runLudusChecks(opts LudusOptions) []CheckResult {
	var results []CheckResult

	if opts.SSHHost == "" {
		// Local mode: ludus CLI must be on PATH.
		results = append(results, checkCommand("ludus", "Ludus CLI"))
	} else {
		results = append(results, checkLudusSSH(opts)...)
	}

	results = append(results, checkLudusAPIKey(opts.APIKey))

	return results
}

func checkLudusAPIKey(configured string) CheckResult {
	if configured != "" {
		return CheckResult{Name: "Ludus API Key", Status: "pass", Message: "set via ludus.api_key"}
	}
	if os.Getenv("LUDUS_API_KEY") != "" {
		return CheckResult{Name: "Ludus API Key", Status: "pass", Message: "set via LUDUS_API_KEY env"}
	}
	return CheckResult{
		Name:    "Ludus API Key",
		Status:  "fail",
		Message: "not configured. Set ludus.api_key in dreadgoad.yaml or export LUDUS_API_KEY",
	}
}

func checkLudusSSH(opts LudusOptions) []CheckResult {
	var results []CheckResult

	// ssh binary is required for any SSH-mode operation; sshpass only when
	// password auth is in use.
	results = append(results, checkCommand("ssh", "ssh client"))
	if opts.SSHPassword != "" {
		results = append(results, checkCommand("sshpass", "sshpass (password auth)"))
	}

	probeHost, port := opts.SSHHost, opts.SSHPort
	if opts.ResolveAlias {
		// `ssh -G <alias>` returns the same hostname/port the ssh client
		// itself would use, so the probe matches what the real connection
		// will hit.
		if r, err := sshconfig.Resolve(opts.SSHHost); err == nil {
			probeHost = r.Hostname
			if port == 0 {
				port = r.Port
			}
		}
	}
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(probeHost, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		results = append(results, CheckResult{
			Name:    "Ludus SSH host",
			Status:  "fail",
			Message: fmt.Sprintf("cannot reach %s: %v", addr, err),
		})
		return results
	}
	_ = conn.Close()
	msg := fmt.Sprintf("%s reachable", addr)
	if opts.ResolveAlias && probeHost != opts.SSHHost {
		msg = fmt.Sprintf("%s reachable (via ssh_config alias %q)", addr, opts.SSHHost)
	}
	results = append(results, CheckResult{
		Name:    "Ludus SSH host",
		Status:  "pass",
		Message: msg,
	})

	return results
}
