package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/scoreboard"
	"github.com/spf13/cobra"
)

var scoreCmd = &cobra.Command{
	Use:   "score",
	Short: "Score an agent's report against the answer key",
	Long: `Scores an agent's JSONL report against the answer key and outputs
a JSON result. Supports live verification via --live-verify to test
credentials against the running GOAD lab.

Use 'score generate-key' to build the answer key from a lab config.`,
	RunE: runScore,
}

var scoreGenerateKeyCmd = &cobra.Command{
	Use:   "generate-key",
	Short: "Generate the answer key from a GOAD config.json",
	RunE:  runScoreGenerateKey,
}

func init() {
	rootCmd.AddCommand(scoreCmd)
	scoreCmd.AddCommand(scoreGenerateKeyCmd)

	scoreCmd.Flags().String("report", "", "Path to the agent's JSONL report file (required)")
	scoreCmd.Flags().String("answer-key", "", "Path to answer_key.json (default: scoreboard/answer_key.json)")
	scoreCmd.Flags().String("output", "", "Write JSON result to file instead of stdout")
	scoreCmd.Flags().Bool("live-verify", false, "Enable live verification via the attack box")
	scoreCmd.Flags().String("attack-box", "", "Instance ID (AWS) or resource ID (Azure) of the Kali attack box")
	scoreCmd.Flags().String("region", "", "AWS region for SSM")
	scoreCmd.Flags().String("profile", "", "AWS named profile")

	scoreGenerateKeyCmd.Flags().String("config", "", "Path to GOAD config.json (default: ad/GOAD/data/config.json)")
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

	answerKeyPath, _ := cmd.Flags().GetString("answer-key")
	if answerKeyPath == "" {
		answerKeyPath = filepath.Join(cfg.ProjectRoot, "scoreboard", "answer_key.json")
	}

	ak, err := scoreboard.LoadAnswerKey(answerKeyPath)
	if err != nil {
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

	outputPath, _ := cmd.Flags().GetString("output")
	if outputPath != "" {
		if err := os.WriteFile(outputPath, data, 0o644); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Score result written to %s\n", outputPath)
		return err
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return err
}

func buildShellRunner(ctx context.Context, cmd *cobra.Command, cfg *config.Config) (scoreboard.ShellRunner, error) {
	attackBox, _ := cmd.Flags().GetString("attack-box")
	if attackBox == "" {
		return nil, fmt.Errorf("--attack-box is required with --live-verify")
	}

	if strings.HasPrefix(attackBox, "/subscriptions/") {
		return &scoreboard.BastionShellRunner{VMResource: attackBox}, nil
	}

	// Default: AWS SSM.
	region, _ := cmd.Flags().GetString("region")
	if region == "" {
		region = cfg.Region
	}
	profile, _ := cmd.Flags().GetString("profile")
	return scoreboard.NewSSMShellRunner(ctx, attackBox, region, profile)
}

func runScoreGenerateKey(cmd *cobra.Command, _ []string) error {
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
