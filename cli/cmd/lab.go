package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/spf13/cobra"
)

var labCmd = &cobra.Command{
	Use:   "lab",
	Short: "Manage DreadGOAD lab lifecycle",
}

var labStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show lab instance states",
	RunE:  runLabStatus,
}

// labStatusJSON toggles machine-readable JSON output for `lab status`.
// The web app's ingestion hook consumes this to refresh range state.
var labStatusJSON bool

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

var labStartVMCmd = &cobra.Command{
	Use:   "start-vm <hostname>",
	Short: "Start a specific lab VM by hostname",
	Args:  cobra.ExactArgs(1),
	RunE:  runVMAction("start"),
}

var labStopVMCmd = &cobra.Command{
	Use:   "stop-vm <hostname>",
	Short: "Stop a specific lab VM by hostname",
	Args:  cobra.ExactArgs(1),
	RunE:  runVMAction("stop"),
}

var labRestartVMCmd = &cobra.Command{
	Use:   "restart-vm <hostname>",
	Short: "Restart a specific lab VM by hostname",
	Args:  cobra.ExactArgs(1),
	RunE:  runVMAction("restart"),
}

var labDestroyVMCmd = &cobra.Command{
	Use:   "destroy-vm <hostname>",
	Short: "Terminate a specific lab VM by hostname",
	Args:  cobra.ExactArgs(1),
	RunE:  runVMAction("destroy"),
}

func init() {
	rootCmd.AddCommand(labCmd)
	labCmd.AddCommand(labStatusCmd)
	labStatusCmd.Flags().BoolVar(&labStatusJSON, "json", false, "Output machine-readable JSON (per-instance array)")
	labCmd.AddCommand(labStartCmd)
	labCmd.AddCommand(labStopCmd)
	labCmd.AddCommand(labStartVMCmd)
	labCmd.AddCommand(labStopVMCmd)
	labCmd.AddCommand(labRestartVMCmd)
	labCmd.AddCommand(labDestroyVMCmd)
}

func getProvider(ctx context.Context) (provider.Provider, *config.Config, error) {
	cfg, err := config.Get()
	if err != nil {
		return nil, nil, err
	}
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return nil, nil, err
	}
	return prov, cfg, nil
}

func runLabStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	prov, cfg, err := getProvider(ctx)
	if err != nil {
		return err
	}

	instances, err := prov.DiscoverAllInstances(ctx, cfg.Env)
	if err != nil {
		return err
	}

	if labStatusJSON {
		b, err := instancesToStatusJSON(instances)
		if err != nil {
			return fmt.Errorf("marshal status json: %w", err)
		}
		fmt.Println(string(b))
		return nil
	}

	if len(instances) == 0 {
		fmt.Printf("No GOAD instances found for env=%s\n", cfg.Env)
		return nil
	}

	fmt.Printf("GOAD Lab Status (%s, provider: %s)\n", cfg.Env, prov.Name())
	fmt.Printf("%-40s %-24s %-15s %s\n", "NAME", "ID", "STATE", "PRIVATE IP")
	fmt.Println(strings.Repeat("-", 95))

	for _, inst := range instances {
		fmt.Printf("%-40s %-24s %-15s %s\n",
			inst.Name, inst.ID, inst.State, inst.PrivateIP)
	}
	return nil
}

// statusJSONInstance is the machine-readable shape emitted by `lab status --json`:
// RAW cloud fields, intentionally NOT the normalized host schema. The web app's
// ingestion hook correlates `name` → config hostname and normalizes
// state→status / id→cloud_id / private_ip→ip_private onto range hosts (design §6.4).
type statusJSONInstance struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	State     string `json:"state"`
	PrivateIP string `json:"private_ip"`
}

// instancesToStatusJSON renders discovered instances as a JSON array.
// Always returns a JSON array (never null) so an empty range yields "[]".
func instancesToStatusJSON(instances []provider.Instance) ([]byte, error) {
	out := make([]statusJSONInstance, 0, len(instances))
	for _, inst := range instances {
		out = append(out, statusJSONInstance{
			Name:      inst.Name,
			ID:        inst.ID,
			State:     inst.State,
			PrivateIP: inst.PrivateIP,
		})
	}
	return json.MarshalIndent(out, "", "  ")
}

func runLabAction(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := context.Background()
		prov, cfg, err := getProvider(ctx)
		if err != nil {
			return err
		}

		var instances []provider.Instance
		if action == "start" {
			// For start, we want stopped instances.
			all, err := prov.DiscoverAllInstances(ctx, cfg.Env)
			if err != nil {
				return err
			}
			for _, inst := range all {
				if inst.State == "stopped" {
					instances = append(instances, inst)
				}
			}
		} else {
			instances, err = prov.DiscoverInstances(ctx, cfg.Env)
			if err != nil {
				return err
			}
		}

		if len(instances) == 0 {
			return fmt.Errorf("no GOAD instances found for env=%s", cfg.Env)
		}

		var ids []string
		for _, inst := range instances {
			ids = append(ids, inst.ID)
			fmt.Printf("  %s %s (%s)\n", action, inst.Name, inst.ID)
		}

		switch action {
		case "start":
			err = prov.StartInstances(ctx, ids)
		case "stop":
			err = prov.StopInstances(ctx, ids)
		}
		if err != nil {
			return fmt.Errorf("%s instances: %w", action, err)
		}

		fmt.Printf("\nSuccessfully initiated %s for %d instances\n", action, len(ids))
		return nil
	}
}

func execVMAction(ctx context.Context, prov provider.Provider, inst *provider.Instance, action string) error {
	ids := []string{inst.ID}
	switch action {
	case "start":
		if err := prov.StartInstances(ctx, ids); err != nil {
			return fmt.Errorf("start VM: %w", err)
		}
		fmt.Printf("Start initiated for %s\n", inst.Name)
	case "stop":
		if err := prov.StopInstances(ctx, ids); err != nil {
			return fmt.Errorf("stop VM: %w", err)
		}
		fmt.Printf("Stop initiated for %s\n", inst.Name)
	case "restart":
		if inst.State == "running" {
			if err := prov.StopInstances(ctx, ids); err != nil {
				return fmt.Errorf("stop VM: %w", err)
			}
			fmt.Printf("Stop initiated for %s, waiting for stopped state...\n", inst.Name)
			if err := prov.WaitForInstanceStopped(ctx, inst.ID); err != nil {
				return fmt.Errorf("wait for stop: %w", err)
			}
			fmt.Printf("%s is now stopped\n", inst.Name)
		}
		if err := prov.StartInstances(ctx, ids); err != nil {
			return fmt.Errorf("start VM: %w", err)
		}
		fmt.Printf("Start initiated for %s\n", inst.Name)
	case "destroy":
		return destroyVM(ctx, prov, inst)
	}
	return nil
}

func destroyVM(ctx context.Context, prov provider.Provider, inst *provider.Instance) error {
	fmt.Printf("WARNING: This will terminate %s (%s) permanently.\n", inst.Name, inst.ID)
	fmt.Print("Type the instance ID to confirm: ")
	var confirm string
	if _, err := fmt.Scanln(&confirm); err != nil || confirm != inst.ID {
		fmt.Println("Aborted.")
		return nil
	}
	if err := prov.DestroyInstances(ctx, []string{inst.ID}); err != nil {
		return fmt.Errorf("terminate VM: %w", err)
	}
	fmt.Printf("Terminate initiated for %s\n", inst.Name)
	return nil
}

func runVMAction(action string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		hostname := args[0]
		ctx := context.Background()

		prov, cfg, err := getProvider(ctx)
		if err != nil {
			return err
		}

		inst, err := prov.FindInstanceByHostname(ctx, cfg.Env, hostname)
		if err != nil {
			return err
		}

		fmt.Printf("Found: %s (%s) [%s]\n", inst.Name, inst.ID, inst.State)
		return execVMAction(ctx, prov, inst, action)
	}
}
