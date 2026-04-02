package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/variant"
	"github.com/spf13/cobra"
)

var variantCmd = &cobra.Command{
	Use:   "variant",
	Short: "Generate GOAD variants with randomized entity names",
	Long: `Creates a graph-isomorphic copy of GOAD with randomized names while
preserving structure, relationships, vulnerabilities, and attack paths.

All entity names (domains, users, hosts, groups, OUs, passwords) are replaced
with realistic random alternatives. The resulting variant is deployable
exactly like the original GOAD.`,
	Example: `  dreadgoad variant generate
  dreadgoad variant generate --source ad/GOAD --target ad/GOAD-variant-2 --name variant-2`,
}

var variantGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a new GOAD variant",
	RunE:  runVariantGenerate,
}

func init() {
	rootCmd.AddCommand(variantCmd)
	variantCmd.AddCommand(variantGenerateCmd)

	variantGenerateCmd.Flags().String("source", "ad/GOAD", "Source GOAD directory")
	variantGenerateCmd.Flags().String("target", "ad/GOAD-variant-1", "Target variant directory")
	variantGenerateCmd.Flags().String("name", "variant-1", "Variant name")
}

func runVariantGenerate(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	envCfg := cfg.ActiveEnvironment()

	source, _ := cmd.Flags().GetString("source")
	target, _ := cmd.Flags().GetString("target")
	name, _ := cmd.Flags().GetString("name")

	// Use environment config as defaults when flags weren't explicitly set
	if !cmd.Flags().Changed("source") && envCfg.VariantSource != "" {
		source = envCfg.VariantSource
	}
	if !cmd.Flags().Changed("target") && envCfg.VariantTarget != "" {
		target = envCfg.VariantTarget
	}
	if !cmd.Flags().Changed("name") && envCfg.VariantName != "" {
		name = envCfg.VariantName
	}

	// Resolve paths relative to project root
	if !filepath.IsAbs(source) {
		source = filepath.Join(cfg.ProjectRoot, source)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(cfg.ProjectRoot, target)
	}

	gen := variant.NewGenerator(source, target, name)
	if err := gen.Run(); err != nil {
		return fmt.Errorf("variant generation failed: %w", err)
	}

	return nil
}
