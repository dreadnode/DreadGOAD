package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/lifecycle"
	"github.com/dreadnode/dreadgoad/internal/rangecommand"
	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
	"github.com/spf13/cobra"
)

var rangeCmd = &cobra.Command{
	Use:   "range",
	Short: "Run range-specific lifecycle operations",
}

var rangeInitSessionCmd = &cobra.Command{
	Use:   "init-session",
	Short: "Run the selected range's declared session initialization actions",
	Args:  cobra.NoArgs,
	RunE:  runRangeInitSession,
}

var rangeCapabilitiesCmd = &cobra.Command{
	Use:   "capabilities",
	Short: "Describe commands supported by the selected range",
	Args:  cobra.NoArgs,
	RunE:  runRangeCapabilities,
}

type rangeCapabilitiesPayload struct {
	Range       string                    `json:"range"`
	Commands    []rangecommand.Capability `json:"commands"`
	AgentPrompt string                    `json:"agent_prompt,omitempty"`
}

func init() {
	rootCmd.AddCommand(rangeCmd)
	rangeCmd.AddCommand(rangeInitSessionCmd)
	rangeCmd.AddCommand(rangeCapabilitiesCmd)
	rangeInitSessionCmd.Flags().String("output-dir", "", "Private directory for generated session artifacts")
	rangeInitSessionCmd.Flags().Bool("json", false, "Output machine-readable action results")
	if err := rangeInitSessionCmd.MarkFlagRequired("output-dir"); err != nil {
		panic(err)
	}
}

func runRangeCapabilities(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	payload, err := loadRangeCapabilities(cfg)
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(payload)
}

func loadRangeCapabilities(cfg *config.Config) (rangeCapabilitiesPayload, error) {
	capabilities, root, err := rangecommand.Capabilities(cfg)
	if err != nil {
		return rangeCapabilitiesPayload{}, err
	}
	agentPrompt, err := rangeconfig.LoadAgentPrompt(root.Path)
	if err != nil {
		return rangeCapabilitiesPayload{}, err
	}
	return rangeCapabilitiesPayload{
		Range: cfg.ResolvedLab(), Commands: capabilities, AgentPrompt: agentPrompt,
	}, nil
}

func runRangeInitSession(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	outputDir, _ := cmd.Flags().GetString("output-dir")
	results, runErr := lifecycle.RunSessionInit(cfg, outputDir)

	jsonOut, _ := cmd.Flags().GetBool("json")
	switch {
	case jsonOut:
		payload := struct {
			Actions []lifecycle.Result `json:"actions"`
		}{Actions: results}
		if payload.Actions == nil {
			payload.Actions = []lifecycle.Result{}
		}
		encoder := json.NewEncoder(cmd.OutOrStdout())
		if err := encoder.Encode(payload); err != nil {
			return err
		}
	case len(results) == 0:
		_, err = fmt.Fprintln(cmd.OutOrStdout(), "No session initialization actions are declared for this range.")
		if err != nil {
			return err
		}
	default:
		for _, result := range results {
			line := fmt.Sprintf("%-9s %s", strings.ToUpper(result.Status), result.Action)
			if result.Message != "" {
				line += ": " + result.Message
			}
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), line); err != nil {
				return err
			}
		}
	}
	return runErr
}
