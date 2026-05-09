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
		if err := config.Init(); err != nil {
			return err
		}
		cfg, err := config.Get()
		if err != nil {
			return err
		}
		logging.Init(cfg.Debug, cfg.LogDir, cfg.Env)
		return nil
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

// SetVersionInfo sets the root command version from build-time ldflags.
func SetVersionInfo(version, commit, date string) {
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)
}

// Execute runs the root cobra command and returns any error encountered.
// It is the entry point called from main.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().StringP("env", "e", "staging", "Target environment (dev, staging, prod)")
	rootCmd.PersistentFlags().StringP("provider", "p", "", "Infrastructure provider (aws, azure, proxmox, ludus)")
	rootCmd.PersistentFlags().String("region", "", "AWS region (required for AWS commands; can also be set via --region, dreadgoad.yaml, DREADGOAD_REGION, or inventory ansible_aws_ssm_region where supported)")
	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug/verbose output")
	rootCmd.PersistentFlags().String("config", "", "Config file path")

	for _, bind := range []struct {
		key  string
		flag string
	}{
		{"env", "env"},
		{"provider", "provider"},
		{"region", "region"},
		{"debug", "debug"},
		{"config", "config"},
	} {
		if err := viper.BindPFlag(bind.key, rootCmd.PersistentFlags().Lookup(bind.flag)); err != nil {
			panic(fmt.Sprintf("failed to bind flag %q: %v", bind.flag, err))
		}
	}
}
