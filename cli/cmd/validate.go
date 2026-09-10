package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/dreadnode/dreadgoad/internal/validate"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the selected lab",
	Long: `Validates the deployed state of the selected lab.

For GOAD labs, checks run against live instances to confirm that the intended
vulnerability configurations are present. For SCOPE-RANGE, validation checks
the Azure topology and the expected users, services, applications, databases,
storage, seeded data, and cross-host workflows on all six Linux hosts.

GOAD validation checks credentials, Kerberos, SMB, delegation, MSSQL (linked servers, impersonation,
xp_cmdshell, sysadmins), ADCS (templates), ACLs, trusts, SID filtering, scheduled tasks,
LLMNR/NBT-NS, GPO abuse, gMSA, LAPS, and services.`,
	Example: `  dreadgoad validate
  dreadgoad validate --env staging --verbose
  dreadgoad validate --output /tmp/results.json
  dreadgoad validate --no-fail
  dreadgoad validate --quick
  dreadgoad validate --plain        # disable the live dashboard
  dreadgoad validate --poll 5m      # rerun every 5 minutes (minimum 1m)
  dreadgoad validate --poll never   # one-shot (default)`,
	RunE: runValidate,
}

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().String("output", "", "JSON report output path")
	validateCmd.Flags().Bool("verbose", false, "Enable verbose output")
	validateCmd.Flags().Bool("no-fail", false, "Don't exit with error on failed checks")
	validateCmd.Flags().Bool("quick", false, "Run critical validation checks only")
	validateCmd.Flags().Bool("plain", false, "Disable the live dashboard; stream results to stdout")
	validateCmd.Flags().Bool("json", false, "Output machine-readable JSON (per-check results + report)")
	validateCmd.Flags().String("poll", "never", "Re-run cadence for the live dashboard (e.g. 1m, 5m, or 'never'; minimum 1m)")

	if err := viper.BindPFlag("validate.poll", validateCmd.Flags().Lookup("poll")); err != nil {
		panic(fmt.Sprintf("failed to bind validate.poll: %v", err))
	}
}

// minPollInterval is the floor for --poll. Shorter cadences don't give the
// validation pass enough time to finish before the next iteration kicks in,
// leaving the dashboard perpetually mid-run.
const minPollInterval = time.Minute

// parsePollInterval interprets the --poll / validate.poll setting. The string
// "never" (case-insensitive) plus other off-style values disable polling and
// return 0; otherwise it must parse as a Go duration of at least
// minPollInterval.
func parsePollInterval(s string) (time.Duration, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "never", "off", "no", "false", "0", "0s":
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid poll interval %q: %w (use a Go duration like 1m/5m, or 'never')", s, err)
	}
	if d < minPollInterval {
		return 0, fmt.Errorf("poll interval must be at least %s: %s", minPollInterval, s)
	}
	return d, nil
}

type validateOpts struct {
	verbose      bool
	outputPath   string
	noFail       bool
	quick        bool
	plain        bool
	pollInterval time.Duration
	json         bool
}

func validateOptsFromFlags(cmd *cobra.Command) (validateOpts, error) {
	opts := validateOpts{}
	opts.verbose, _ = cmd.Flags().GetBool("verbose")
	opts.outputPath, _ = cmd.Flags().GetString("output")
	opts.noFail, _ = cmd.Flags().GetBool("no-fail")
	opts.quick, _ = cmd.Flags().GetBool("quick")
	opts.plain, _ = cmd.Flags().GetBool("plain")
	opts.json, _ = cmd.Flags().GetBool("json")

	d, err := parsePollInterval(viper.GetString("validate.poll"))
	if err != nil {
		return opts, err
	}
	opts.pollInterval = d
	if opts.json && opts.pollInterval > 0 {
		return opts, fmt.Errorf("--json cannot be combined with --poll")
	}
	return opts, nil
}

// makeRunChecks wraps the validator's check entry-points and any provider drain
// in a single function suitable for TUIConfig.Run / one-shot invocation.
func makeRunChecks(v *validate.Validator, p provider.Provider, quick bool) func(context.Context) {
	return func(c context.Context) {
		if quick {
			v.RunQuickChecks(c)
		} else {
			v.RunAllChecks(c)
		}
		// Wait for any provider-side cleanup (e.g. Azure Run Command DELETEs)
		// to finish so orphan subresources don't accumulate across runs.
		if d, ok := p.(provider.Drainer); ok {
			d.Drain()
		}
	}
}

func runValidate(cmd *cobra.Command, args []string) error {
	// Signal-aware context from the root: Ctrl+C/SIGTERM cancels ctx so this
	// function unwinds and the deferred Drain below runs (tearing down the
	// Bastion tunnel) instead of the process dying with the tunnel orphaned.
	ctx := cmd.Context()

	opts, err := validateOptsFromFlags(cmd)
	if err != nil {
		return err
	}
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	return inspectorFor(cfg).validate(ctx, cmd, cfg, opts)
}

func runGOADValidate(
	ctx context.Context, _ *cobra.Command, _ *config.Config, opts validateOpts,
) error {

	if !opts.json {
		fmt.Println("==========================================")
		fmt.Println("GOAD Vulnerability Validation")
		fmt.Println("==========================================")
	}

	infra, err := requireInfra(ctx)
	if err != nil {
		return err
	}

	// Guarantee provider teardown (Bastion tunnel + cached clients) on every
	// exit path — normal return, error, or interrupt. Drain is idempotent, so
	// the terminal Drain inside makeRunChecks on the happy path is harmless.
	if d, ok := infra.Provider.(provider.Drainer); ok {
		defer d.Drain()
	}

	useTUI := !opts.json && !opts.plain && term.IsTerminal(int(os.Stdout.Fd()))
	if opts.pollInterval > 0 && !useTUI {
		fmt.Fprintf(os.Stderr, "Warning: --poll is ignored without the live dashboard (TTY/--plain)\n")
		opts.pollInterval = 0
	}

	if !opts.json {
		fmt.Printf("Environment: %s\n", infra.Env)
		fmt.Printf("Region: %s\n", infra.Region)
	}

	v := validate.NewValidator(infra.Provider, infra.Env, opts.verbose, slog.Default(), infra.Lab)
	if opts.json {
		v.SetSilent(true)
		encoder := json.NewEncoder(os.Stdout)
		var encoderMu sync.Mutex
		v.SetOnResult(func(check validate.Result) {
			encoderMu.Lock()
			defer encoderMu.Unlock()
			_ = encoder.Encode(check)
		})
	}

	if err := v.DiscoverHosts(ctx); err != nil {
		return fmt.Errorf("discover hosts: %w", err)
	}

	runChecks := makeRunChecks(v, infra.Provider, opts.quick)
	runStart := time.Now()
	if useTUI {
		if err := validate.RunTUI(ctx, validate.TUIConfig{
			Validator:    v,
			Env:          infra.Env,
			Region:       infra.Region,
			Run:          runChecks,
			PollInterval: opts.pollInterval,
		}); err != nil {
			return err
		}
	} else {
		runChecks(ctx)
	}

	report := v.GetReport()
	outputPath := opts.outputPath
	if outputPath == "" && !opts.json {
		outputPath = fmt.Sprintf("/tmp/goad-validation-%s.json", time.Now().Format("20060102-150405"))
	}
	if outputPath != "" {
		if err := v.SaveReport(outputPath); err != nil {
			fmt.Fprintf(validationWarningWriter(opts.json), "Warning: could not save report: %v\n", err)
		}
	}

	if opts.json {
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			return fmt.Errorf("encode validation report: %w", err)
		}
	} else {
		fmt.Println()
		fmt.Println(validate.RenderSummaryPanel(report, infra.Env, infra.Region, time.Since(runStart), terminalWidth()))
		fmt.Printf("\nResults saved to: %s\n", outputPath)
	}

	if !opts.noFail && report.Failed > 0 {
		return fmt.Errorf("validation failed with %d errors", report.Failed)
	}
	return nil
}

func validationWarningWriter(jsonOut bool) *os.File {
	if jsonOut {
		return os.Stderr
	}
	return os.Stdout
}

func scopeRangeValidatorArgs(cfg *config.Config, opts validateOpts) ([]string, error) {
	if opts.pollInterval > 0 {
		return nil, fmt.Errorf("--poll is not supported for SCOPE-RANGE validation")
	}

	script := filepath.Join(cfg.ProjectRoot, "scripts", "validate-scope-range-live.py")
	args := []string{script, "--env", cfg.Env}
	if opts.outputPath != "" {
		args = append(args, "--output", opts.outputPath)
	}
	if opts.quick {
		args = append(args, "--quick")
	}
	if opts.verbose {
		args = append(args, "--verbose")
	}
	if opts.noFail {
		args = append(args, "--no-fail")
	}
	if opts.json {
		args = append(args, "--json")
	}
	return args, nil
}

func runScopeRangeValidate(ctx context.Context, cfg *config.Config, opts validateOpts) error {
	args, err := scopeRangeValidatorArgs(cfg, opts)
	if err != nil {
		return err
	}
	return runScopeRangeInspector(ctx, args, "validation")
}

func terminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 120
}
