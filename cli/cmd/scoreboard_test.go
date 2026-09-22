package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/spf13/cobra"
)

func TestScoreboardDemoAllowsBuiltinActiveDirectoryScorer(t *testing.T) {
	repositoryRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	command, stdout := scoreboardDemoTestCommand(
		t,
		filepath.Join(repositoryRoot, "ad", "GOAD", "data", "config.json"),
	)

	if err := runScoreboardDemoWithConfig(command, &config.Config{
		ProjectRoot: repositoryRoot,
		Lab:         "GOAD",
		Env:         "test",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "DreadGOAD SCOREBOARD") {
		t.Fatalf("scoreboard demo did not render the board: %q", stdout.String())
	}
}

func TestScoreboardDemoRejectsExecutableScorerBeforeReadingLabConfig(t *testing.T) {
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
	handlerPath := filepath.Join(root, "ad", "TEST", "commands", "score")
	if err := os.MkdirAll(filepath.Dir(handlerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(handlerPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command, _ := scoreboardDemoTestCommand(t, filepath.Join(root, "missing-config.json"))

	err := runScoreboardDemoWithConfig(command, &config.Config{
		ProjectRoot: root,
		Lab:         "TEST",
		Env:         "test",
	})
	if err == nil || !strings.Contains(err.Error(), "only available for the built-in active-directory scorer") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "missing-config.json") {
		t.Fatalf("demo read config before checking scorer ownership: %v", err)
	}
}

func TestScoreboardDemoRejectsDisabledScorerBeforeReadingLabConfig(t *testing.T) {
	root := t.TempDir()
	writeScoreManifest(t, root, `schema_version: 1
kind: active-directory
commands:
  score:
    enabled: false
`)
	command, _ := scoreboardDemoTestCommand(t, filepath.Join(root, "missing-config.json"))

	err := runScoreboardDemoWithConfig(command, &config.Config{
		ProjectRoot: root,
		Lab:         "TEST",
		Env:         "test",
	})
	if err == nil || !strings.Contains(err.Error(), "does not support score") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "missing-config.json") {
		t.Fatalf("demo read config before checking scorer availability: %v", err)
	}
}

func scoreboardDemoTestCommand(t *testing.T, configPath string) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	command := &cobra.Command{Use: "demo"}
	command.Flags().String("config", "", "")
	if err := command.Flags().Set("config", configPath); err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	command.SetOut(stdout)
	return command, stdout
}
