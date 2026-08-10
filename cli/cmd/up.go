package cmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/doctor"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	upSkipDoctor   bool
	upFromStep     string
	upLimit        string
	upPlays        string
	upMaxRetries   int
	upRetryDelay   int
	upInfraModule  string
	upInfraExclude string
	upWithKali     bool
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Deploy the lab end-to-end (doctor → infra → provision → health-check)",
	Long: `One-command lab bring-up. Runs the full pipeline in order:

  1. doctor        pre-flight tooling and connectivity checks
  2. infra apply   provision instances/range (auto-approved)
  3. provision     run Ansible playbooks to build AD
  4. health-check  verify DCs, replication, trusts, services

Stops on the first failing step and prints a resume hint. Use --from <step>
to restart from a specific point. The recommended new-user flow is:

  dreadgoad init && dreadgoad up

On Azure, step 2 also deploys the Bastion and the in-VNet Ansible controller.
Step 3 reaches the Windows hosts only through them, so they are prerequisites
of this pipeline rather than options -- note that Bastion is a billed,
always-on resource. To build the range without them, use 'infra apply'
directly; provisioning will then need another route to the hosts.`,
	Example: `  dreadgoad up
  dreadgoad up --skip-doctor
  dreadgoad up --from provision
  dreadgoad up --limit dc01
  dreadgoad up --with-kali        # Azure: add the Kali attack box`,
	RunE: runUp,
}

func init() {
	rootCmd.AddCommand(upCmd)

	upCmd.Flags().BoolVar(&upSkipDoctor, "skip-doctor", false, "Skip the doctor pre-flight checks")
	upCmd.Flags().StringVar(&upFromStep, "from", "", "Resume from this step (doctor, infra, provision, health-check)")
	upCmd.Flags().StringVar(&upLimit, "limit", "", "Limit provisioning to specific hosts")
	upCmd.Flags().StringVar(&upPlays, "plays", "", "Comma-separated playbooks to run (default: all)")
	upCmd.Flags().IntVar(&upMaxRetries, "max-retries", 0, "Max retry attempts for provisioning")
	upCmd.Flags().IntVar(&upRetryDelay, "retry-delay", 0, "Delay between retries in seconds")
	upCmd.Flags().StringVar(&upInfraModule, "module", "", "Target a specific infra module (default: all)")
	upCmd.Flags().StringVar(&upInfraExclude, "exclude", "", "Exclude infra modules (comma-separated)")
	upCmd.Flags().BoolVar(&upWithKali, "with-kali", false, "(Azure) Also deploy the optional Kali Linux attack box")
}

type upStep struct {
	id   string
	name string
	run  func(cmd *cobra.Command, args []string) error
}

func runUp(cmd *cobra.Command, args []string) error {
	steps := []upStep{
		{id: "doctor", name: "Pre-flight checks", run: runUpDoctor},
		{id: "infra", name: "Infrastructure apply", run: runUpInfraApply},
		{id: "provision", name: "Configuration provisioning", run: runUpProvision},
		{id: "health-check", name: "Lab health check", run: runUpHealthCheck},
	}

	if upFromStep != "" {
		idx := -1
		for i, s := range steps {
			if s.id == upFromStep {
				idx = i
				break
			}
		}
		if idx < 0 {
			valid := make([]string, len(steps))
			for i, s := range steps {
				valid[i] = s.id
			}
			return fmt.Errorf("--from %q is not a valid step (one of: %s)", upFromStep, strings.Join(valid, ", "))
		}
		steps = steps[idx:]
	} else if upSkipDoctor {
		steps = steps[1:]
	}

	total := len(steps)
	start := time.Now()
	for i, step := range steps {
		printUpHeader(i+1, total, step.name)
		if err := step.run(cmd, args); err != nil {
			fmt.Println()
			color.Red("✗ %s failed: %v", step.name, err)
			color.Yellow("  Resume with: dreadgoad up --from %s", step.id)
			return err
		}
	}

	fmt.Println()
	color.Green("✓ Lab is up. Total time: %s", time.Since(start).Round(time.Second))
	fmt.Println("Next: dreadgoad validate    # vulnerability checks")
	return nil
}

func printUpHeader(step, total int, name string) {
	line := strings.Repeat("━", 60)
	fmt.Println()
	color.Cyan(line)
	color.Cyan("▶ Step %d/%d  %s", step, total, name)
	color.Cyan(line)
}

func runUpDoctor(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	results := doctor.RunChecks(doctor.Options{
		InventoryPath: cfg.InventoryPath(),
		ProjectRoot:   cfg.ProjectRoot,
		Provider:      cfg.ResolvedProvider(),
		Ludus: doctor.LudusOptions{
			APIKey:      cfg.Ludus.APIKey,
			SSHHost:     cfg.Ludus.SSHTarget(),
			SSHUser:     cfg.Ludus.SSHUser,
			SSHKeyPath:  cfg.Ludus.SSHKeyPath,
			SSHPassword: cfg.Ludus.SSHPassword,
			SSHPort:     cfg.Ludus.SSHPort,
			ResolveAlias: cfg.Ludus.Host != "" &&
				cfg.Ludus.SSHUser == "" &&
				cfg.Ludus.SSHKeyPath == "" &&
				cfg.Ludus.SSHPassword == "" &&
				cfg.Ludus.SSHPort == 0,
		},
	})
	if failed := doctor.PrintResults(results); failed > 0 {
		return fmt.Errorf("%d pre-flight check(s) failed (re-run 'dreadgoad doctor' for details, or pass --skip-doctor to bypass)", failed)
	}
	return nil
}

// newUpInfraCommand builds the synthetic cobra.Command that `up` drives
// `infra apply` through, so the inner action sees only the flags we want
// (auto-approve=true, module/exclude pass-through) without conflating with
// the up command's own flag set.
//
// EVERY flag runInfraAction* reads must be registered here. A flag that is
// absent reads back as its zero value and the lookup error is discarded, so
// omitting one fails silently — see TestUpInfraCommandForwardsEveryFlag,
// which is what keeps this set in sync with the real command.
//
// On Azure the Bastion and controller modules are excluded from terragrunt
// unless DREADGOAD_ENABLE_AZURE_* is set, and step 3 (provision) reaches the
// Windows hosts only over Bastion → controller → SOCKS5 (see
// startAzureSOCKSTunnel). They are prerequisites of this pipeline rather than
// options, so `up` opts in on the operator's behalf; without them the run
// deploys every VM and then cannot reach any of them. Kali is a genuine extra
// and stays behind --with-kali.
func newUpInfraCommand(ctx context.Context, providerName string) *cobra.Command {
	needsTunnel := providerName == provider.NameAzure

	// Named `synth`, not `infraCmd`: the latter is the real `infra` command at
	// package scope, and shadowing it here made the two easy to confuse.
	synth := &cobra.Command{}
	synth.Flags().String("module", upInfraModule, "")
	synth.Flags().String("exclude", upInfraExclude, "")
	synth.Flags().Bool("auto-approve", true, "")
	synth.Flags().Bool("individual", false, "")
	synth.Flags().String("deployment", "", "")
	synth.Flags().Bool("with-bastion", needsTunnel, "")
	synth.Flags().Bool("with-controller", needsTunnel, "")
	synth.Flags().Bool("with-kali", upWithKali, "")
	synth.SetContext(ctx)
	return synth
}

// runUpInfraApply invokes `infra apply` with auto-approve.
func runUpInfraApply(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	return runInfraAction("apply")(newUpInfraCommand(cmd.Context(), cfg.ResolvedProvider()), args)
}

// runUpProvision calls runProvision via a synthetic command so up's --from
// (which is the step name) is not mistakenly read as the playbook-resume
// flag of the provision subcommand.
func runUpProvision(cmd *cobra.Command, args []string) error {
	provCmd := &cobra.Command{}
	provCmd.Flags().String("plays", "", "")
	provCmd.Flags().String("from", "", "")
	provCmd.Flags().String("limit", "", "")
	provCmd.Flags().Int("max-retries", 0, "")
	provCmd.Flags().Int("retry-delay", 0, "")
	if upPlays != "" {
		_ = provCmd.Flags().Set("plays", upPlays)
	}
	if upLimit != "" {
		_ = provCmd.Flags().Set("limit", upLimit)
	}
	if upMaxRetries > 0 {
		_ = provCmd.Flags().Set("max-retries", strconv.Itoa(upMaxRetries))
	}
	if upRetryDelay > 0 {
		_ = provCmd.Flags().Set("retry-delay", strconv.Itoa(upRetryDelay))
	}
	provCmd.SetContext(cmd.Context())
	return runProvision(provCmd, args)
}

func runUpHealthCheck(cmd *cobra.Command, args []string) error {
	return runHealthCheck(cmd, args)
}
