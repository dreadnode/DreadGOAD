package cmd

import (
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/doctor"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run pre-flight system checks",
	Long: `Verifies that all required tools and configurations are in place.

Common checks: ansible-core version, Python, jq, zip, Ansible collections, inventory.

Provider-specific:
  aws (default)  AWS CLI, AWS credentials, Terragrunt, Terraform/Tofu
  ludus          Ludus CLI (or SSH reachability when ludus.ssh_host is set), API key`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Get()
		if err != nil {
			return err
		}
		results := doctor.RunChecks(doctor.Options{
			InventoryPath: cfg.InventoryPath(),
			ProjectRoot:   cfg.ProjectRoot,
			Provider:      cfg.ResolvedProvider(),
			Ludus: doctor.LudusOptions{
				APIKey:      cfg.Ludus.APIKey,
				SSHHost:     cfg.Ludus.SSHTarget(),
				SSHUser:     cfg.Ludus.SSHUser,
				SSHKeyPath:  cfg.Ludus.SSHKeyPath,
				SSHPassword: cfg.Ludus.SSHPassword,
				SSHPort:     cfg.Ludus.SSHPort,
				ResolveAlias: cfg.Ludus.Host != "" &&
					cfg.Ludus.SSHUser == "" &&
					cfg.Ludus.SSHKeyPath == "" &&
					cfg.Ludus.SSHPassword == "" &&
					cfg.Ludus.SSHPort == 0,
			},
		})
		doctor.PrintResults(results)

		for _, r := range results {
			if r.Status == "fail" {
				return nil // non-zero shown by print, but don't error on doctor
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
