package cmd

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/doctor"
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
	upFromPlaybook string
	upInfraModule  string
	upInfraExclude string
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Deploy the lab end-to-end (doctor → infra → provision → health-check)",
	Long: `One-command lab bring-up. Runs the full pipeline in order:

  1. doctor        pre-flight tooling and connectivity checks
  2. infra apply   provision instances/range (auto-approved)
  3. provision     run the selected lab's Ansible playbooks
  4. health-check  run the selected lab's final service checks

Stops on the first failing step and prints a resume hint. Use --from <step>
to restart from a specific point. The recommended new-user flow is:

  dreadgoad init && dreadgoad up`,
	Example: `  dreadgoad up
  dreadgoad up --skip-doctor
  dreadgoad up --from provision
  dreadgoad up --from provision --from-playbook ad-data.yml
  dreadgoad up --limit dc01`,
	RunE: runUp,
}

func init() {
	rootCmd.AddCommand(upCmd)

	upCmd.Flags().BoolVar(&upSkipDoctor, "skip-doctor", false, "Skip the doctor pre-flight checks")
	upCmd.Flags().StringVar(&upFromStep, "from", "", "Resume from this step (doctor, infra, provision, health-check)")
	upCmd.Flags().StringVar(&upLimit, "limit", "", "Limit provisioning to specific hosts")
	upCmd.Flags().StringVar(&upPlays, "plays", "", "Comma-separated playbooks to run (default: all)")
	upCmd.Flags().IntVar(&upMaxRetries, "max-retries", 0, "Max retry attempts for provisioning (0 disables retries)")
	upCmd.Flags().IntVar(&upRetryDelay, "retry-delay", 0, "Delay between retries in seconds (0 disables delay)")
	upCmd.Flags().StringVar(&upFromPlaybook, "from-playbook", "", "Resume provisioning from this playbook onward")
	upCmd.Flags().StringVar(&upInfraModule, "module", "", "Target a specific infra module (default: all)")
	upCmd.Flags().StringVar(&upInfraExclude, "exclude", "", "Exclude infra modules (comma-separated)")
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
	if err := validateUpProvisionResume(steps, upPlays, upFromPlaybook); err != nil {
		return err
	}
	resumeOptions := currentUpResumeOptions(cmd)

	total := len(steps)
	start := time.Now()
	for i, step := range steps {
		printUpHeader(i+1, total, step.name)
		if err := step.run(cmd, args); err != nil {
			fmt.Println()
			color.Red("✗ %s failed: %v", step.name, err)
			color.Yellow("  Resume with: %s", upResumeCommand(step.id, err, resumeOptions))
			return err
		}
	}

	fmt.Println()
	color.Green("✓ Lab is up. Total time: %s", time.Since(start).Round(time.Second))
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	fmt.Println(upNextStep(cfg.ResolvedLab()))
	return nil
}

func upNextStep(labName string) string {
	if labName == "SCOPE-RANGE" {
		return "Next: dreadgoad provision --plays scope-kali.yml    # rerun service checks"
	}
	return "Next: dreadgoad validate    # vulnerability checks"
}

func validateUpProvisionResume(steps []upStep, plays, fromPlaybook string) error {
	if fromPlaybook == "" {
		return nil
	}
	if plays != "" {
		return fmt.Errorf("--from-playbook cannot be combined with --plays")
	}
	for _, step := range steps {
		if step.id == "provision" {
			return nil
		}
	}
	return fmt.Errorf("--from-playbook cannot be used when the provision step is skipped")
}

type upResumeOptions struct {
	plays        string
	fromPlaybook string
	limit        string
	infraModule  string
	infraExclude string
	retry        retryOverrides
}

func currentUpResumeOptions(cmd *cobra.Command) upResumeOptions {
	opts := upResumeOptions{
		plays:        upPlays,
		fromPlaybook: upFromPlaybook,
		limit:        upLimit,
		infraModule:  upInfraModule,
		infraExclude: upInfraExclude,
	}
	if cmd.Flags().Changed("max-retries") {
		value := upMaxRetries
		opts.retry.maxRetries = &value
	}
	if cmd.Flags().Changed("retry-delay") {
		value := upRetryDelay
		opts.retry.retryDelay = &value
	}
	return opts
}

func upResumeCommand(stepID string, err error, opts upResumeOptions) string {
	command := fmt.Sprintf("dreadgoad up --from %s", stepID)
	if stepID == "health-check" {
		return command
	}

	if stepID == "doctor" || stepID == "infra" {
		if opts.infraModule != "" {
			command += " --module " + shellQuoteResumeArg(opts.infraModule)
		}
		if opts.infraExclude != "" {
			command += " --exclude " + shellQuoteResumeArg(opts.infraExclude)
		}
	}

	var failure *provisionFailure
	switch {
	case stepID == "provision" && errors.As(err, &failure) && failure.Playbook != "":
		if opts.plays != "" {
			command += " --plays " + shellQuoteResumeArg(remainingPlaybookSelection(opts.plays, failure.Playbook))
		} else {
			command += " --from-playbook " + shellQuoteResumeArg(failure.Playbook)
		}
	case opts.plays != "":
		command += " --plays " + shellQuoteResumeArg(opts.plays)
	case opts.fromPlaybook != "":
		command += " --from-playbook " + shellQuoteResumeArg(opts.fromPlaybook)
	}
	if opts.limit != "" {
		command += " --limit " + shellQuoteResumeArg(opts.limit)
	}
	if opts.retry.maxRetries != nil {
		command += " --max-retries " + strconv.Itoa(*opts.retry.maxRetries)
	}
	if opts.retry.retryDelay != nil {
		command += " --retry-delay " + strconv.Itoa(*opts.retry.retryDelay)
	}
	return command
}

func remainingPlaybookSelection(selected, failed string) string {
	playbooks := strings.Split(selected, ",")
	for i, playbook := range playbooks {
		if playbook == failed {
			return strings.Join(playbooks[i:], ",")
		}
	}
	return selected
}

func shellQuoteResumeArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
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
		return upDoctorFailure(failed)
	}
	return nil
}

func upDoctorFailure(failed int) error {
	return fmt.Errorf("%d pre-flight check(s) failed; run 'dreadgoad doctor' for details, fix the reported issues, then retry 'dreadgoad up'", failed)
}

// runUpInfraApply invokes `infra apply` with auto-approve. We build a
// synthetic cobra.Command so the inner action sees only the flags we want
// (auto-approve=true, module/exclude pass-through) without conflating with
// the up command's own flag set.
func runUpInfraApply(cmd *cobra.Command, args []string) error {
	infraCmd := &cobra.Command{}
	infraCmd.Flags().String("module", upInfraModule, "")
	infraCmd.Flags().String("exclude", upInfraExclude, "")
	infraCmd.Flags().Bool("auto-approve", true, "")
	infraCmd.Flags().Bool("individual", false, "")
	infraCmd.Flags().String("deployment", "", "")
	infraCmd.SetContext(cmd.Context())
	return runInfraAction("apply")(infraCmd, args)
}

type upProvisionOptions struct {
	plays        string
	fromPlaybook string
	limit        string
	retry        retryOverrides
}

// newUpProvisionCommand builds the synthetic command that `up` uses to invoke
// provisioning. Every flag read by runProvision must be registered here so a
// missing flag cannot silently turn into its zero value. The structural test in
// up_test.go keeps this flag set aligned with the real provision command.
func newUpProvisionCommand(ctx context.Context, opts upProvisionOptions) (*cobra.Command, error) {
	provCmd := &cobra.Command{}
	provCmd.Flags().String("plays", "", "")
	provCmd.Flags().String("from", "", "")
	provCmd.Flags().String("limit", "", "")
	provCmd.Flags().Int("max-retries", 0, "")
	provCmd.Flags().Int("retry-delay", 0, "")
	provCmd.Flags().StringArray("extra-vars", nil, "")

	setFlag := func(name, value string) error {
		if err := provCmd.Flags().Set(name, value); err != nil {
			return fmt.Errorf("configure synthetic provision flag --%s: %w", name, err)
		}
		return nil
	}
	if opts.plays != "" {
		if err := setFlag("plays", opts.plays); err != nil {
			return nil, err
		}
	}
	if opts.fromPlaybook != "" {
		if err := setFlag("from", opts.fromPlaybook); err != nil {
			return nil, err
		}
	}
	if opts.limit != "" {
		if err := setFlag("limit", opts.limit); err != nil {
			return nil, err
		}
	}
	if opts.retry.maxRetries != nil {
		if err := setFlag("max-retries", strconv.Itoa(*opts.retry.maxRetries)); err != nil {
			return nil, err
		}
	}
	if opts.retry.retryDelay != nil {
		if err := setFlag("retry-delay", strconv.Itoa(*opts.retry.retryDelay)); err != nil {
			return nil, err
		}
	}
	provCmd.SetContext(ctx)
	return provCmd, nil
}

// runUpProvision calls runProvision via a synthetic command so up's --from
// (which is the step name) is not mistakenly read as the playbook-resume
// flag of the provision subcommand.
func runUpProvision(cmd *cobra.Command, args []string) error {
	retry, err := retryOverridesFromFlags(cmd)
	if err != nil {
		return err
	}
	provCmd, err := newUpProvisionCommand(cmd.Context(), upProvisionOptions{
		plays:        upPlays,
		fromPlaybook: upFromPlaybook,
		limit:        upLimit,
		retry:        retry,
	})
	if err != nil {
		return err
	}
	return runProvision(provCmd, args)
}

func runUpHealthCheck(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	if cfg.ResolvedLab() == "SCOPE-RANGE" {
		fmt.Println("SCOPE-RANGE foundational and versioned seed checks passed during provisioning.")
		return nil
	}
	return runHealthCheck(cmd, args)
}
