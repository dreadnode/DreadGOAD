package cmd

import (
	"fmt"

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
		if config.ConfigMissing() {
			fmt.Println("warning: no dreadgoad.yaml found, using defaults (run 'dreadgoad init' to configure)")
			fmt.Println()
		}
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
		failed := doctor.PrintResults(results)

		if failed > 0 {
			return fmt.Errorf("%d check(s) failed", failed)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
