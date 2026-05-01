package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/azure"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/spf13/cobra"
)

// bastionCmd is the Azure-only command tree for native-client access via the
// Azure Bastion module (modules/terraform-azure-bastion). Bastion is opt-in:
// the terragrunt module is excluded unless DREADGOAD_ENABLE_AZURE_BASTION=true
// (or `infra apply --with-bastion`) is set when the stack was applied. When
// no Bastion is deployed, these commands return an actionable error rather
// than silently falling back to Run Command.
var bastionCmd = &cobra.Command{
	Use:   "bastion",
	Short: "Connect to lab VMs via Azure Bastion (SSH, RDP, port tunnel)",
	Long: `Native-client access to lab VMs through the deployed Azure Bastion host.

Requires the Bastion module to be deployed (set DREADGOAD_ENABLE_AZURE_BASTION=true
before 'infra apply', or use 'infra apply --with-bastion'). Tunneling-enabled
Standard/Premium SKUs are required for ssh/rdp/tunnel; the Developer SKU only
supports the browser console.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Get()
		if err != nil {
			return err
		}
		if cfg.ResolvedProvider() != provider.NameAzure {
			return fmt.Errorf("bastion is only available with the Azure provider (current: %s)", cfg.ResolvedProvider())
		}
		return nil
	},
}

var bastionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the deployed Bastion host (or report none)",
	RunE:  runBastionStatus,
}

var bastionSSHCmd = &cobra.Command{
	Use:   "ssh <host>",
	Short: "SSH to a lab VM via Bastion",
	Long: `Open a native-client SSH session to a lab VM through Azure Bastion.

Defaults to password auth (matches the GOAD provisioning workflow). Pass
--auth-type ssh-key --ssh-key <path> for key auth, or --auth-type AAD for
Azure AD-joined VMs.`,
	Args: cobra.ExactArgs(1),
	RunE: runBastionSSH,
}

var bastionRDPCmd = &cobra.Command{
	Use:   "rdp <host>",
	Short: "RDP to a lab VM via Bastion (Windows clients only)",
	Args:  cobra.ExactArgs(1),
	RunE:  runBastionRDP,
}

var bastionTunnelCmd = &cobra.Command{
	Use:   "tunnel <host>",
	Short: "Forward a remote port from a lab VM to localhost via Bastion",
	Long: `Open a port tunnel through Azure Bastion. Defaults to RDP (3389) so
non-Windows clients can point any RDP app at localhost. Override with
--remote-port and --local-port for SMB (445), WinRM (5985/5986), etc.

Requires tunneling_enabled = true on the Bastion (Standard or Premium SKU).`,
	Args: cobra.ExactArgs(1),
	RunE: runBastionTunnel,
}

func init() {
	rootCmd.AddCommand(bastionCmd)
	bastionCmd.AddCommand(bastionStatusCmd)
	bastionCmd.AddCommand(bastionSSHCmd)
	bastionCmd.AddCommand(bastionRDPCmd)
	bastionCmd.AddCommand(bastionTunnelCmd)

	bastionSSHCmd.Flags().StringP("user", "u", "", "Remote username")
	bastionSSHCmd.Flags().String("auth-type", "password", "Auth type: password | ssh-key | AAD")
	bastionSSHCmd.Flags().String("ssh-key", "", "Path to SSH private key (auth-type=ssh-key)")

	bastionTunnelCmd.Flags().Int("remote-port", 3389, "Remote port on the target VM")
	bastionTunnelCmd.Flags().Int("local-port", 3389, "Local port to bind")
}

// azureClientFromProvider extracts the underlying *azure.Client from the
// configured provider. We type-assert via the AzureProvider concrete type
// rather than adding a Bastion method to the provider.Provider interface,
// since Bastion is Azure-specific and shouldn't pollute the cross-provider
// abstraction.
func azureClientFromProvider(prov provider.Provider) (*azure.Client, error) {
	ap, ok := prov.(*azure.AzureProvider)
	if !ok {
		return nil, fmt.Errorf("bastion requires the Azure provider; got %s", prov.Name())
	}
	return ap.Client(), nil
}

func bastionContext(ctx context.Context) (*azure.Client, *azure.BastionHost, *config.Config, error) {
	cfg, err := config.Get()
	if err != nil {
		return nil, nil, nil, err
	}
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	client, err := azureClientFromProvider(prov)
	if err != nil {
		return nil, nil, nil, err
	}
	if _, err := client.VerifyCredentials(ctx); err != nil {
		return nil, nil, nil, err
	}
	host, err := client.DiscoverBastion(ctx, cfg.Env)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("discover bastion: %w", err)
	}
	if host == nil {
		return nil, nil, nil, fmt.Errorf(
			"no Azure Bastion host found for env=%s. Deploy with: "+
				"DREADGOAD_ENABLE_AZURE_BASTION=true dreadgoad infra apply --module bastion "+
				"(or use --with-bastion on infra apply)", cfg.Env)
	}
	return client, host, cfg, nil
}

func runBastionStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return err
	}
	client, err := azureClientFromProvider(prov)
	if err != nil {
		return err
	}
	if _, err := client.VerifyCredentials(ctx); err != nil {
		return err
	}
	host, err := client.DiscoverBastion(ctx, cfg.Env)
	if err != nil {
		return fmt.Errorf("discover bastion: %w", err)
	}
	if host == nil {
		fmt.Printf("No Azure Bastion host deployed for env=%s.\n", cfg.Env)
		fmt.Println("Deploy with: DREADGOAD_ENABLE_AZURE_BASTION=true dreadgoad infra apply --module bastion")
		fmt.Println("           or: dreadgoad infra apply --with-bastion --module bastion")
		return nil
	}
	fmt.Printf("Azure Bastion (%s)\n", cfg.Env)
	fmt.Printf("  Name:              %s\n", host.Name)
	fmt.Printf("  Resource group:    %s\n", host.ResourceGroup)
	fmt.Printf("  Location:          %s\n", host.Location)
	fmt.Printf("  SKU:               %s\n", host.SKU)
	fmt.Printf("  Tunneling enabled: %t\n", host.TunnelingEnabled)
	fmt.Printf("  IP connect:        %t\n", host.IPConnectEnabled)
	if !host.TunnelingEnabled {
		fmt.Println("\nWarning: tunneling is disabled; ssh/rdp/tunnel subcommands require it.")
		fmt.Println("Re-deploy with bastion_tunneling_enabled = true on Standard/Premium SKU.")
	}
	return nil
}

func runBastionSSH(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, host, cfg, err := bastionContext(ctx)
	if err != nil {
		return err
	}
	if !host.TunnelingEnabled {
		return fmt.Errorf("bastion %s does not have tunneling enabled; ssh requires bastion_tunneling_enabled=true on a Standard/Premium SKU", host.Name)
	}

	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return err
	}
	vmID, err := resolveAzureHost(ctx, prov, cfg, args[0])
	if err != nil {
		return err
	}

	user, _ := cmd.Flags().GetString("user")
	authType, _ := cmd.Flags().GetString("auth-type")
	sshKey, _ := cmd.Flags().GetString("ssh-key")

	// Auto-pick the ephemeral key for the in-VNet Ansible controller. The
	// terraform-azure-controller module writes its private key to a
	// well-known path and stamps Role=AnsibleController on the VM, so we
	// can reach it without making the operator type --auth-type ssh-key
	// --ssh-key <path> -u dreadadmin every time. A failed live lookup is
	// non-fatal — we just fall back to the user-supplied flag values.
	if inst, err := client.FindInstanceByHostname(ctx, cfg.Env, args[0]); err == nil && inst.Tags["Role"] == "AnsibleController" {
		if !cmd.Flags().Changed("auth-type") {
			authType = "ssh-key"
		}
		if !cmd.Flags().Changed("ssh-key") && authType == "ssh-key" {
			if path := controllerKeyPath(cfg.Env, inst.Name); path != "" {
				sshKey = path
			}
		}
		if !cmd.Flags().Changed("user") {
			user = "dreadadmin"
		}
	}

	fmt.Printf("Bastion SSH to %s via %s...\n", args[0], host.Name)
	return client.OpenBastionSSH(ctx, host, vmID, user, authType, sshKey)
}

// controllerKeyPath derives the conventional ephemeral private-key path the
// terraform-azure-controller module writes. VM names follow
// "{env}-{deployment}-controller-vm"; the module writes to
// "~/.dreadgoad/keys/azure-{env}-{deployment}-controller". Returns "" if the
// VM name doesn't match the pattern or the key file isn't on disk.
func controllerKeyPath(env, vmName string) string {
	deployment := strings.TrimSuffix(strings.TrimPrefix(vmName, env+"-"), "-controller-vm")
	if deployment == "" || deployment == vmName {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(home, ".dreadgoad", "keys", fmt.Sprintf("azure-%s-%s-controller", env, deployment))
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

func runBastionRDP(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, host, cfg, err := bastionContext(ctx)
	if err != nil {
		return err
	}
	if !host.TunnelingEnabled {
		return fmt.Errorf("bastion %s does not have tunneling enabled; rdp requires bastion_tunneling_enabled=true on a Standard/Premium SKU", host.Name)
	}
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return err
	}
	vmID, err := resolveAzureHost(ctx, prov, cfg, args[0])
	if err != nil {
		return err
	}

	fmt.Printf("Bastion RDP to %s via %s...\n", args[0], host.Name)
	return client.OpenBastionRDP(ctx, host, vmID)
}

func runBastionTunnel(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	client, host, cfg, err := bastionContext(ctx)
	if err != nil {
		return err
	}
	if !host.TunnelingEnabled {
		return fmt.Errorf("bastion %s does not have tunneling enabled; tunnel requires bastion_tunneling_enabled=true on a Standard/Premium SKU", host.Name)
	}
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return err
	}
	vmID, err := resolveAzureHost(ctx, prov, cfg, args[0])
	if err != nil {
		return err
	}

	remotePort, _ := cmd.Flags().GetInt("remote-port")
	localPort, _ := cmd.Flags().GetInt("local-port")

	fmt.Printf("Bastion tunnel %s:%d → localhost:%d via %s\n",
		args[0], remotePort, localPort, host.Name)
	fmt.Println("Press Ctrl+C to close the tunnel.")
	return client.OpenBastionTunnel(ctx, host, vmID, remotePort, localPort)
}
