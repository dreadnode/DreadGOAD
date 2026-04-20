package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
		cfg, err := config.Get()
		if err != nil {
			return err
		}
		fmt.Printf("Environment:    %s\n", cfg.Env)
		fmt.Printf("Region:         %s\n", valueOrDefault(cfg.Region, "(unset — required for AWS commands)"))
		fmt.Printf("Debug:          %v\n", cfg.Debug)
		fmt.Printf("Max Retries:    %d\n", cfg.MaxRetries)
		fmt.Printf("Retry Delay:    %ds\n", cfg.RetryDelay)
		fmt.Printf("Idle Timeout:   %ds\n", cfg.IdleTimeout)
		fmt.Printf("Log Dir:        %s\n", cfg.LogDir)
		fmt.Printf("Project Root:   %s\n", cfg.ProjectRoot)
		fmt.Printf("Inventory:      %s\n", cfg.InventoryPath())
		fmt.Printf("Ansible Config: %s\n", cfg.AnsibleCfgPath())
		fmt.Printf("Playbooks:      %s\n", strings.Join(cfg.Playbooks, ", "))

		fmt.Println("\nEnvironments:")
		if len(cfg.Environments) == 0 {
			fmt.Println("  (none configured, using defaults)")
		} else {
			for name, ec := range cfg.Environments {
				marker := ""
				if name == cfg.Env {
					marker = " (active)"
				}
				fmt.Printf("  %s%s:\n", name, marker)
				fmt.Printf("    variant: %v\n", ec.Variant)
				if ec.Variant {
					fmt.Printf("    variant_source: %s\n", valueOrDefault(ec.VariantSource, "ad/GOAD"))
					fmt.Printf("    variant_target: %s\n", valueOrDefault(ec.VariantTarget, "ad/GOAD-variant-1"))
					fmt.Printf("    variant_name:   %s\n", valueOrDefault(ec.VariantName, "variant-1"))
				}
			}
		}

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
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolving home directory: %w", err)
		}
		dir := filepath.Join(home, ".config", "dreadgoad")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating config directory: %w", err)
		}
		cfgPath := filepath.Join(dir, "dreadgoad.yaml")

		if _, err := os.Stat(cfgPath); err == nil {
			return fmt.Errorf("config file already exists: %s", cfgPath)
		}

		content := `# DreadGOAD CLI Configuration
env: staging
# region: us-east-1  # AWS region (required for AWS commands; can also be set via DREADGOAD_REGION or --region)
debug: false
max_retries: 3
retry_delay: 30
idle_timeout: 1200
# log_dir: ~/.ansible/logs/goad
# project_root: /path/to/DreadGOAD  # Auto-detected if omitted

# Per-environment settings
environments:
  dev:
    variant: true
    variant_source: ad/GOAD
    variant_target: ad/GOAD-variant-1
    variant_name: variant-1
    vpc_cidr: "10.0.0.0/16"
  staging:
    variant: false
    vpc_cidr: "10.1.0.0/16"
  prod:
    vpc_cidr: "10.2.0.0/16"
  test:
    vpc_cidr: "10.8.0.0/16"
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

var configTraceCmd = &cobra.Command{
	Use:   "trace",
	Short: "Show where each configuration value comes from",
	Long: `Displays every configuration key with its effective value and the
source that provided it (cli flag, env var, config file, or default).

Also shows the Ansible environment variables and extra-vars that Go
injects at runtime, highlighting overlaps with ansible.cfg.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Get()
		if err != nil {
			return err
		}

		// Collect which persistent flags were explicitly set on the CLI.
		changedFlags := make(map[string]bool)
		for _, name := range []string{"env", "region", "debug", "config"} {
			if f := cmd.Root().PersistentFlags().Lookup(name); f != nil && f.Changed {
				changedFlags[name] = true
			}
		}

		// --- Section 1: Go/Viper config ---
		entries := config.TraceConfig(cfg, changedFlags)

		fmt.Println("Go/Viper Configuration")
		fmt.Println("======================")
		if cfgFile := viper.ConfigFileUsed(); cfgFile != "" {
			fmt.Printf("Config file: %s\n", cfgFile)
		} else {
			fmt.Println("Config file: (none found)")
		}
		fmt.Println("Precedence: cli flag > env var (DREADGOAD_*) > config file > default")
		fmt.Println()

		maxKey, maxVal := 0, 0
		for _, e := range entries {
			if len(e.Key) > maxKey {
				maxKey = len(e.Key)
			}
			if len(e.Value) > maxVal {
				maxVal = len(e.Value)
			}
		}
		for _, e := range entries {
			fmt.Printf("  %-*s  = %-*s  [%s]\n", maxKey, e.Key, maxVal, e.Value, e.Source)
		}

		// --- Section 2: Ansible env vars ---
		fmt.Println()
		fmt.Println("Ansible Environment Variables (injected by Go)")
		fmt.Println("===============================================")
		ansibleEnv, err := cfg.AnsibleEnv()
		if err != nil {
			return err
		}
		envKeys := make([]string, 0, len(ansibleEnv))
		for k := range ansibleEnv {
			envKeys = append(envKeys, k)
		}
		sort.Strings(envKeys)
		for _, k := range envKeys {
			origin := "hardcoded"
			switch k {
			case "ANSIBLE_CONFIG":
				origin = "derived: project_root"
			case "ANSIBLE_CACHE_PLUGIN_CONNECTION":
				origin = "derived: env"
			}
			fmt.Printf("  %-38s = %-45s [%s]\n", k, ansibleEnv[k], origin)
		}

		// --- Section 3: Ansible extra-vars ---
		fmt.Println()
		fmt.Println("Ansible Extra-Vars (injected by Go at runtime)")
		fmt.Println("===============================================")
		labConfig := cfg.LabConfigPath()
		if _, statErr := os.Stat(labConfig); statErr == nil {
			fmt.Printf("  @%s\n", labConfig)
			fmt.Println("    [derived: env + project_root]")
		} else {
			fmt.Printf("  @%s (not found)\n", labConfig)
		}
		fmt.Println("  ansible_facts_gathering_timeout=60  [hardcoded]")
		fmt.Println()
		fmt.Println("  Note: Error-specific retry logic may add extra-vars at runtime.")
		fmt.Println("  Run with --debug to see exact ansible-playbook invocations.")

		// --- Section 4: ansible.cfg overlaps ---
		fmt.Println()
		fmt.Println("ansible.cfg Overlaps")
		fmt.Println("====================")
		fmt.Printf("  Path: %s\n\n", cfg.AnsibleCfgPath())
		fmt.Println("  ansible.cfg setting             overridden by")
		fmt.Println("  ───────────────────────────────  ───────────────────────────────────────────")
		fmt.Println("  fact_caching_connection          ANSIBLE_CACHE_PLUGIN_CONNECTION (always)")
		fmt.Println("  host_key_checking                ANSIBLE_HOST_KEY_CHECKING (always)")
		fmt.Println("  gathering                        ANSIBLE_GATHERING (retry: fact-gathering)")
		fmt.Println("  timeout                          ANSIBLE_TIMEOUT (retry: SSM/reconnection)")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configInitCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configTraceCmd)
}

func valueOrDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
