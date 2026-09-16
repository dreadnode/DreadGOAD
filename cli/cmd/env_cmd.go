package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
	"github.com/dreadnode/dreadgoad/internal/variant"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Manage deployment environments",
}

var envCreateCmd = &cobra.Command{
	Use:   "create <env-name>",
	Short: "Create a new deployment environment",
	Long: `Scaffold a new deployment environment with all required infrastructure
and configuration files. The active environment's lab selects the range-owned
scaffolding profile and provider template.

Creates:
  - infra/{provider}/{deployment}/{env}/ infrastructure (provider layout varies)
  - a range-owned config or randomized Active Directory variant
  - {env}-inventory

Use --variant only with ranges that declare variant support.`,
	Args: cobra.ExactArgs(1),
	RunE: runEnvCreate,
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available environments",
	RunE:  runEnvList,
}

func init() {
	rootCmd.AddCommand(envCmd)
	envCmd.AddCommand(envCreateCmd)
	envCmd.AddCommand(envListCmd)

	envCreateCmd.Flags().String("region", "", "Region for the environment (e.g. us-west-2 for AWS, centralus for Azure)")
	envCreateCmd.Flags().String("vpc-cidr", "", "VPC/VNet CIDR block (default: auto-assigned)")
	envCreateCmd.Flags().String("reference", "staging", "Reference environment to copy infrastructure from (default: selected range metadata)")
	envCreateCmd.Flags().Bool("variant", false, "Generate randomized variant config")
	envCreateCmd.Flags().String("variant-source", defaultVariantSource, "Base lab to generate the variant from (with --variant)")
	envCreateCmd.Flags().Bool("force", false, "Overwrite existing environment")
}

func runEnvCreate(cmd *cobra.Command, args []string) error {
	envName := strings.TrimSpace(args[0])
	if err := validateEnvName(envName); err != nil {
		return err
	}

	cfg, err := config.Get()
	if err != nil {
		return err
	}

	useVariant, _ := cmd.Flags().GetBool("variant")
	force, _ := cmd.Flags().GetBool("force")
	variantSource, _ := cmd.Flags().GetString("variant-source")
	if strings.TrimSpace(variantSource) == "" {
		variantSource = defaultVariantSource
	}

	plan, err := resolveScaffoldPlan(cfg, variantSource, useVariant)
	if err != nil {
		return err
	}

	region, _ := cmd.Flags().GetString("region")
	if region == "" {
		region = cfg.ResolvedRegion()
	}
	if region == "" {
		region = plan.Spec.DefaultRegion
	}
	if err := validatePathComponent("region", region); err != nil {
		return fmt.Errorf("env create requires a valid region: %w", err)
	}

	vpcCIDR, _ := cmd.Flags().GetString("vpc-cidr")
	if plan.Spec.Network.CIDR != "" && !plan.Spec.NetworkEditable(plan.Profile == rangeconfig.ProfileActiveDir) {
		if vpcCIDR != "" && vpcCIDR != plan.Spec.Network.CIDR {
			return fmt.Errorf("range %s requires VPC/VNet CIDR %s; got %s", plan.Lab, plan.Spec.Network.CIDR, vpcCIDR)
		}
		vpcCIDR = plan.Spec.Network.CIDR
	}
	if vpcCIDR == "" {
		vpcCIDR = cfg.VpcCIDR(envName)
	}

	reference, _ := cmd.Flags().GetString("reference")
	if !cmd.Flags().Changed("reference") {
		reference = plan.Spec.TemplateEnvironment
	}

	return scaffoldEnvWithPlan(cfg, plan, scaffoldRequest{
		envName:       envName,
		region:        region,
		vpcCIDR:       vpcCIDR,
		reference:     reference,
		variantSource: variantSource,
		useVariant:    useVariant,
		force:         force,
	})
}

type scaffoldPlan struct {
	Lab     string
	LabPath string
	Profile string
	Spec    rangeconfig.ProviderSpec
}

// scaffoldRequest holds the operator-selected values for one environment.
// Keeping them named prevents provider, region, reference, and variant values
// from being accidentally swapped as the request moves through scaffolding.
type scaffoldRequest struct {
	envName       string
	region        string
	vpcCIDR       string
	reference     string
	variantSource string
	useVariant    bool
	force         bool
}

// scaffoldContext combines a validated request with the paths and provider
// metadata derived from it. Scaffold helpers consume this single coherent
// value instead of independently reconstructing paths from positional strings.
type scaffoldContext struct {
	scaffoldRequest
	projectRoot        string
	plan               scaffoldPlan
	provider           string
	envDir             string
	regionDir          string
	inventoryPath      string
	referenceRegionDir string
	hostFilter         map[string]bool
}

// scaffoldArtifacts records only paths atomically claimed by this invocation.
// A failed scaffold may safely remove these paths without touching artifacts
// created by another process between preflight and creation.
type scaffoldArtifacts struct {
	paths []string
}

func (artifacts *scaffoldArtifacts) reserveDirectory(path string) error {
	if err := os.Mkdir(path, 0o755); err != nil {
		return err
	}
	artifacts.paths = append(artifacts.paths, path)
	return nil
}

func (artifacts *scaffoldArtifacts) reserveFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		if removeErr := os.Remove(path); removeErr != nil {
			return fmt.Errorf("close reserved file: %w; remove reservation: %v", err, removeErr)
		}
		return fmt.Errorf("close reserved file: %w", err)
	}
	artifacts.paths = append(artifacts.paths, path)
	return nil
}

func (artifacts *scaffoldArtifacts) track(path string) {
	artifacts.paths = append(artifacts.paths, path)
}

func resolveScaffoldPlan(cfg *config.Config, variantSource string, useVariant bool) (scaffoldPlan, error) {
	labName := cfg.ResolvedLab()
	labPath := cfg.LabPath()
	if useVariant && strings.TrimSpace(variantSource) != "" {
		labPath = variantSource
		if !filepath.IsAbs(labPath) {
			labPath = filepath.Join(cfg.ProjectRoot, labPath)
		}
		labName = filepath.Base(filepath.Clean(labPath))
	}

	manifest, found, err := rangeconfig.Load(labPath)
	if err != nil {
		return scaffoldPlan{}, err
	}
	if !found {
		manifest = &rangeconfig.Manifest{Kind: rangeconfig.KindActiveDirectory}
	}
	if useVariant && !manifest.SupportsVariants() {
		return scaffoldPlan{}, fmt.Errorf("variants are not supported for range %s", labName)
	}

	provider := cfg.ResolvedProvider()
	providerDir := filepath.Join(labPath, "providers", provider)
	if info, err := os.Stat(providerDir); err != nil || !info.IsDir() {
		return scaffoldPlan{}, fmt.Errorf("range %s does not support provider %s", labName, provider)
	}
	spec, ok := manifest.Provider(provider)
	if !ok {
		return scaffoldPlan{}, fmt.Errorf("range %s has no scaffolding metadata for provider %s", labName, provider)
	}
	return scaffoldPlan{
		Lab:     labName,
		LabPath: labPath,
		Profile: spec.ScaffoldProfile,
		Spec:    spec,
	}, nil
}

func validatePathComponent(label, value string) error {
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) || !envNameRe.MatchString(value) {
		return fmt.Errorf("%s %q is not a safe directory name", label, value)
	}
	return nil
}

// defaultVariantSource is the base lab a variant is generated from when the
// caller does not name one. Matches `variant generate --source`.
const defaultVariantSource = "ad/GOAD"

// variantTargetFor returns the directory `--variant` will generate into.
//
// Derived as <source basename>-<env> rather than a literal "GOAD-" prefix, so a
// variant of ad/SCCM lands in ad/SCCM-<env> instead of a GOAD-named directory
// holding an SCCM lab. For the default source this is byte-identical to the old
// behaviour (ad/GOAD -> GOAD-<env>), so existing environments are unaffected;
// only the sources that --variant-source newly made reachable differ.
//
// The target is derived rather than accepted as a flag so it cannot disagree
// with the source and environment it belongs to — and so it matches what the
// console writes into variant_target for the same pair.
func variantTargetFor(projectRoot, envName, variantSource string) string {
	source := variantSource
	if source == "" {
		source = defaultVariantSource
	}
	return filepath.Join(projectRoot, "ad", filepath.Base(source)+"-"+envName)
}

// envNameRe is what an environment name may contain. The name is not just a
// label: it becomes a directory under infra/, the {env}-inventory filename, the
// ad/GOAD-{env} variant tree, and part of the deployed Azure resource names.
//
// Dots are deliberately allowed — "3.1" and "dg-test-2.A" are real environment
// names. They used to break variant resolution, but that was viper splitting
// config keys on ".", fixed in config.repairDottedEnvironmentKeys rather than by
// forbidding the character.
var envNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// validateEnvName rejects names that would escape or corrupt the paths built
// from them, before any directory is created.
func validateEnvName(name string) error {
	if name == "" {
		return fmt.Errorf("environment name cannot be empty")
	}
	// Checked ahead of the pattern so traversal gets a message that names the
	// actual problem rather than a generic "invalid character".
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("environment name %q would escape the project directory", name)
	}
	if !envNameRe.MatchString(name) {
		return fmt.Errorf(
			"environment name %q is not usable as a directory and file name\n"+
				"  use letters, digits, dot, hyphen or underscore, starting with a letter or digit (e.g. 3.1, dg-test-2.A)",
			name)
	}
	return nil
}

func scaffoldEnv(cfg *config.Config, request scaffoldRequest) error {
	if request.useVariant {
		source := request.variantSource
		if source == "" {
			source = defaultVariantSource
		}
		if !filepath.IsAbs(source) {
			source = filepath.Join(cfg.ProjectRoot, source)
		}
		if err := variant.ValidateSource(source); err != nil {
			return fmt.Errorf("validate variant source: %w", err)
		}
	}
	plan, err := resolveScaffoldPlan(cfg, request.variantSource, request.useVariant)
	if err != nil {
		return err
	}
	return scaffoldEnvWithPlan(cfg, plan, request)
}

func scaffoldEnvWithPlan(cfg *config.Config, plan scaffoldPlan, request scaffoldRequest) error {
	ctx, err := newScaffoldContext(cfg, plan, request)
	if err != nil {
		return err
	}
	if err := ensureScaffoldTargetsAvailable(ctx); err != nil {
		return err
	}
	if err := resolveScaffoldReference(&ctx); err != nil {
		return err
	}

	// Atomically claim each top-level artifact before writing it. This both
	// closes the preflight/create race and gives rollback an exact ownership
	// list. --force retains its historical in-place semantics and therefore
	// deliberately skips reservations and automatic cleanup.
	artifacts := scaffoldArtifacts{}
	succeeded := false
	defer cleanupFailedScaffold(&succeeded, &artifacts)
	if err := reserveScaffoldArtifacts(ctx, &artifacts); err != nil {
		return err
	}

	printEnvSummary(ctx.provider, ctx.envName, ctx.region, ctx.vpcCIDR, ctx.reference, ctx.useVariant)

	if err := scaffoldPlanInfrastructure(ctx); err != nil {
		return err
	}
	if ctx.hostFilter != nil {
		color.Green("  Copied infrastructure from %s (filtered to %d hosts)", ctx.reference, len(ctx.hostFilter))
	} else {
		color.Green("  Copied infrastructure from %s", ctx.reference)
	}

	configPath, err := scaffoldLabConfigForPlan(ctx, &artifacts)
	if err != nil {
		return err
	}

	if err := scaffoldInventoryForPlan(ctx); err != nil {
		return err
	}
	color.Green("  Created inventory: %s", filepath.Base(ctx.inventoryPath))

	printNextSteps(ctx.provider, ctx.envName, ctx.region, ctx.envDir, configPath, ctx.inventoryPath)
	succeeded = true
	return nil
}

func reserveScaffoldArtifacts(ctx scaffoldContext, artifacts *scaffoldArtifacts) error {
	if ctx.force {
		return nil
	}
	if err := artifacts.reserveDirectory(ctx.envDir); err != nil {
		return fmt.Errorf("reserve environment directory %s: %w", ctx.envDir, err)
	}
	if !ctx.useVariant && ctx.plan.Profile == rangeconfig.ProfileActiveDir && ctx.plan.Lab == "GOAD" {
		overlay := filepath.Join(ctx.projectRoot, "ad", "GOAD", "data", ctx.envName+"-overlay.json")
		if err := artifacts.reserveFile(overlay); err != nil {
			return fmt.Errorf("reserve lab overlay %s: %w", overlay, err)
		}
	}
	if err := artifacts.reserveFile(ctx.inventoryPath); err != nil {
		return fmt.Errorf("reserve inventory %s: %w", ctx.inventoryPath, err)
	}
	return nil
}

func newScaffoldContext(cfg *config.Config, plan scaffoldPlan, request scaffoldRequest) (scaffoldContext, error) {
	if err := validateScaffoldRequest(cfg.ProjectRoot, request); err != nil {
		return scaffoldContext{}, err
	}
	provider := cfg.ResolvedProvider()
	deployment := scaffoldDeployment(cfg, plan)
	infraBase := infraBaseForDeployment(cfg.ProjectRoot, provider, deployment)
	ctx := scaffoldContext{
		scaffoldRequest: request,
		projectRoot:     cfg.ProjectRoot,
		plan:            plan,
		provider:        provider,
		envDir:          filepath.Join(infraBase, request.envName),
		inventoryPath:   filepath.Join(cfg.ProjectRoot, request.envName+"-inventory"),
	}
	ctx.regionDir = filepath.Join(ctx.envDir, request.region)
	return ctx, nil
}

func resolveScaffoldReference(ctx *scaffoldContext) error {
	infraBase := filepath.Dir(ctx.envDir)
	ctx.referenceRegionDir = findReferenceRegion(infraBase, ctx.reference, ctx.plan.Spec.DefaultRegion)
	if ctx.referenceRegionDir == "" {
		return fmt.Errorf("reference environment %q not found in %s", ctx.reference, infraBase)
	}
	labSource := scaffoldLabSource(ctx.projectRoot, ctx.plan, ctx.variantSource, ctx.useVariant)
	ctx.hostFilter = labHostKeysFromPath(labSource)
	if err := validateScaffoldTemplateHosts(*ctx); err != nil {
		return err
	}
	return nil
}

func validateScaffoldRequest(projectRoot string, request scaffoldRequest) error {
	if err := validateEnvName(request.envName); err != nil {
		return err
	}
	if err := validatePathComponent("region", request.region); err != nil {
		return err
	}
	if err := validatePathComponent("reference environment", request.reference); err != nil {
		return err
	}
	if !request.useVariant {
		return nil
	}
	source := request.variantSource
	if source == "" {
		source = defaultVariantSource
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(projectRoot, source)
	}
	if err := variant.ValidateSource(source); err != nil {
		return fmt.Errorf("validate variant source: %w", err)
	}
	return nil
}

func scaffoldDeployment(cfg *config.Config, plan scaffoldPlan) string {
	if configured := cfg.ActiveEnvironment().Deployment; configured != "" {
		return configured
	}
	return plan.Spec.Deployment
}

func ensureScaffoldTargetsAvailable(ctx scaffoldContext) error {
	if ctx.force {
		return nil
	}
	exists, err := scaffoldTargetExists(ctx.envDir)
	if err != nil {
		return fmt.Errorf("inspect environment target %s: %w", ctx.envDir, err)
	}
	if exists {
		return fmt.Errorf("environment %q already exists at %s\nUse --force to overwrite", ctx.envName, ctx.envDir)
	}
	exists, err = scaffoldTargetExists(ctx.inventoryPath)
	if err != nil {
		return fmt.Errorf("inspect inventory target %s: %w", ctx.inventoryPath, err)
	}
	if exists {
		return fmt.Errorf("inventory for environment %q already exists at %s\nUse --force to overwrite", ctx.envName, ctx.inventoryPath)
	}
	if ctx.useVariant {
		target := variantTargetFor(ctx.projectRoot, ctx.envName, ctx.variantSource)
		exists, err = scaffoldTargetExists(target)
		if err != nil {
			return fmt.Errorf("inspect variant target %s: %w", target, err)
		}
		if exists {
			return fmt.Errorf("variant target already exists at %s", target)
		}
		return nil
	}
	if ctx.plan.Profile == rangeconfig.ProfileActiveDir && ctx.plan.Lab == "GOAD" {
		overlay := filepath.Join(ctx.projectRoot, "ad", "GOAD", "data", ctx.envName+"-overlay.json")
		exists, err = scaffoldTargetExists(overlay)
		if err != nil {
			return fmt.Errorf("inspect lab overlay target %s: %w", overlay, err)
		}
		if exists {
			return fmt.Errorf("lab overlay already exists at %s", overlay)
		}
	}
	return nil
}

func scaffoldTargetExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func scaffoldLabSource(projectRoot string, plan scaffoldPlan, variantSource string, useVariant bool) string {
	if !useVariant || variantSource == "" {
		return plan.LabPath
	}
	if filepath.IsAbs(variantSource) {
		return variantSource
	}
	return filepath.Join(projectRoot, variantSource)
}

func validateScaffoldTemplateHosts(ctx scaffoldContext) error {
	if ctx.plan.Profile != rangeconfig.ProfileActiveDir {
		return nil
	}
	missing := missingTemplateHosts(ctx.referenceRegionDir, ctx.hostFilter)
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"range %s cannot be scaffolded from %s for %s; template has no modules for: %s",
		ctx.plan.Lab, ctx.reference, ctx.provider, strings.Join(missing, ", "),
	)
}

func cleanupFailedScaffold(succeeded *bool, artifacts *scaffoldArtifacts) {
	if *succeeded {
		return
	}
	for index := len(artifacts.paths) - 1; index >= 0; index-- {
		path := artifacts.paths[index]
		if err := os.RemoveAll(path); err != nil {
			slog.Warn("could not clean failed environment scaffold", "path", path, "error", err)
		}
	}
}

func scaffoldPlanInfrastructure(ctx scaffoldContext) error {
	switch ctx.plan.Profile {
	case rangeconfig.ProfileActiveDir:
		if err := scaffoldHCL(ctx); err != nil {
			return err
		}
		if err := copyInfrastructure(ctx.referenceRegionDir, ctx.regionDir, ctx.hostFilter); err != nil {
			return fmt.Errorf("copy infrastructure: %w", err)
		}
		labDataName := ctx.plan.Lab
		if ctx.useVariant {
			labDataName = filepath.Base(variantTargetFor(ctx.projectRoot, ctx.envName, ctx.variantSource))
		}
		if err := repointInfrastructureLab(ctx.regionDir, labDataName); err != nil {
			return fmt.Errorf("point infrastructure at range config: %w", err)
		}
		return nil
	case rangeconfig.ProfileTemplate:
		return scaffoldTemplateInfrastructure(ctx)
	default:
		return fmt.Errorf("unsupported scaffold profile %q", ctx.plan.Profile)
	}
}

func infraBaseForDeployment(projectRoot, provider, deployment string) string {
	if provider == "azure" {
		return filepath.Join(projectRoot, "infra", "azure", deployment)
	}
	return filepath.Join(projectRoot, "infra", deployment)
}

func missingTemplateHosts(refRegionDir string, hosts map[string]bool) []string {
	var missing []string
	for host := range hosts {
		if info, err := os.Stat(filepath.Join(refRegionDir, "goad", host)); err != nil || !info.IsDir() {
			missing = append(missing, host)
		}
	}
	sort.Strings(missing)
	return missing
}

func repointInfrastructureLab(regionDir, labName string) error {
	if labName == "GOAD" {
		return nil
	}
	old := "/ad/GOAD/data/"
	newValue := "/ad/" + labName + "/data/"
	return filepath.WalkDir(regionDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".hcl" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		updated := strings.ReplaceAll(string(raw), old, newValue)
		if updated == string(raw) {
			return nil
		}
		return os.WriteFile(path, []byte(updated), 0o644)
	})
}

func printEnvSummary(provider, envName, region, vpcCIDR, reference string, useVariant bool) {
	cidrLabel := "VPC CIDR:"
	if provider == "azure" {
		cidrLabel = "VNet CIDR:"
	}
	color.Cyan("Creating environment: %s", envName)
	fmt.Printf("  %-14s %s\n", "Provider:", provider)
	fmt.Printf("  %-14s %s\n", "Region:", region)
	fmt.Printf("  %-14s %s\n", cidrLabel, vpcCIDR)
	fmt.Printf("  %-14s %s\n", "Reference:", reference)
	fmt.Printf("  %-14s %v\n", "Variant:", useVariant)
	fmt.Println()
}

func scaffoldHCL(ctx scaffoldContext) error {
	if ctx.provider == "azure" {
		if err := createAzureEnvHCL(ctx.envDir, ctx.envName, ctx.vpcCIDR, ctx.hostFilter); err != nil {
			return fmt.Errorf("create env.hcl: %w", err)
		}
		color.Green("  Created env.hcl (Azure)")
		if err := createAzureRegionHCL(ctx.regionDir, ctx.region); err != nil {
			return fmt.Errorf("create region.hcl: %w", err)
		}
		color.Green("  Created %s/region.hcl (location=%s)", ctx.region, ctx.region)
	} else {
		if err := createEnvHCL(ctx.envDir, ctx.envName, ctx.vpcCIDR); err != nil {
			return fmt.Errorf("create env.hcl: %w", err)
		}
		color.Green("  Created env.hcl")
		if err := createRegionHCL(ctx.regionDir, ctx.region); err != nil {
			return fmt.Errorf("create region.hcl: %w", err)
		}
		color.Green("  Created %s/region.hcl", ctx.region)
	}
	return nil
}

func scaffoldLabConfig(ctx scaffoldContext, artifacts *scaffoldArtifacts) (string, error) {
	if ctx.useVariant {
		created, err := generateVariantConfig(ctx.projectRoot, ctx.envName, ctx.variantSource)
		if created && !ctx.force {
			artifacts.track(variantTargetFor(ctx.projectRoot, ctx.envName, ctx.variantSource))
		}
		if err != nil {
			return "", fmt.Errorf("generate variant config: %w", err)
		}
		configPath := filepath.Join(variantTargetFor(ctx.projectRoot, ctx.envName, ctx.variantSource), "data")
		color.Green("  Generated variant config in %s", configPath)
		return configPath, nil
	}
	if err := copyBaseConfig(ctx.projectRoot, ctx.envName); err != nil {
		return "", fmt.Errorf("copy base config: %w", err)
	}
	configPath := filepath.Join(ctx.projectRoot, "ad", "GOAD", "data", ctx.envName+"-overlay.json")
	color.Green("  Created overlay: %s-overlay.json", ctx.envName)
	return configPath, nil
}

func scaffoldLabConfigForPlan(ctx scaffoldContext, artifacts *scaffoldArtifacts) (string, error) {
	if ctx.useVariant {
		return scaffoldLabConfig(ctx, artifacts)
	}
	if ctx.plan.Profile == rangeconfig.ProfileActiveDir && ctx.plan.Lab == "GOAD" {
		return scaffoldLabConfig(ctx, artifacts)
	}
	// Non-variant ranges consume their authored base config directly. Creating a
	// GOAD overlay here would silently point a service range or GOAD-Light at the
	// wrong data tree.
	configPath := filepath.Join(ctx.plan.LabPath, "data", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		return "", fmt.Errorf("range base config: %w", err)
	}
	color.Green("  Using range config: %s", configPath)
	return configPath, nil
}

func scaffoldTemplateInfrastructure(ctx scaffoldContext) error {
	if err := os.MkdirAll(ctx.envDir, 0o755); err != nil {
		return fmt.Errorf("create environment directory: %w", err)
	}
	sourceEnv := filepath.Dir(ctx.referenceRegionDir)
	envTemplate, err := os.ReadFile(filepath.Join(sourceEnv, "env.hcl"))
	if err != nil {
		return fmt.Errorf("read template env.hcl: %w", err)
	}
	// Template profiles are authored as complete, working environments. Render
	// only explicit placeholders, quoted tokens, and resource-name prefixes so a
	// short reference name cannot corrupt unrelated HCL substrings.
	renderedEnv := renderTemplateContent(string(envTemplate), ctx.reference, ctx.envName, filepath.Base(ctx.referenceRegionDir), ctx.region)
	if err := os.WriteFile(filepath.Join(ctx.envDir, "env.hcl"), []byte(renderedEnv), 0o644); err != nil {
		return fmt.Errorf("write env.hcl: %w", err)
	}
	if ctx.provider == "azure" {
		if err := createAzureRegionHCL(ctx.regionDir, ctx.region); err != nil {
			return fmt.Errorf("create region.hcl: %w", err)
		}
	} else {
		if err := createRegionHCL(ctx.regionDir, ctx.region); err != nil {
			return fmt.Errorf("create region.hcl: %w", err)
		}
	}
	if err := copyInfrastructure(ctx.referenceRegionDir, ctx.regionDir, nil); err != nil {
		return fmt.Errorf("copy template infrastructure: %w", err)
	}
	return renderTemplateTree(ctx.regionDir, ctx.reference, ctx.envName, filepath.Base(ctx.referenceRegionDir), ctx.region)
}

func renderTemplateTree(root, reference, envName, templateRegion, region string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rendered := renderTemplateContent(string(raw), reference, envName, templateRegion, region)
		if rendered == string(raw) {
			return nil
		}
		return os.WriteFile(path, []byte(rendered), 0o644)
	})
}

func renderTemplateContent(content, reference, envName, templateRegion, region string) string {
	// Prefer explicit placeholders. Prefix and quoted-token replacements keep
	// existing authored templates compatible without turning a short reference
	// such as "test" into an unsafe global substring replacement.
	content = strings.ReplaceAll(content, "{{env}}", envName)
	content = strings.ReplaceAll(content, reference+"-", envName+"-")
	content = strings.ReplaceAll(content, fmt.Sprintf("%q", reference), fmt.Sprintf("%q", envName))
	content = strings.ReplaceAll(content, "{{region}}", region)
	content = strings.ReplaceAll(content, fmt.Sprintf("%q", templateRegion), fmt.Sprintf("%q", region))
	return content
}

func scaffoldInventoryForPlan(ctx scaffoldContext) error {
	if ctx.plan.Profile == rangeconfig.ProfileActiveDir {
		if err := scaffoldInventory(ctx); err != nil {
			return err
		}
		if !ctx.useVariant && ctx.plan.Lab != "GOAD" {
			if err := repointInventoryDomainTo(ctx.projectRoot, ctx.envName, ctx.plan.Lab); err != nil {
				return fmt.Errorf("repoint inventory domain_name: %w", err)
			}
		}
		return filterInventoryHosts(ctx.inventoryPath, ctx.hostFilter)
	}
	template := filepath.Join(ctx.plan.LabPath, "providers", ctx.provider, "inventory")
	raw, err := os.ReadFile(template)
	if err != nil {
		return fmt.Errorf("read range inventory template: %w", err)
	}
	content := renderTemplateContent(string(raw), ctx.reference, ctx.envName, ctx.plan.Spec.DefaultRegion, ctx.region)
	if err := os.WriteFile(ctx.inventoryPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write range inventory: %w", err)
	}
	return nil
}

func filterInventoryHosts(path string, keep map[string]bool) error {
	if keep == nil {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	all := map[string]bool{"dc01": true, "dc02": true, "dc03": true, "srv01": true, "srv02": true, "srv03": true, "ws01": true, "lx01": true}
	lines := strings.Split(string(raw), "\n")
	out := lines[:0]
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && all[strings.ToLower(fields[0])] && !keep[strings.ToLower(fields[0])] {
			continue
		}
		out = append(out, line)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

func scaffoldInventory(ctx scaffoldContext) error {
	var err error
	if ctx.provider == "azure" {
		err = generateAzureInventory(ctx.projectRoot, ctx.envName, ctx.reference)
	} else {
		err = generateInventory(ctx.projectRoot, ctx.envName, ctx.region, ctx.reference)
	}
	if err != nil {
		return fmt.Errorf("generate inventory: %w", err)
	}
	if ctx.useVariant {
		if err := repointInventoryDomain(ctx.projectRoot, ctx.envName, ctx.variantSource); err != nil {
			return fmt.Errorf("repoint inventory domain_name: %w", err)
		}
	}
	return nil
}

// repointInventoryDomain points a variant environment's inventory at the
// variant's own asset tree.
//
// The inventory is built from a reference environment or the stock provider
// template, so it arrives carrying the BASE lab's domain_name. Playbooks
// resolve vulnerability and security scripts as ad/{{ domain_name }}/scripts
// (ansible/playbooks/security.yml, vulnerabilities.yml), so leaving it would
// provision a randomized variant using the stock lab's assets — quietly, and
// only for environments created this way.
//
// Rewriting in place rather than sourcing the variant's own inventory wholesale
// is deliberate: on AWS the reference carries SSM settings (ansible_aws_ssm_*,
// bucket) that the variant template does not have, so swapping the source would
// trade this bug for a worse one. domain_name is the only functional difference
// between the two.
func repointInventoryDomain(projectRoot, envName, variantSource string) error {
	target := filepath.Base(variantTargetFor(projectRoot, envName, variantSource))
	return repointInventoryDomainTo(projectRoot, envName, target)
}

func repointInventoryDomainTo(projectRoot, envName, target string) error {
	invPath := filepath.Join(projectRoot, envName+"-inventory")
	data, err := os.ReadFile(invPath)
	if err != nil {
		return err
	}
	updated := variant.RepointDomainName(string(data), target)
	if updated == string(data) {
		return nil
	}
	if err := os.WriteFile(invPath, []byte(updated), 0o644); err != nil {
		return err
	}
	color.Green("  Pointed inventory domain_name at %s", target)
	return nil
}

func printNextSteps(provider, envName, region, envDir, configPath, invPath string) {
	fmt.Println()
	color.Green("Environment %q created successfully!", envName)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Review: %s\n", envDir)
	fmt.Printf("  2. Review: %s\n", configPath)
	fmt.Printf("  3. Review: %s\n", invPath)
	if provider == "azure" {
		fmt.Printf("  4. Deploy: dreadgoad -p azure -e %s --region %s up\n", envName, region)
	} else {
		fmt.Printf("  4. Initialize: dreadgoad -e %s --region %s infra init\n", envName, region)
		fmt.Printf("  5. Plan:       dreadgoad -e %s --region %s infra plan\n", envName, region)
		fmt.Printf("  6. Apply:      dreadgoad -e %s --region %s infra apply --auto-approve\n", envName, region)
		fmt.Printf("  7. Sync IDs:   dreadgoad -e %s --region %s inventory sync\n", envName, region)
	}
}

func runEnvList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	infraBase := cfg.InfraBasePathForProvider(cfg.ResolvedProvider())

	entries, err := os.ReadDir(infraBase)
	if err != nil {
		return fmt.Errorf("read deployment directory: %w", err)
	}

	color.Cyan("Available environments:")
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		envHCL := filepath.Join(infraBase, name, "env.hcl")
		if _, err := os.Stat(envHCL); err != nil {
			continue
		}

		var regions []string
		regionEntries, _ := os.ReadDir(filepath.Join(infraBase, name))
		for _, re := range regionEntries {
			if !re.IsDir() {
				continue
			}
			regionHCL := filepath.Join(infraBase, name, re.Name(), "region.hcl")
			if _, err := os.Stat(regionHCL); err == nil {
				regions = append(regions, re.Name())
			}
		}

		goadData := filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "data")
		hasConfig := false
		for _, suffix := range []string{"-overlay.json", "-config.json"} {
			if _, err := os.Stat(filepath.Join(goadData, name+suffix)); err == nil {
				hasConfig = true
				break
			}
		}
		// Also check variant target directory.
		if !hasConfig {
			variantData := filepath.Join(cfg.ProjectRoot, "ad", "GOAD-"+name, "data")
			if _, err := os.Stat(filepath.Join(variantData, "config.json")); err == nil {
				hasConfig = true
			}
		}

		marker := " "
		if name == cfg.Env {
			marker = "*"
		}

		configStatus := color.RedString("no config")
		if hasConfig {
			configStatus = color.GreenString("config OK")
		}

		fmt.Printf("  %s %-12s  regions: %-20s  %s\n",
			marker, name, strings.Join(regions, ", "), configStatus)
	}

	return nil
}

func findReferenceRegion(infraBase, reference, preferredRegion string) string {
	refDir := filepath.Join(infraBase, reference)
	if preferredRegion != "" {
		preferred := filepath.Join(refDir, preferredRegion)
		if _, err := os.Stat(filepath.Join(preferred, "region.hcl")); err == nil {
			return preferred
		}
		return ""
	}
	entries, err := os.ReadDir(refDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		regionHCL := filepath.Join(refDir, entry.Name(), "region.hcl")
		if _, err := os.Stat(regionHCL); err == nil {
			return filepath.Join(refDir, entry.Name())
		}
	}
	return ""
}

func createEnvHCL(envDir, envName, vpcCIDR string) error {
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`# Set common variables for the environment.
# This is automatically pulled in by the root terragrunt.hcl configuration.
locals {
  deployment_name = "dreadgoad"      # Change to your deployment name
  aws_account_id  = get_aws_account_id()
  env             = %q
  vpc_cidr        = %q

  # Optional Kali attack box. Enable with --with-kali on infra commands.
  kali_instance_type = "t3.medium"
}
`, envName, vpcCIDR)
	return os.WriteFile(filepath.Join(envDir, "env.hcl"), []byte(content), 0o644)
}

func createRegionHCL(regionDir, region string) error {
	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`locals {
  aws_region = %q
}
`, region)
	return os.WriteFile(filepath.Join(regionDir, "region.hcl"), []byte(content), 0o644)
}

func labHostKeysFromPath(source string) map[string]bool {
	data, err := os.ReadFile(filepath.Join(source, "data", "config.json"))
	if err != nil {
		return nil
	}
	var cfg struct {
		Lab struct {
			Hosts map[string]json.RawMessage `json:"hosts"`
		} `json:"lab"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	hosts := make(map[string]bool, len(cfg.Lab.Hosts))
	for k := range cfg.Lab.Hosts {
		hosts[k] = true
	}
	return hosts
}

func copyInfrastructure(srcRegionDir, dstRegionDir string, hostFilter map[string]bool) error {
	return filepath.WalkDir(srcRegionDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcRegionDir, path)
		if err != nil {
			return err
		}

		if strings.Contains(relPath, ".terragrunt-cache") ||
			strings.Contains(relPath, ".terraform") ||
			strings.HasSuffix(relPath, ".terraform.lock.hcl") ||
			relPath == "region.hcl" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		skipHost, err := shouldSkipHostUnit(path, relPath, d, hostFilter)
		if err != nil {
			return err
		}
		if skipHost {
			return filepath.SkipDir
		}

		dstPath := filepath.Join(dstRegionDir, relPath)

		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dstPath, data, 0o644)
	})
}

// shouldSkipHostUnit identifies Terragrunt host units excluded by the lab's
// host filter. Other direct children of goad/ are shared assets and must remain.
func shouldSkipHostUnit(path, relPath string, d fs.DirEntry, hostFilter map[string]bool) (bool, error) {
	if hostFilter == nil || !d.IsDir() {
		return false, nil
	}
	parts := strings.Split(relPath, string(filepath.Separator))
	if len(parts) != 2 || parts[0] != "goad" || hostFilter[parts[1]] {
		return false, nil
	}
	_, err := os.Stat(filepath.Join(path, "terragrunt.hcl"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func copyBaseConfig(projectRoot, envName string) error {
	dstPath := filepath.Join(projectRoot, "ad", "GOAD", "data", envName+"-overlay.json")

	// Copy dev-overlay.json as starting template if it exists.
	devOverlay := filepath.Join(projectRoot, "ad", "GOAD", "data", "dev-overlay.json")
	if data, err := os.ReadFile(devOverlay); err == nil {
		return os.WriteFile(dstPath, data, 0o644)
	}

	// Otherwise create an empty overlay (inherits base config as-is).
	return os.WriteFile(dstPath, []byte("{}\n"), 0o644)
}

func generateInventory(projectRoot, envName, region, reference string) error {
	refInvPath, err := resolveReferenceInventory(projectRoot, reference)
	if err != nil {
		return err
	}
	dstInvPath := filepath.Join(projectRoot, envName+"-inventory")

	data, err := os.ReadFile(refInvPath)
	if err != nil {
		return fmt.Errorf("read reference inventory %s: %w", filepath.Base(refInvPath), err)
	}
	content := string(data)

	envRe := regexp.MustCompile(`(?m)^(\s*env=)(.+)$`)
	regionRe := regexp.MustCompile(`(?m)^(\s*ansible_aws_ssm_region=)(.+)$`)
	bucketRe := regexp.MustCompile(`(?m)^(\s*ansible_aws_ssm_bucket_name=)(.+)$`)
	instanceRe := regexp.MustCompile(`(ansible_host=)i-[0-9a-f]+`)
	ipFieldRe := regexp.MustCompile(`\s+(?:dc_ipv4|host_ipv4)=\S+`)

	refEnv := reference
	if m := envRe.FindStringSubmatch(content); len(m) > 2 {
		refEnv = strings.TrimSpace(m[2])
	}
	refRegion := ""
	if m := regionRe.FindStringSubmatch(content); len(m) > 2 {
		refRegion = strings.TrimSpace(m[2])
	}

	content = envRe.ReplaceAllStringFunc(content, func(match string) string {
		eq := strings.Index(match, "=")
		return match[:eq+1] + envName
	})

	content = regionRe.ReplaceAllStringFunc(content, func(match string) string {
		eq := strings.Index(match, "=")
		return match[:eq+1] + region
	})

	if refRegion != "" {
		if m := bucketRe.FindStringSubmatch(content); len(m) > 2 {
			oldBucket := strings.TrimSpace(m[2])
			newBucket := strings.Replace(oldBucket, refEnv+"-"+refRegion, envName+"-"+region, 1)
			content = bucketRe.ReplaceAllString(content, "${1}"+newBucket)
		}
	}

	content = instanceRe.ReplaceAllString(content, "${1}PENDING")

	content = ipFieldRe.ReplaceAllString(content, "")

	return os.WriteFile(dstInvPath, []byte(content), 0o644)
}

func resolveReferenceInventory(projectRoot, reference string) (string, error) {
	candidates := []string{
		filepath.Join(projectRoot, reference+"-inventory"),
		filepath.Join(projectRoot, reference+"-inventory.example"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", fmt.Errorf(
		"reference inventory %q not found; expected %s or %s",
		reference,
		filepath.Base(candidates[0]),
		filepath.Base(candidates[1]),
	)
}

// generateVariantConfig builds the variant for an environment.
//
// “variantSource“ names the base lab to copy from — ad/GOAD by default, but
// the repo ships several (GOAD-Light, GOAD-Mini, SCCM, NHA, DRACARYS) and they
// differ in host count and provider support. It was previously hardcoded to
// ad/GOAD, which made `env create --variant` unable to express any of them.
// Relative paths resolve against the project root, matching how
// `variant generate --source` and the config's variant_source are written.
func generateVariantConfig(projectRoot, envName, variantSource string) (bool, error) {
	source := variantSource
	if source == "" {
		source = defaultVariantSource
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(projectRoot, source)
	}
	target := variantTargetFor(projectRoot, envName, source)

	gen := variant.NewGenerator(source, target, envName)
	err := gen.Run()
	return gen.CreatedTarget(), err
}

// azureSubnets holds the computed subnet CIDRs for an Azure deployment.
type azureSubnets struct {
	Bastion    string
	Controller string
	Kali       string
}

// deriveAzureSubnets computes bastion, controller, and kali subnet CIDRs from
// a /16 VNet CIDR. Given "10.X.0.0/16" it produces:
//
//	bastion:    10.X.2.0/26  (64 IPs, required by Azure Bastion)
//	controller: 10.X.3.0/28  (16 IPs, single Ansible controller)
//	kali:       10.X.4.0/28  (16 IPs, optional attack box)
func deriveAzureSubnets(vnetCIDR string) (azureSubnets, error) {
	_, ipnet, err := net.ParseCIDR(vnetCIDR)
	if err != nil {
		return azureSubnets{}, fmt.Errorf("invalid VNet CIDR %q: %w", vnetCIDR, err)
	}
	ones, _ := ipnet.Mask.Size()
	if ones != 16 {
		return azureSubnets{}, fmt.Errorf("VNet CIDR must be a /16, got /%d", ones)
	}
	base := ipnet.IP.To4()
	if base == nil {
		return azureSubnets{}, fmt.Errorf("VNet CIDR must be IPv4, got %q", vnetCIDR)
	}
	return azureSubnets{
		Bastion:    fmt.Sprintf("%d.%d.2.0/26", base[0], base[1]),
		Controller: fmt.Sprintf("%d.%d.3.0/28", base[0], base[1]),
		Kali:       fmt.Sprintf("%d.%d.4.0/28", base[0], base[1]),
	}, nil
}

// azureInstanceSize returns the Azure VM size for a host role.
// dc02 gets extra memory for the recurring attack-simulation tasks.
func azureInstanceSize(role string) string {
	if role == "dc02" {
		return "Standard_D4s_v3"
	}
	return "Standard_D2s_v3"
}

// createAzureEnvHCL writes an Azure-specific env.hcl with VNet, bastion,
// controller, and kali subnet CIDRs auto-derived from the VNet CIDR.
// hostFilter determines which hosts appear in goad_instance_sizes; nil means
// the full 5-host GOAD topology.
func createAzureEnvHCL(envDir, envName, vnetCIDR string, hostFilter map[string]bool) error {
	subnets, err := deriveAzureSubnets(vnetCIDR)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		return err
	}

	hosts := hostFilter
	if hosts == nil {
		hosts = map[string]bool{"dc01": true, "dc02": true, "dc03": true, "srv02": true, "srv03": true}
	}
	// Build the instance_sizes map in sorted order for deterministic output.
	sorted := make([]string, 0, len(hosts))
	for h := range hosts {
		sorted = append(sorted, h)
	}
	sort.Strings(sorted)
	var sizeLines strings.Builder
	for _, h := range sorted {
		fmt.Fprintf(&sizeLines, "    %-5s = %q\n", h, azureInstanceSize(h))
	}

	content := fmt.Sprintf(`locals {
  deployment_name = "dreadgoad"
  env             = %q
  vnet_cidr       = %q

  # DC02 gets extra memory for the recurring attack-simulation tasks.
  goad_instance_sizes = {
%s  }

  bastion_sku               = "Standard"
  bastion_subnet_cidr       = %q
  bastion_tunneling_enabled = true

  controller_subnet_cidr               = %q
  controller_ssh_source_address_prefix = %q
  controller_instance_size = "Standard_D2s_v3"

  # Optional Kali attack box. Enable with --with-kali on infra commands.
  kali_subnet_cidr               = %q
  kali_ssh_source_address_prefix = %q
  kali_instance_size             = "Standard_D2s_v3"
}
`, envName, vnetCIDR, sizeLines.String(), subnets.Bastion, subnets.Controller, subnets.Bastion,
		subnets.Kali, subnets.Bastion)
	return os.WriteFile(filepath.Join(envDir, "env.hcl"), []byte(content), 0o644)
}

// createAzureRegionHCL writes an Azure region.hcl with the location field.
func createAzureRegionHCL(regionDir, location string) error {
	if err := os.MkdirAll(regionDir, 0o755); err != nil {
		return err
	}
	content := fmt.Sprintf(`locals {
  location = %q
}
`, location)
	return os.WriteFile(filepath.Join(regionDir, "region.hcl"), []byte(content), 0o644)
}

// generateAzureInventory creates an inventory file for Azure by copying the
// reference inventory and resetting host IPs to PENDING.
func generateAzureInventory(projectRoot, envName, reference string) error {
	refInvPath, err := resolveReferenceInventory(projectRoot, reference)
	if err != nil {
		// Fall back to the base GOAD Azure provider inventory template.
		fallback := filepath.Join(projectRoot, "ad", "GOAD", "providers", "azure", "inventory")
		if _, ferr := os.Stat(fallback); ferr != nil {
			return err // return original error
		}
		refInvPath = fallback
	}
	dstInvPath := filepath.Join(projectRoot, envName+"-inventory")

	data, err := os.ReadFile(refInvPath)
	if err != nil {
		return fmt.Errorf("read reference inventory %s: %w", filepath.Base(refInvPath), err)
	}
	content := string(data)

	envRe := regexp.MustCompile(`(?m)^(\s*env=).+$`)
	content = envRe.ReplaceAllStringFunc(content, func(match string) string {
		eq := strings.Index(match, "=")
		return match[:eq+1] + envName
	})

	ipRe := regexp.MustCompile(`(ansible_host=)\S+`)
	content = ipRe.ReplaceAllString(content, "${1}PENDING")

	return os.WriteFile(dstInvPath, []byte(content), 0o644)
}
