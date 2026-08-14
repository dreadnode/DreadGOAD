package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/azure"
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

var labDescribeCmd = &cobra.Command{
	Use:   "describe <hostname>",
	Short: "Show the disks and network interfaces attached to a lab VM",
	Long: `Describe one VM's attached resources: managed disks and network
interfaces, with their sizes, SKUs, subnets and security groups.

Azure only. Read-only — nothing is modified.

--id takes the VM's full ARM resource ID and skips hostname resolution, which
otherwise lists every VM in the subscription to substring-match the name. A
caller that already holds the ID (the console, which stores it on each range
node) should pass it.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runLabDescribe,
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
	labCmd.AddCommand(labDescribeCmd)
	labDescribeCmd.Flags().String("id", "", "Full ARM resource ID of the VM (skips hostname lookup)")
	labDescribeCmd.Flags().Bool("json", false, "Output machine-readable JSON")
	labCmd.AddCommand(labDestroyVMCmd)
	// Lets a caller with nobody at a keyboard run destroy-vm at all. Without it
	// the confirmation below reads stdin, which a non-interactive caller cannot
	// answer: fmt.Scanln fails, the function prints "Aborted." and returns nil,
	// and the caller is told the VM was destroyed when it was untouched.
	labDestroyVMCmd.Flags().Bool("yes", false, "Skip the confirmation prompt (for non-interactive callers)")
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
	// Where the instance lives. Account is the AWS account ID or Azure
	// subscription ID; Group is the Azure resource group (AWS has no
	// equivalent). Both are omitted when the provider can't determine them, so
	// consumers can distinguish "unknown" from "empty".
	Account string `json:"account,omitempty"`
	Group   string `json:"group,omitempty"`
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
			Account:   inst.Account,
			Group:     inst.Group,
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

// vmActionTimeout bounds a single-VM lifecycle action end to end.
const vmActionTimeout = 15 * time.Minute

// Describing a VM is two reads, not a power operation. Borrowing
// vmActionTimeout would leave the console's detail panel spinning for a quarter
// of an hour on a stalled call; this fails while an operator is still watching.
const describeTimeout = 90 * time.Second

func execVMAction(ctx context.Context, prov provider.Provider, inst *provider.Instance, action string, yes bool) error {
	ids := []string{inst.ID}
	switch action {
	case "start":
		fmt.Printf("Starting %s...\n", inst.Name)
		if err := prov.StartInstances(ctx, ids); err != nil {
			return fmt.Errorf("start VM: %w", err)
		}
		fmt.Printf("%s is running\n", inst.Name)
	case "stop":
		fmt.Printf("Stopping %s (deallocating; this takes a few minutes)...\n", inst.Name)
		if err := prov.StopInstances(ctx, ids); err != nil {
			return fmt.Errorf("stop VM: %w", err)
		}
		fmt.Printf("%s is stopped\n", inst.Name)
	case "restart":
		// StopInstances/StartInstances block until the Azure operation
		// completes — they are not "initiate and return". Announce before the
		// call, not after: printed afterwards these lines describe a state the
		// operator never sees the command in, and a restart shows nothing at
		// all for the minutes it actually takes.
		if inst.State == "running" {
			fmt.Printf("Stopping %s (deallocating; this takes a few minutes)...\n", inst.Name)
			if err := prov.StopInstances(ctx, ids); err != nil {
				return fmt.Errorf("stop VM: %w", err)
			}
			// The wait is required and stays. Whether StopInstances blocks is
			// per-provider: Azure polls the deallocate to completion, but AWS
			// (internal/aws/ec2.go) just calls the EC2 API and returns, so
			// without this the Start below would be issued against an instance
			// still in "stopping" and rejected. Redundant on Azure, and cheap
			// there now that the poll checks before it sleeps.
			if err := prov.WaitForInstanceStopped(ctx, inst.ID); err != nil {
				return fmt.Errorf("wait for stop: %w", err)
			}
			fmt.Printf("%s is stopped\n", inst.Name)
		}
		fmt.Printf("Starting %s...\n", inst.Name)
		if err := prov.StartInstances(ctx, ids); err != nil {
			return fmt.Errorf("start VM: %w", err)
		}
		fmt.Printf("%s is running\n", inst.Name)
	case "destroy":
		return destroyVM(ctx, prov, inst, yes)
	}
	return nil
}

// destroyVM terminates one instance, confirming first unless the caller has
// already done so.
//
// The prompt reads stdin, so it can only be answered by a human at a terminal.
// A non-interactive caller — the console, CI, any script — gets an error from
// Scanln, falls into the abort branch, and is told "Aborted." with a nil error:
// exit 0 for a VM that still exists. --yes exists so those callers can run the
// command at all, and it is deliberately a flag rather than a stdin check so an
// operator at a terminal keeps the type-the-ID gate.
func destroyVM(ctx context.Context, prov provider.Provider, inst *provider.Instance, yes bool) error {
	if !yes {
		fmt.Printf("WARNING: This will terminate %s (%s) permanently.\n", inst.Name, inst.ID)
		fmt.Print("Type the instance ID to confirm: ")
		var confirm string
		if _, err := fmt.Scanln(&confirm); err != nil || confirm != inst.ID {
			fmt.Println("Aborted.")
			return nil
		}
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
		// Bounded, because none of the calls below are. StopInstances and
		// StartInstances poll an Azure long-running operation to completion,
		// and with context.Background() a stalled operation hangs the command
		// forever with no output — indistinguishable from one that is simply
		// slow. 15 minutes is well above a real deallocate+start and well
		// below the point where an operator has lost the afternoon.
		ctx, cancel := context.WithTimeout(context.Background(), vmActionTimeout)
		defer cancel()

		prov, cfg, err := getProvider(ctx)
		if err != nil {
			return err
		}

		inst, err := prov.FindInstanceByHostname(ctx, cfg.Env, hostname)
		if err != nil {
			return err
		}

		fmt.Printf("Found: %s (%s) [%s]\n", inst.Name, inst.ID, inst.State)
		// Only destroy-vm registers --yes; GetBool returns false for the others,
		// which is the value they want anyway.
		yes, _ := cmd.Flags().GetBool("yes")
		return execVMAction(ctx, prov, inst, action, yes)
	}
}

// runLabDescribe reports one VM's attached disks and NICs.
//
// Azure-specific, so it type-asserts the provider rather than widening the
// cross-provider interface — the same trade bastion.go makes for the same
// reason. Other providers get a plain refusal instead of an empty result that
// looks like a VM with no disks.
func runLabDescribe(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), describeTimeout)
	defer cancel()

	prov, cfg, err := getProvider(ctx)
	if err != nil {
		return err
	}
	client, err := azureClientFromProvider(prov)
	if err != nil {
		return err
	}

	id, _ := cmd.Flags().GetString("id")
	if id == "" {
		if len(args) == 0 {
			return fmt.Errorf("give a hostname, or --id with the VM's resource ID")
		}
		inst, err := client.FindInstanceByHostname(ctx, cfg.Env, args[0])
		if err != nil {
			return err
		}
		id = inst.ID
	}

	detail, err := client.DescribeInstance(ctx, id)
	if err != nil {
		return err
	}

	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(detail)
	}
	printInstanceDetail(cmd, detail)
	return nil
}

func printInstanceDetail(cmd *cobra.Command, d *azure.InstanceDetail) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s  (%s", d.Name, d.ResourceGroup)
	if d.Location != "" {
		fmt.Fprintf(out, ", %s", d.Location)
	}
	if d.VMSize != "" {
		fmt.Fprintf(out, ", %s", d.VMSize)
	}
	// The JSON carries power state and the console panel shows it; a terminal
	// reader asking about a VM's disks wants to know it is running just as much.
	if d.PowerState != "" {
		fmt.Fprintf(out, ", %s", d.PowerState)
	}
	fmt.Fprintln(out, ")")

	fmt.Fprintf(out, "\nDisks (%d)\n", len(d.Disks))
	for _, disk := range d.Disks {
		size := ""
		if disk.SizeGB != nil {
			size = fmt.Sprintf("%d GB", *disk.SizeGB)
		}
		fmt.Fprintf(out, "  %-4s %-38s %-9s %s\n", disk.Role, disk.Name, size, disk.StorageType)
	}

	fmt.Fprintf(out, "\nNetwork interfaces (%d)\n", len(d.NICs))
	for _, nic := range d.NICs {
		fmt.Fprintf(out, "  %-38s %s\n", nic.Name, strings.Join(nic.PrivateIPs, ", "))
		if nic.SubnetID != "" {
			fmt.Fprintf(out, "      subnet %s\n", nic.SubnetID)
		}
		if nic.NSGID != "" {
			fmt.Fprintf(out, "      nsg    %s\n", nic.NSGID)
		}
	}
}
