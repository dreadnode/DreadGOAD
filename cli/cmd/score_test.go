package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/rangecommand"
	"github.com/spf13/cobra"
)

func TestResolveAWSConnectionConfigUsesEnvironmentRegionAndProfile(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("region", "", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("profile", "lab"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Env:    "staging",
		Region: "us-east-1",
		Environments: map[string]config.EnvironmentConfig{
			"staging": {Region: "us-west-1"},
		},
	}

	region, profile, err := resolveAWSConnectionConfig(cmd, cfg)
	if err != nil {
		t.Fatalf("resolveAWSConnectionConfig() error = %v", err)
	}
	if region != "us-west-1" || profile != "lab" {
		t.Fatalf("resolveAWSConnectionConfig() = (%q, %q), want (%q, %q)", region, profile, "us-west-1", "lab")
	}
}

func TestResolveAWSConnectionConfigPrefersFlagRegion(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("region", "", "")
	cmd.Flags().String("profile", "", "")
	if err := cmd.Flags().Set("region", "eu-west-1"); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Env: "staging",
		Environments: map[string]config.EnvironmentConfig{
			"staging": {Region: "us-west-1"},
		},
	}

	region, _, err := resolveAWSConnectionConfig(cmd, cfg)
	if err != nil {
		t.Fatalf("resolveAWSConnectionConfig() error = %v", err)
	}
	if region != "eu-west-1" {
		t.Fatalf("resolveAWSConnectionConfig() region = %q, want eu-west-1", region)
	}
}

func TestScoreGenerateKeyAllowsBuiltinActiveDirectoryScorer(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(t.TempDir(), "answer_key.json")
	command, stdout := scoreGenerateKeyTestCommand(
		t,
		filepath.Join(repositoryRoot, "ad", "GOAD", "data", "config.json"),
		outputPath,
	)

	if err := runScoreGenerateKeyWithConfig(command, &config.Config{
		ProjectRoot: repositoryRoot,
		Lab:         "GOAD",
		Env:         "test",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("generated answer key: %v", err)
	}
	if !strings.Contains(stdout.String(), "Generated answer key:") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestScoreGenerateKeyRejectsDisabledScoringWithoutWriting(t *testing.T) {
	root := t.TempDir()
	writeScoreManifest(t, root, `schema_version: 1
kind: active-directory
commands:
  score:
    enabled: false
`)
	outputPath := filepath.Join(root, "output", "answer_key.json")
	command, _ := scoreGenerateKeyTestCommand(t, filepath.Join(root, "missing.json"), outputPath)

	err := runScoreGenerateKeyWithConfig(command, &config.Config{
		ProjectRoot: root,
		Lab:         "TEST",
		Env:         "test",
	})
	var unsupported *rangecommand.UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Command != "score" {
		t.Fatalf("error = %v, want unsupported score", err)
	}
	assertOutputNotCreated(t, outputPath)
}

func TestScoreGenerateKeyRejectsExecutableScoringWithoutWriting(t *testing.T) {
	root := t.TempDir()
	writeScoreManifest(t, root, `schema_version: 1
kind: active-directory
commands:
  score:
    protocol: score/v1
    handler:
      type: executable
      path: commands/score
`)
	handler := filepath.Join(root, "ad", "TEST", "commands", "score")
	if err := os.MkdirAll(filepath.Dir(handler), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handler, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(root, "output", "answer_key.json")
	command, _ := scoreGenerateKeyTestCommand(t, filepath.Join(root, "missing.json"), outputPath)

	err := runScoreGenerateKeyWithConfig(command, &config.Config{
		ProjectRoot: root,
		Lab:         "TEST",
		Env:         "test",
	})
	if err == nil || !strings.Contains(err.Error(), "only available for the built-in active-directory scorer") {
		t.Fatalf("error = %v", err)
	}
	assertOutputNotCreated(t, outputPath)
}

func TestExternalScoreResolvesRelativeReportFromCallerDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	root := t.TempDir()
	writeScoreManifest(t, root, `schema_version: 1
kind: active-directory
commands:
  score:
    protocol: score/v1
    handler:
      type: executable
      path: commands/score
`)
	handler := filepath.Join(root, "ad", "TEST", "commands", "score")
	if err := os.MkdirAll(filepath.Dir(handler), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
request=$(cat)
printf '%s\n' "$request" >&2
printf '%s\n' '{"schema":"score/v1","score":0,"maximum":0,"objectives":[]}'
`
	if err := os.WriteFile(handler, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	callerDir := t.TempDir()
	reportPath := filepath.Join(callerDir, "report.jsonl")
	if err := os.WriteFile(reportPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(callerDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })

	command := externalScoreTestCommand()
	command.SetContext(context.Background())
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	cfg := &config.Config{ProjectRoot: root, Lab: "TEST", Env: "test"}
	capability, err := rangecommand.Require(cfg, "score")
	if err != nil {
		t.Fatal(err)
	}
	if err := runExternalScore(command, cfg, capability, "report.jsonl"); err != nil {
		t.Fatal(err)
	}
	var request rangecommand.Request
	if err := json.Unmarshal(bytes.TrimSpace(stderr.Bytes()), &request); err != nil {
		t.Fatalf("request JSON: %v\n%s", err, stderr.String())
	}
	expectedReportPath, err := filepath.Abs("report.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if got := request.Options["report"]; got != expectedReportPath {
		t.Fatalf("report option = %v, want %q", got, expectedReportPath)
	}
}

func TestResolveScoreAnswerKeyPath(t *testing.T) {
	root := t.TempDir()
	artifacts := filepath.Join(root, "session", "artifacts")
	explicit := filepath.Join(root, "custom.json")
	for _, test := range []struct {
		name, answerKey, rangeArtifacts, want string
	}{
		{"direct CLI default", "", "", filepath.Join(root, "scoreboard", "answer_key.json")},
		{"console session", "", artifacts, filepath.Join(artifacts, "answer_key.json")},
		{"explicit override", explicit, artifacts, explicit},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := &cobra.Command{Use: "score"}
			command.Flags().String("answer-key", test.answerKey, "")
			command.Flags().String("range-artifacts", test.rangeArtifacts, "")
			got := resolveScoreAnswerKeyPath(command, &config.Config{ProjectRoot: root})
			if got != test.want {
				t.Fatalf("resolveScoreAnswerKeyPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func scoreGenerateKeyTestCommand(t *testing.T, configPath, outputPath string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	command := &cobra.Command{Use: "generate-key"}
	command.Flags().String("config", "", "")
	command.Flags().String("output", "", "")
	if err := command.Flags().Set("config", configPath); err != nil {
		t.Fatal(err)
	}
	if err := command.Flags().Set("output", outputPath); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	command.SetOut(stdout)
	return command, stdout
}

func externalScoreTestCommand() *cobra.Command {
	command := &cobra.Command{Use: "score"}
	for _, flag := range []string{"answer-key", "output", "range-artifacts", "attack-box", "region", "profile", "ssh-key", "ssh-user"} {
		command.Flags().String(flag, "", "")
	}
	command.Flags().Bool("live-verify", false, "")
	return command
}

func writeScoreManifest(t *testing.T, root, body string) {
	t.Helper()
	rangeDir := filepath.Join(root, "ad", "TEST")
	if err := os.MkdirAll(rangeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rangeDir, "range.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertOutputNotCreated(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output path exists or could not be inspected: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output directory exists or could not be inspected: %v", err)
	}
}
