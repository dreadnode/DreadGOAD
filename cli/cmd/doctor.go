package cmd

import (
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/doctor"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run pre-flight system checks",
	Long: `Verifies that all required tools and configurations are in place:
ansible-core version, AWS CLI, Python, Ansible collections, credentials, and inventory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()
		results := doctor.RunChecks(cfg.InventoryPath(), cfg.ProjectRoot)
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
