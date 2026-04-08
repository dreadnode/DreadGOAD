package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/ansible"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/spf13/cobra"
)

var extensionCmd = &cobra.Command{
	Use:     "extension",
	Aliases: []string{"ext"},
	Short:   "Manage lab extensions",
	Long:    `List, inspect, and provision lab extensions such as ELK, Exchange, Guacamole, and more.`,
}

var extensionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available extensions",
	RunE:  runExtensionList,
}

var extensionProvisionCmd = &cobra.Command{
	Use:   "provision <name>",
	Short: "Provision a specific extension",
	Args:  cobra.ExactArgs(1),
	RunE:  runExtensionProvision,
}

var extensionProvisionAllCmd = &cobra.Command{
	Use:   "provision-all",
	Short: "Provision all enabled extensions for the active environment",
	RunE:  runExtensionProvisionAll,
}

func init() {
	rootCmd.AddCommand(extensionCmd)
	extensionCmd.AddCommand(extensionListCmd)
	extensionCmd.AddCommand(extensionProvisionCmd)
	extensionCmd.AddCommand(extensionProvisionAllCmd)

	extensionListCmd.Flags().String("lab", "", "Filter by lab compatibility (e.g. GOAD, GOAD-Light)")

	extensionProvisionCmd.Flags().String("limit", "", "Limit execution to specific hosts")
	extensionProvisionCmd.Flags().Int("max-retries", 0, "Max retry attempts (default: from config)")
	extensionProvisionCmd.Flags().Int("retry-delay", 0, "Delay between retries in seconds")
}

func runExtensionList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	labFilter, _ := cmd.Flags().GetString("lab")

	names := make([]string, 0, len(cfg.Extensions))
	for name := range cfg.Extensions {
		names = append(names, name)
	}
	sort.Strings(names)

	enabled := cfg.EnabledExtensionsForEnv()
	enabledSet := make(map[string]bool, len(enabled))
	for _, e := range enabled {
		enabledSet[e] = true
	}

	fmt.Printf("Available extensions (env: %s):\n\n", cfg.Env)
	for _, name := range names {
		ext := cfg.Extensions[name]

		if labFilter != "" && !cfg.IsExtensionCompatible(name, labFilter) {
			continue
		}

		status := " "
		if enabledSet[name] {
			status = "*"
		}

		compat := strings.Join(ext.Compatibility, ", ")
		fmt.Printf("  [%s] %-12s  %s\n", status, name, ext.Description)
		fmt.Printf("      machines: %s | compatible: %s\n", strings.Join(ext.Machines, ", "), compat)
		if ext.Impact != "" && ext.Impact != "none" {
			fmt.Printf("      impact: %s\n", ext.Impact)
		}
		fmt.Println()
	}

	fmt.Println("  [*] = enabled for current environment")
	return nil
}

func runExtensionProvision(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	name := args[0]

	ext, ok := cfg.Extensions[name]
	if !ok {
		return fmt.Errorf("unknown extension %q; run 'dreadgoad extension list' to see available extensions", name)
	}

	limit, _ := cmd.Flags().GetString("limit")
	maxRetries, _ := cmd.Flags().GetInt("max-retries")
	retryDelay, _ := cmd.Flags().GetInt("retry-delay")

	return provisionExtension(cfg, name, ext, limit, maxRetries, retryDelay)
}

func runExtensionProvisionAll(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	enabled := cfg.EnabledExtensionsForEnv()

	if len(enabled) == 0 {
		fmt.Printf("No extensions enabled for environment %q.\n", cfg.Env)
		fmt.Println("Enable extensions in your config under environments.<env>.enabled_extensions")
		return nil
	}

	fmt.Printf("Provisioning %d extension(s) for environment %q:\n", len(enabled), cfg.Env)
	for _, name := range enabled {
		fmt.Printf("  - %s\n", name)
	}
	fmt.Println()

	for _, name := range enabled {
		ext, ok := cfg.Extensions[name]
		if !ok {
			return fmt.Errorf("enabled extension %q not found in config", name)
		}
		if err := provisionExtension(cfg, name, ext, "", 0, 0); err != nil {
			return fmt.Errorf("extension %s failed: %w", name, err)
		}
	}

	fmt.Println("All extensions provisioned successfully.")
	return nil
}

func provisionExtension(cfg *config.Config, name string, ext config.ExtensionConfig, limit string, maxRetries, retryDelay int) error {
	ctx := context.Background()

	_ = os.MkdirAll(cfg.LogDir, 0o755)
	logFile := filepath.Join(cfg.LogDir, fmt.Sprintf("%s-ext-%s-%s.log",
		cfg.Env, name, time.Now().Format("20060102_150405")))

	extInvPath := cfg.ExtensionInventoryTemplate(name)

	extraVars := make(map[string]string)
	if ext.DataDir != "" {
		extraVars["extension_data_dir"] = cfg.ExtensionDataDir(name)
	}

	// Guacamole needs special vars
	if name == "guacamole" {
		extraVars["lab_data_file"] = filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "data", "inventory.yml")
		extraVars["guacamole_vars_file"] = filepath.Join(cfg.ExtensionDataDir(name), "guacamole.yml")

		var dataFiles []string
		for extName, extCfg := range cfg.Extensions {
			if extCfg.DataDir != "" {
				dataFile := filepath.Join(cfg.ExtensionDataDir(extName), "config.json")
				if _, err := os.Stat(dataFile); err == nil {
					dataFiles = append(dataFiles, dataFile)
				}
			}
		}
		if len(dataFiles) > 0 {
			extraVars["extension_data_files"] = "[" + strings.Join(dataFiles, ",") + "]"
		}
	}

	fmt.Println("===============================================")
	fmt.Printf("Provisioning extension: %s\n", name)
	fmt.Printf("Playbook: ansible/playbooks/%s\n", ext.Playbook)
	fmt.Printf("Log file: %s\n", logFile)
	fmt.Println("===============================================")

	opts := ansible.RetryOptions{
		Playbook:    ext.Playbook,
		Env:         cfg.Env,
		Inventories: []string{extInvPath},
		ExtraVars:   extraVars,
		Limit:       limit,
		Debug:       cfg.Debug,
		LogFile:     logFile,
	}
	if maxRetries > 0 {
		opts.MaxRetries = maxRetries
	}
	if retryDelay > 0 {
		opts.RetryDelay = time.Duration(retryDelay) * time.Second
	}

	if err := ansible.RunPlaybookWithRetry(ctx, opts); err != nil {
		return err
	}

	fmt.Printf("Extension %s provisioned successfully.\n", name)
	return nil
}
