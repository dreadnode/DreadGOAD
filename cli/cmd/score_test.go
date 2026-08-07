package cmd

import (
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
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
