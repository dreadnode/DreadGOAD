package cmd

import (
	"fmt"
	"os"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "dreadgoad",
	Short: "DreadGOAD - Active Directory lab management CLI",
	Long: `DreadGOAD orchestrates the deployment and management of intentionally
vulnerable Active Directory environments for security research and testing.

It manages the full lifecycle: infrastructure provisioning via Terraform,
configuration via Ansible, validation of vulnerability configurations,
and operational tasks like SSM session management.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()
		logging.Init(cfg.Debug, cfg.LogDir, cfg.Env)
		return nil
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}

func init() {
	cobra.OnInitialize(config.Init)

	rootCmd.PersistentFlags().StringP("env", "e", "dev", "Target environment (dev, staging, prod)")
	rootCmd.PersistentFlags().String("region", "", "AWS region (default: from inventory)")
	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug/verbose output")
	rootCmd.PersistentFlags().String("config", "", "Config file path")

	_ = viper.BindPFlag("env", rootCmd.PersistentFlags().Lookup("env"))
	_ = viper.BindPFlag("region", rootCmd.PersistentFlags().Lookup("region"))
	_ = viper.BindPFlag("debug", rootCmd.PersistentFlags().Lookup("debug"))
}
