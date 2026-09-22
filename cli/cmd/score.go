package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/azure"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/dreadnode/dreadgoad/internal/rangecommand"
	"github.com/dreadnode/dreadgoad/internal/scoreboard"
	"github.com/spf13/cobra"
)

var scoreCmd = &cobra.Command{
	Use:   "score",
	Short: "Score an engagement report for the selected range",
	Long: `Invokes the selected range's scorer and outputs a JSON result.

The built-in Active Directory scorer evaluates a JSONL report against an
answer key and can use --live-verify to test credentials against the live lab.

When the selected range uses that built-in scorer, use 'score generate-key' to
build its answer key from the resolved lab config.`,
	RunE: runScore,
}

var scoreGenerateKeyCmd = &cobra.Command{
	Use:   "generate-key",
	Short: "Generate an answer key for the built-in Active Directory scorer",
	Long: `Generate an answer key for the selected range's built-in Active Directory
scorer. Ranges with executable-owned scoring use their range-owned handler and,
when setup artifacts are required, their declared initializer instead.`,
	RunE: runScoreGenerateKey,
}

func init() {
	rootCmd.AddCommand(scoreCmd)
	scoreCmd.AddCommand(scoreGenerateKeyCmd)

	scoreCmd.Flags().String("report", "", "Path to the agent's JSONL report file (required)")
	scoreCmd.Flags().String("answer-key", "", "Path to answer_key.json (default: <range-artifacts>/answer_key.json when set, otherwise scoreboard/answer_key.json)")
	scoreCmd.Flags().String("output", "", "Write JSON result to file instead of stdout")
	scoreCmd.Flags().String("range-artifacts", "", "Private session artifact directory for the selected range scorer")
	scoreCmd.Flags().Bool("live-verify", false, "Enable live verification via the attack box")
	scoreCmd.Flags().String("attack-box", "", "Instance ID (AWS) or resource ID (Azure) of the Kali attack box")
	scoreCmd.Flags().String("region", "", "AWS region for SSM")
	scoreCmd.Flags().String("profile", "", "AWS named profile")

	// Azure-specific flags for live verification (optional overrides;
	// all are auto-discovered from the environment when omitted).
	scoreCmd.Flags().String("ssh-key", "", "Path to SSH private key for the Kali VM (Azure; auto-discovered if omitted)")
	scoreCmd.Flags().String("ssh-user", "kali", "SSH username for the Kali VM (Azure)")

	scoreGenerateKeyCmd.Flags().String("config", "", "Path to GOAD config.json (default: the active environment's resolved lab config)")
	scoreGenerateKeyCmd.Flags().String("output", "", "Output path for answer_key.json (default: scoreboard/answer_key.json)")
}

func runScore(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	reportPath, _ := cmd.Flags().GetString("report")
	if reportPath == "" {
		return fmt.Errorf("--report is required")
	}

	capability, err := rangecommand.Require(cfg, "score")
	if err != nil {
		return err
	}
	if capability.HandlerType == rangecommand.HandlerExecutable {
		return runExternalScore(cmd, cfg, capability, reportPath)
	}

	answerKeyPath := resolveScoreAnswerKeyPath(cmd, cfg)

	ak, err := scoreboard.LoadAnswerKey(answerKeyPath)
	if err != nil {
		explicitAnswerKey, _ := cmd.Flags().GetString("answer-key")
		rangeArtifacts, _ := cmd.Flags().GetString("range-artifacts")
		if explicitAnswerKey == "" && rangeArtifacts != "" {
			return fmt.Errorf("%w (session answer key is unavailable; rerun range session initialization)", err)
		}
		return fmt.Errorf("%w (run 'dreadgoad score generate-key' first)", err)
	}

	raw, err := os.ReadFile(reportPath)
	if err != nil {
		return fmt.Errorf("read report: %w", err)
	}
	report := scoreboard.ParseReport(string(raw))

	ctx := cmd.Context()
	var lv *scoreboard.LiveVerifier
	if live, _ := cmd.Flags().GetBool("live-verify"); live {
		runner, err := buildShellRunner(ctx, cmd, cfg)
		if err != nil {
			return fmt.Errorf("live verification setup: %w", err)
		}
		lv = scoreboard.NewLiveVerifier(runner)
	}

	result := scoreboard.ScoreReport(ctx, report, ak, lv)

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	out := cmd.OutOrStdout()

	// Human-readable summary on stderr so stdout stays valid JSON.
	stderr := cmd.ErrOrStderr()
	_, _ = fmt.Fprintf(stderr, "\n  Score: %s (%s)\n\n", result.AgentID, result.Mode)
	total, achieved := 0, 0
	for _, g := range []string{"credentials", "hosts", "domains"} {
		s := result.Summary[g]
		if s == nil {
			continue
		}
		_, _ = fmt.Fprintf(stderr, "    %-14s %d / %d\n", g, s.Achieved, s.Total)
		total += s.Total
		achieved += s.Achieved
	}
	_, _ = fmt.Fprintf(stderr, "    %-14s %d / %d\n", "TOTAL", achieved, total)
	if len(result.FailedChecks) > 0 {
		_, _ = fmt.Fprintf(stderr, "\n    %d failed check(s) — see JSON output for details\n", len(result.FailedChecks))
	}
	_, _ = fmt.Fprintln(stderr)

	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath != "" {
		if err := os.WriteFile(outputPath, data, 0o644); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		_, err = fmt.Fprintf(cmd.ErrOrStderr(), "JSON result written to %s\n", outputPath)
		return err
	}

	_, err = fmt.Fprintln(out, string(data))
	return err
}

func runExternalScore(cmd *cobra.Command, cfg *config.Config, capability rangecommand.Capability, reportPath string) error {
	absoluteReportPath, err := filepath.Abs(reportPath)
	if err != nil {
		return fmt.Errorf("resolve report path: %w", err)
	}
	options := map[string]any{"report": absoluteReportPath}
	for _, flag := range []string{"answer-key", "output", "range-artifacts", "attack-box", "region", "profile", "ssh-key", "ssh-user"} {
		value, err := cmd.Flags().GetString(flag)
		if err != nil {
			return err
		}
		if value != "" {
			options[strings.ReplaceAll(flag, "-", "_")] = value
		}
	}
	live, _ := cmd.Flags().GetBool("live-verify")
	options["live_verify"] = live
	return rangecommand.Execute(cmd.Context(), cfg, capability, rangecommand.Request{
		Options: options,
	}, cmd.OutOrStdout(), cmd.ErrOrStderr())
}

func buildShellRunner(ctx context.Context, cmd *cobra.Command, cfg *config.Config) (scoreboard.ShellRunner, error) {
	attackBox, _ := cmd.Flags().GetString("attack-box")

	// Explicit Azure resource ID — skip auto-discovery.
	if strings.HasPrefix(attackBox, "/subscriptions/") {
		return buildAzureRunner(ctx, cmd, cfg, attackBox)
	}

	// Explicit AWS instance ID.
	if attackBox != "" {
		if !strings.HasPrefix(attackBox, "i-") {
			return nil, fmt.Errorf("unrecognized --attack-box format %q (expected AWS instance ID like i-0abc123 or Azure resource ID starting with /subscriptions/)", attackBox)
		}
		return buildAWSRunner(ctx, cmd, cfg, attackBox)
	}

	// No --attack-box: auto-detect provider and locate the tagged attack box.
	if cfg.ResolvedProvider() == provider.NameAzure {
		return buildAzureRunner(ctx, cmd, cfg, "")
	}
	if cfg.ResolvedProvider() == provider.NameAWS {
		region, profile, err := resolveAWSConnectionConfig(cmd, cfg)
		if err != nil {
			return nil, err
		}
		rangeTag, err := cfg.ProviderRangeTag()
		if err != nil {
			return nil, err
		}
		prov, err := provider.New(ctx, provider.NameAWS, provider.ConstructorOpts{
			Region:     region,
			AWSProfile: profile,
			RangeTag:   rangeTag,
		})
		if err != nil {
			return nil, fmt.Errorf("create AWS provider: %w", err)
		}
		instances, err := prov.DiscoverInstances(ctx, cfg.Env)
		if err != nil {
			return nil, fmt.Errorf("discover AWS instances: %w", err)
		}
		kali := provider.FindInstanceByRole(instances, "AttackBox")
		if kali == nil {
			return nil, fmt.Errorf("no running Kali attack box (Role=AttackBox) found for env=%s; pass --attack-box to override", cfg.Env)
		}
		return scoreboard.NewSSMShellRunner(ctx, kali.ID, region, profile)
	}
	return nil, fmt.Errorf("--attack-box is required with --live-verify for provider %s", cfg.ResolvedProvider())
}

func buildAWSRunner(ctx context.Context, cmd *cobra.Command, cfg *config.Config, instanceID string) (scoreboard.ShellRunner, error) {
	region, profile, err := resolveAWSConnectionConfig(cmd, cfg)
	if err != nil {
		return nil, err
	}
	return scoreboard.NewSSMShellRunner(ctx, instanceID, region, profile)
}

func resolveAWSConnectionConfig(cmd *cobra.Command, cfg *config.Config) (string, string, error) {
	region, _ := cmd.Flags().GetString("region")
	if region == "" {
		var err error
		region, err = cfg.ResolveRegion()
		if err != nil {
			return "", "", err
		}
	}
	profile, _ := cmd.Flags().GetString("profile")
	return region, profile, nil
}

func buildAzureRunner(ctx context.Context, cmd *cobra.Command, cfg *config.Config, vmResourceID string) (scoreboard.ShellRunner, error) {
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("create azure provider: %w", err)
	}
	ap, ok := prov.(*azure.AzureProvider)
	if !ok {
		return nil, fmt.Errorf("expected azure provider, got %s — pass -p azure when using an Azure resource ID for --attack-box", prov.Name())
	}
	client := ap.Client()

	if _, err := client.VerifyCredentials(ctx); err != nil {
		return nil, fmt.Errorf("azure credentials: %w", err)
	}

	// Discover Bastion.
	bastion, err := client.DiscoverBastion(ctx, cfg.Env)
	if err != nil {
		return nil, fmt.Errorf("discover bastion: %w", err)
	}
	if bastion == nil {
		return nil, fmt.Errorf("no Azure Bastion found for env=%s", cfg.Env)
	}

	// Discover Kali VM through the range-scoped provider if not explicitly provided.
	if vmResourceID == "" {
		instances, err := prov.DiscoverInstances(ctx, cfg.Env)
		if err != nil {
			return nil, fmt.Errorf("discover kali: %w", err)
		}
		kali := provider.FindInstanceByRole(instances, "AttackBox")
		if kali == nil {
			return nil, fmt.Errorf("no Kali attack box (Role=AttackBox) found for env=%s", cfg.Env)
		}
		vmResourceID = kali.ID

		// Auto-discover SSH key from Kali VM name.
		sshKey, _ := cmd.Flags().GetString("ssh-key")
		if sshKey == "" {
			sshKey = azure.KaliKeyPath(cfg.Env, kali.Name)
			if sshKey == "" {
				return nil, fmt.Errorf("could not find SSH key for Kali VM %s; use --ssh-key", kali.Name)
			}
		}
		sshUser, _ := cmd.Flags().GetString("ssh-user")
		return &scoreboard.BastionShellRunner{
			BastionName:   bastion.Name,
			ResourceGroup: bastion.ResourceGroup,
			VMResourceID:  vmResourceID,
			SSHKeyPath:    sshKey,
			Username:      sshUser,
		}, nil
	}

	// Explicit VM resource ID — still need SSH key.
	sshKey, _ := cmd.Flags().GetString("ssh-key")
	if sshKey == "" {
		return nil, fmt.Errorf("--ssh-key is required when using explicit --attack-box with Azure")
	}
	sshUser, _ := cmd.Flags().GetString("ssh-user")
	return &scoreboard.BastionShellRunner{
		BastionName:   bastion.Name,
		ResourceGroup: bastion.ResourceGroup,
		VMResourceID:  vmResourceID,
		SSHKeyPath:    sshKey,
		Username:      sshUser,
	}, nil
}

func runScoreGenerateKey(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	return runScoreGenerateKeyWithConfig(cmd, cfg)
}

func runScoreGenerateKeyWithConfig(cmd *cobra.Command, cfg *config.Config) error {
	if _, err := rangecommand.RequireBuiltinProfile(cfg, "score", rangecommand.ProfileActiveDir); err != nil {
		return fmt.Errorf("score generate-key is only available for the built-in active-directory scorer: %w", err)
	}

	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		// Resolve through the active environment so overlays and variant labs
		// are honored. Hardcoding ad/GOAD/data/config.json scores the base lab
		// no matter which --env is selected.
		resolved, err := cfg.ResolvedLabConfigPath()
		if err != nil {
			return err
		}
		configPath = resolved
	}
	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath == "" {
		outputPath = filepath.Join(cfg.ProjectRoot, "scoreboard", "answer_key.json")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(outputPath), err)
	}

	ak, err := scoreboard.GenerateAnswerKey(configPath)
	if err != nil {
		return err
	}
	if err := scoreboard.WriteAnswerKey(ak, outputPath); err != nil {
		return fmt.Errorf("write answer key: %w", err)
	}

	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "Generated answer key: %d objectives → %s\n", ak.TotalObjectives, outputPath); err != nil {
		return err
	}
	keys := make([]string, 0, len(ak.Groups))
	for g := range ak.Groups {
		keys = append(keys, g)
	}
	sort.Strings(keys)
	for _, g := range keys {
		if _, err := fmt.Fprintf(out, "  %s: %d\n", g, ak.Groups[g]); err != nil {
			return err
		}
	}
	return nil
}

func resolveScoreAnswerKeyPath(cmd *cobra.Command, cfg *config.Config) string {
	if path, _ := cmd.Flags().GetString("answer-key"); path != "" {
		return path
	}
	if artifacts, _ := cmd.Flags().GetString("range-artifacts"); artifacts != "" {
		return filepath.Join(artifacts, "answer_key.json")
	}
	return filepath.Join(cfg.ProjectRoot, "scoreboard", "answer_key.json")
}
