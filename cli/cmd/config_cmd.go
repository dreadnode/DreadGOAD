package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage CLI configuration",
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Display current effective configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Get()
		fmt.Printf("Environment:    %s\n", cfg.Env)
		fmt.Printf("Region:         %s\n", valueOrDefault(cfg.Region, "(from inventory)"))
		fmt.Printf("Debug:          %v\n", cfg.Debug)
		fmt.Printf("Max Retries:    %d\n", cfg.MaxRetries)
		fmt.Printf("Retry Delay:    %ds\n", cfg.RetryDelay)
		fmt.Printf("Idle Timeout:   %ds\n", cfg.IdleTimeout)
		fmt.Printf("Log Dir:        %s\n", cfg.LogDir)
		fmt.Printf("Project Root:   %s\n", cfg.ProjectRoot)
		fmt.Printf("Inventory:      %s\n", cfg.InventoryPath())
		fmt.Printf("Ansible Config: %s\n", cfg.AnsibleCfgPath())
		fmt.Printf("Playbooks:      %s\n", strings.Join(cfg.Playbooks, ", "))

		if cfgFile := viper.ConfigFileUsed(); cfgFile != "" {
			fmt.Printf("\nConfig file:    %s\n", cfgFile)
		} else {
			fmt.Println("\nNo config file found (using defaults)")
		}
		return nil
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create default configuration file",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, _ := os.UserHomeDir()
		dir := filepath.Join(home, ".config", "dreadgoad")
		_ = os.MkdirAll(dir, 0o755)
		cfgPath := filepath.Join(dir, "dreadgoad.yaml")

		if _, err := os.Stat(cfgPath); err == nil {
			return fmt.Errorf("config file already exists: %s", cfgPath)
		}

		content := `# DreadGOAD CLI Configuration
env: dev
# region: us-west-2  # Override AWS region (default: from inventory)
debug: false
max_retries: 3
retry_delay: 30
idle_timeout: 1200
# log_dir: ~/.ansible/logs/goad
# project_root: /path/to/DreadGOAD  # Auto-detected if omitted
`
		if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
			return err
		}
		fmt.Printf("Created config: %s\n", cfgPath)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		viper.Set(args[0], args[1])
		cfgFile := viper.ConfigFileUsed()
		if cfgFile == "" {
			return fmt.Errorf("no config file found. Run: dreadgoad config init")
		}
		if err := viper.WriteConfig(); err != nil {
			return err
		}
		fmt.Printf("Set %s = %s in %s\n", args[0], args[1], cfgFile)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configSetCmd)
}

func valueOrDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
