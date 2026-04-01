package cmd

import (
	"context"
	"fmt"
	"strings"

	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/spf13/cobra"
)

var labCmd = &cobra.Command{
	Use:   "lab",
	Short: "Manage GOAD lab lifecycle",
}

var labStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show lab instance states",
	RunE:  runLabStatus,
}

var labStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start stopped lab instances",
	RunE:  runLabAction("start"),
}

var labStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop running lab instances",
	RunE:  runLabAction("stop"),
}

func init() {
	rootCmd.AddCommand(labCmd)
	labCmd.AddCommand(labStatusCmd)
	labCmd.AddCommand(labStartCmd)
	labCmd.AddCommand(labStopCmd)
}

func runLabStatus(cmd *cobra.Command, args []string) error {
	cfg := config.Get()
	ctx := context.Background()

	region := cfg.Region
	if region == "" {
		region = "us-west-1"
	}

	client, err := daws.NewClient(ctx, region)
	if err != nil {
		return err
	}

	instances, err := client.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return err
	}

	if len(instances) == 0 {
		fmt.Printf("No GOAD instances found for env=%s\n", cfg.Env)
		return nil
	}

	fmt.Printf("GOAD Lab Status (%s)\n", cfg.Env)
	fmt.Printf("%-40s %-24s %-15s %s\n", "NAME", "INSTANCE ID", "STATE", "PRIVATE IP")
	fmt.Println(strings.Repeat("-", 95))

	for _, inst := range instances {
		fmt.Printf("%-40s %-24s %-15s %s\n",
			inst.Name, inst.InstanceID, inst.State, inst.PrivateIP)
	}
	return nil
}

func runLabAction(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()
		ctx := context.Background()

		region := cfg.Region
		if region == "" {
			region = "us-west-1"
		}

		client, err := daws.NewClient(ctx, region)
		if err != nil {
			return err
		}

		instances, err := client.DiscoverInstances(ctx, cfg.Env)
		if err != nil {
			return err
		}

		if len(instances) == 0 {
			return fmt.Errorf("no GOAD instances found for env=%s", cfg.Env)
		}

		var ids []string
		for _, inst := range instances {
			ids = append(ids, inst.InstanceID)
			fmt.Printf("  %s %s (%s)\n", action, inst.Name, inst.InstanceID)
		}

		switch action {
		case "start":
			err = client.StartInstances(ctx, ids)
		case "stop":
			err = client.StopInstances(ctx, ids)
		}
		if err != nil {
			return fmt.Errorf("%s instances: %w", action, err)
		}

		fmt.Printf("\nSuccessfully initiated %s for %d instances\n", action, len(ids))
		return nil
	}
}
