package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/scoreboard"
	"github.com/spf13/cobra"
)

var scoreboardCmd = &cobra.Command{
	Use:   "scoreboard",
	Short: "Live scoreboard for GOAD engagements",
	Long: `Tracks an agent's progress against a GOAD lab: parses the lab config
into a checklist of objectives ("answer key"), polls a JSONL report file
locally or from an EC2 instance via SSM, and verifies findings against the
key. Run 'scoreboard generate-key' first to build the answer key.`,
}

var scoreboardGenerateKeyCmd = &cobra.Command{
	Use:   "generate-key",
	Short: "Generate the answer key from a GOAD config.json",
	RunE:  runScoreboardGenerateKey,
}

var scoreboardRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the live scoreboard against an agent's report",
	Long: `Polls the agent's JSONL report and renders a live verification
TUI. Use --transport=local to read a local file, or --transport=ssm with
--instance-id to read /tmp/report.jsonl from a remote EC2 instance.`,
	RunE: runScoreboardRun,
}

var scoreboardDemoCmd = &cobra.Command{
	Use:   "demo",
	Short: "Render a sample scoreboard with mock findings",
	RunE:  runScoreboardDemo,
}

func init() {
	rootCmd.AddCommand(scoreboardCmd)
	scoreboardCmd.AddCommand(scoreboardGenerateKeyCmd)
	scoreboardCmd.AddCommand(scoreboardRunCmd)
	scoreboardCmd.AddCommand(scoreboardDemoCmd)

	scoreboardGenerateKeyCmd.Flags().String("config", "", "Path to GOAD config.json (default: ad/GOAD/data/config.json)")
	scoreboardGenerateKeyCmd.Flags().String("output", "", "Output path for answer_key.json (default: scoreboard/answer_key.json)")

	scoreboardDemoCmd.Flags().String("config", "", "Path to GOAD config.json (default: ad/GOAD/data/config.json)")

	scoreboardRunCmd.Flags().String("transport", "local", "Transport: local, ssm, or ares")
	scoreboardRunCmd.Flags().String("report", "/tmp/report.jsonl", "Path to the agent's report file (on the target, for local/ssm)")
	scoreboardRunCmd.Flags().String("answer-key", "", "Path to answer_key.json (default: scoreboard/answer_key.json)")
	scoreboardRunCmd.Flags().String("instance-id", "", "EC2 instance ID (required for --transport=ssm or --transport=ares)")
	scoreboardRunCmd.Flags().String("ssm-region", "", "AWS region for SSM (defaults to --region or SDK default)")
	scoreboardRunCmd.Flags().String("ares-binary", "", "Path to the ares binary on the target (default: /usr/local/bin/ares)")
	scoreboardRunCmd.Flags().Duration("interval", 3*time.Second, "Poll interval (e.g. 3s, 1500ms)")
	scoreboardRunCmd.Flags().Bool("restart", false, "Delete the existing report file on the target before starting (no-op for --transport=ares)")
	scoreboardRunCmd.Flags().Bool("once", false, "Fetch + verify once, print the static board, exit (no TUI)")
}

func runScoreboardGenerateKey(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "data", "config.json")
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

func runScoreboardRun(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	answerKeyPath, _ := cmd.Flags().GetString("answer-key")
	if answerKeyPath == "" {
		answerKeyPath = filepath.Join(cfg.ProjectRoot, "scoreboard", "answer_key.json")
	}
	ak, err := scoreboard.LoadAnswerKey(answerKeyPath)
	if err != nil {
		return fmt.Errorf("%w (run 'dreadgoad scoreboard generate-key' first)", err)
	}

	ctx := cmd.Context()
	t, displayPath, err := buildTransport(ctx, cmd, cfg)
	if err != nil {
		return err
	}

	if restart, _ := cmd.Flags().GetBool("restart"); restart {
		if err := runRestart(ctx, cmd, t); err != nil {
			return err
		}
	}

	if once, _ := cmd.Flags().GetBool("once"); once {
		return runOnce(ctx, cmd, t, ak, displayPath)
	}

	interval, _ := cmd.Flags().GetDuration("interval")
	return scoreboard.RunTUI(ctx, scoreboard.TUIConfig{
		Transport:    t,
		AnswerKey:    ak,
		PollInterval: interval,
		ReportPath:   displayPath,
	})
}

func buildTransport(ctx context.Context, cmd *cobra.Command, cfg *config.Config) (scoreboard.Transport, string, error) {
	transport, _ := cmd.Flags().GetString("transport")
	reportPath, _ := cmd.Flags().GetString("report")
	instanceID, _ := cmd.Flags().GetString("instance-id")
	ssmRegion, _ := cmd.Flags().GetString("ssm-region")
	aresBinary, _ := cmd.Flags().GetString("ares-binary")

	switch transport {
	case "local":
		return &scoreboard.LocalTransport{Path: reportPath}, reportPath, nil
	case "ssm":
		if instanceID == "" {
			return nil, "", fmt.Errorf("--instance-id is required for --transport=ssm")
		}
		region := ssmRegion
		if region == "" {
			region = cfg.Region
		}
		st, err := scoreboard.NewSSMTransport(ctx, instanceID, reportPath, region)
		if err != nil {
			return nil, "", err
		}
		return st, fmt.Sprintf("...%s:%s", shortInstanceID(instanceID), reportPath), nil
	case "ares":
		if instanceID == "" {
			return nil, "", fmt.Errorf("--instance-id is required for --transport=ares")
		}
		region := ssmRegion
		if region == "" {
			region = cfg.Region
		}
		at, err := scoreboard.NewAresTransport(ctx, instanceID, aresBinary, region)
		if err != nil {
			return nil, "", err
		}
		return at, fmt.Sprintf("ares@...%s", shortInstanceID(instanceID)), nil
	default:
		return nil, "", fmt.Errorf("unknown transport: %s (expected local, ssm, or ares)", transport)
	}
}

func shortInstanceID(id string) string {
	if len(id) > 5 {
		return id[len(id)-5:]
	}
	return id
}

func runRestart(ctx context.Context, cmd *cobra.Command, t scoreboard.Transport) error {
	if _, err := fmt.Fprintln(cmd.OutOrStdout(), "Removing existing report file..."); err != nil {
		return err
	}
	ok, err := t.DeleteReport(ctx)
	switch {
	case err != nil:
		_, werr := fmt.Fprintf(cmd.ErrOrStderr(), "Warning: could not delete report file: %v\n", err)
		return werr
	case ok:
		_, werr := fmt.Fprintln(cmd.OutOrStdout(), "Report file deleted.")
		return werr
	default:
		_, werr := fmt.Fprintln(cmd.OutOrStdout(), "No existing report file found.")
		return werr
	}
}

func runOnce(ctx context.Context, cmd *cobra.Command, t scoreboard.Transport, ak *scoreboard.AnswerKey, displayPath string) error {
	raw, err := t.FetchReport(ctx)
	if err != nil {
		if errors.Is(err, scoreboard.ErrNoReport) {
			_, werr := fmt.Fprintf(cmd.ErrOrStderr(), "No report at %s yet.\n", displayPath)
			return werr
		}
		return err
	}
	report := scoreboard.ParseReport(raw)
	status := scoreboard.VerifyReport(report, ak)
	start, _ := time.Parse(time.RFC3339, report.StartTime)
	_, err = fmt.Fprintln(cmd.OutOrStdout(), scoreboard.RenderStatic(status, ak, report.AgentID, start))
	return err
}

func runScoreboardDemo(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	configPath, _ := cmd.Flags().GetString("config")
	if configPath == "" {
		configPath = filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "data", "config.json")
	}
	ak, err := scoreboard.GenerateAnswerKey(configPath)
	if err != nil {
		return err
	}
	report, start := scoreboard.BuildDemoReport()
	status := scoreboard.VerifyReport(report, ak)
	_, err = fmt.Fprintln(cmd.OutOrStdout(), scoreboard.RenderStatic(status, ak, report.AgentID, start))
	return err
}
