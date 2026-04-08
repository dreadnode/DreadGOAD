package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/cowdogmoo/warpgate/v3/builder"
	"github.com/cowdogmoo/warpgate/v3/builder/ami"
	warpconfig "github.com/cowdogmoo/warpgate/v3/config"
	warplog "github.com/cowdogmoo/warpgate/v3/logging"
	"github.com/cowdogmoo/warpgate/v3/progress"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

var amiCmd = &cobra.Command{
	Use:   "ami",
	Short: "AMI image management",
}

var amiCleanResourcesCmd = &cobra.Command{
	Use:   "clean-resources [template]",
	Short: "Remove Image Builder pipeline resources (not AMIs)",
	Long: `Delete EC2 Image Builder pipeline resources (components, recipes, pipelines,
infrastructure configs, distribution configs) left behind by warpgate builds.
Does NOT delete the built AMIs themselves.

Without arguments, removes all warpgate pipeline resources.
With a template name, only removes resources for that specific build.`,
	RunE: runAMICleanResources,
}

var amiListResourcesCmd = &cobra.Command{
	Use:   "list-resources",
	Short: "List Image Builder pipeline resources created by warpgate",
	Long: `Lists all EC2 Image Builder pipeline resources tagged with warpgate metadata.
These are the intermediate build resources (components, recipes, pipelines),
not the resulting AMIs.`,
	RunE: runAMIListResources,
}

var amiListCmd = &cobra.Command{
	Use:   "list",
	Short: "List AMIs built by warpgate",
	Long: `Lists AMIs owned by your account that were built by warpgate.
Filters to AMIs tagged with warpgate:name by default.`,
	RunE: runAMIList,
}

var amiDeleteCmd = &cobra.Command{
	Use:   "delete <ami-id> [ami-id...]",
	Short: "Deregister AMIs and optionally delete their snapshots",
	Long: `Deregister one or more AMIs built by warpgate and optionally delete
associated EBS snapshots. Requires at least one AMI ID.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAMIDelete,
}

var amiBuildCmd = &cobra.Command{
	Use:   "build [template]",
	Short: "Build an AMI from a warpgate template",
	Long: `Build an AMI using EC2 Image Builder from a warpgate template.

Template can be:
  - A template name (e.g. "goad-dc-base") from warpgate-templates/
  - A path to a warpgate.yaml file or directory containing one
  - Omitted with --all to build all templates in warpgate-templates/

With --all, builds run in parallel. Shows a progress bar per build
by default. Use --debug for detailed build output.`,
	RunE: runAMIBuild,
}

func init() {
	rootCmd.AddCommand(amiCmd)
	amiCmd.AddCommand(amiBuildCmd)
	amiCmd.AddCommand(amiListCmd)
	amiCmd.AddCommand(amiDeleteCmd)
	amiCmd.AddCommand(amiCleanResourcesCmd)
	amiCmd.AddCommand(amiListResourcesCmd)

	amiBuildCmd.Flags().String("region", "", "AWS region (overrides template)")
	amiBuildCmd.Flags().String("instance-type", "", "EC2 instance type (overrides template)")
	amiBuildCmd.Flags().String("profile", "", "AWS profile")
	amiBuildCmd.Flags().String("instance-profile", "", "IAM instance profile for EC2 Image Builder")
	amiBuildCmd.Flags().Bool("reuse-resources", false, "Reuse existing Image Builder resources instead of recreating")
	amiBuildCmd.Flags().Bool("all", false, "Build all templates in warpgate-templates/")

	amiListCmd.Flags().String("region", "", "AWS region")
	amiListCmd.Flags().String("profile", "", "AWS profile")
	amiListCmd.Flags().String("filter-name", "", "Filter by warpgate build name")

	amiDeleteCmd.Flags().String("region", "", "AWS region")
	amiDeleteCmd.Flags().String("profile", "", "AWS profile")
	amiDeleteCmd.Flags().Bool("delete-snapshots", true, "Delete associated EBS snapshots")
	amiDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	amiCleanResourcesCmd.Flags().String("region", "", "AWS region")
	amiCleanResourcesCmd.Flags().String("profile", "", "AWS profile")
	amiCleanResourcesCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	amiListResourcesCmd.Flags().String("region", "", "AWS region")
	amiListResourcesCmd.Flags().String("profile", "", "AWS profile")
}

func resolveTemplates(cfg *config.Config, args []string, buildAll bool) ([]string, error) {
	if !buildAll && len(args) == 0 {
		return nil, fmt.Errorf("requires a template argument or --all flag")
	}
	if buildAll && len(args) > 0 {
		return nil, fmt.Errorf("--all flag cannot be used with a template argument")
	}
	if buildAll {
		templates, err := discoverWarpgateTemplates(cfg.ProjectRoot)
		if err != nil {
			return nil, err
		}
		if len(templates) == 0 {
			return nil, fmt.Errorf("no templates found in warpgate-templates/")
		}
		return templates, nil
	}
	p, err := resolveTemplatePath(cfg, args[0])
	if err != nil {
		return nil, err
	}
	return []string{p}, nil
}

func runAMIBuild(cmd *cobra.Command, args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cfg, err := config.Get()
	if err != nil {
		return err
	}

	buildAll, _ := cmd.Flags().GetBool("all")
	templates, err := resolveTemplates(cfg, args, buildAll)
	if err != nil {
		return err
	}

	verbose := viper.GetBool("debug")

	// Region precedence for `ami build`: --region flag > cfg.Region (from
	// dreadgoad.yaml or DREADGOAD_REGION) > the template's own embedded region.
	// The empty fallback (instead of a hardcoded default) is what allows the
	// template's embedded region to win when neither --region nor cfg.Region
	// is set; if either is set, it overrides the template even though the
	// user didn't ask for this specific build.
	bf := buildFlags{
		region:          getFlagString(cmd, "region", cfg.Region, ""),
		instanceType:    getFlagStringOpt(cmd, "instance-type"),
		profile:         getFlagStringOpt(cmd, "profile"),
		instanceProfile: getFlagStringOpt(cmd, "instance-profile"),
		reuseResources:  getFlagBool(cmd, "reuse-resources"),
	}

	// Set up progress display — add all bars before starting the render loop
	// to avoid partial renders with mismatched ANSI cursor-up counts.
	display := progress.NewDisplay(os.Stderr)
	bars := make([]*progress.Bar, len(templates))
	for i, tmplPath := range templates {
		bars[i] = display.AddBar(templateName(tmplPath), i+1, len(templates))
	}
	if !verbose {
		display.Start(500 * time.Millisecond)
	}

	results := make([]amiBuildResult, len(templates))
	var wg sync.WaitGroup

	for i, tmplPath := range templates {
		wg.Add(1)

		go func(idx int, path string, bar *progress.Bar) {
			defer wg.Done()
			result, buildErr := buildSingleAMI(ctx, cfg, path, bf, bar, verbose)
			if buildErr != nil {
				results[idx] = amiBuildResult{template: path, err: buildErr}
			} else {
				results[idx] = *result
			}
		}(i, tmplPath, bars[i])
	}

	wg.Wait()

	if !verbose {
		display.Stop()
	}

	fmt.Fprintln(os.Stderr)
	printBuildSummary(results)

	for _, r := range results {
		if r.err != nil {
			return fmt.Errorf("one or more builds failed")
		}
	}
	return nil
}

type buildFlags struct {
	region          string
	instanceType    string
	profile         string
	instanceProfile string
	reuseResources  bool
}

type amiBuildResult struct {
	template string
	amiID    string
	region   string
	duration string
	err      error
}

func buildSingleAMI(ctx context.Context, cfg *config.Config, templatePath string, bf buildFlags, bar *progress.Bar, verbose bool) (*amiBuildResult, error) {
	tmplName := templateName(templatePath)
	buildCfg, err := loadWarpgateTemplate(templatePath, cfg.ProjectRoot)
	if err != nil {
		bar.Fail()
		return nil, fmt.Errorf("load template %s: %w", tmplName, err)
	}

	for i := range buildCfg.Targets {
		if buildCfg.Targets[i].Type != "ami" {
			continue
		}
		if bf.region != "" {
			buildCfg.Targets[i].Region = bf.region
		}
		if bf.instanceType != "" {
			buildCfg.Targets[i].InstanceType = bf.instanceType
		}
		if bf.instanceProfile != "" {
			buildCfg.Targets[i].InstanceProfileName = bf.instanceProfile
		}
	}

	clientCfg := ami.ClientConfig{
		Region:  bf.region,
		Profile: bf.profile,
	}

	forceRecreate := !bf.reuseResources

	// Set up warpgate logger — quiet mode suppresses info logs that break progress bars
	var warpLogger *warplog.CustomLogger
	if verbose {
		warpLogger = warplog.NewCustomLoggerWithOptions("debug", "color", false, true)
		warpLogger.ConsoleWriter = os.Stderr
	} else {
		warpLogger = warplog.NewCustomLoggerWithOptions("error", "plain", true, false)
		warpLogger.ConsoleWriter = os.Stderr
		bar.Update("Initializing", 0.01, 0, 0)
	}

	ctx = warplog.WithLogger(ctx, warpLogger)

	monitorCfg := ami.MonitorConfig{
		StreamLogs:    verbose,
		ShowEC2Status: verbose,
		StatusCallback: func(update ami.StatusUpdate) {
			if !verbose {
				bar.Update(update.Stage, update.Progress, update.Elapsed, update.EstimatedRemaining)
			}
		},
	}

	imgBuilder, err := ami.NewImageBuilderWithAllOptions(ctx, clientCfg, forceRecreate, monitorCfg)
	if err != nil {
		bar.Fail()
		return nil, fmt.Errorf("create AMI builder for %s: %w", tmplName, err)
	}
	defer func() { _ = imgBuilder.Close() }()

	result, err := imgBuilder.Build(ctx, *buildCfg)
	if err != nil {
		bar.Fail()
		return nil, fmt.Errorf("%s failed: %w", tmplName, err)
	}

	bar.CompleteWithMessage(result.AMIID)

	return &amiBuildResult{
		template: templatePath,
		amiID:    result.AMIID,
		region:   result.Region,
		duration: result.Duration,
	}, nil
}

func templateName(path string) string {
	return filepath.Base(filepath.Dir(path))
}

func getFlagString(cmd *cobra.Command, name, fallback1, fallback2 string) string {
	if v, _ := cmd.Flags().GetString(name); v != "" {
		return v
	}
	if fallback1 != "" {
		return fallback1
	}
	return fallback2
}

func getFlagStringOpt(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func getFlagBool(cmd *cobra.Command, name string) bool {
	v, _ := cmd.Flags().GetBool(name)
	return v
}

func newAWSClients(cmd *cobra.Command, cfg *config.Config) (*ami.AWSClients, error) {
	// For ami list-resources / clean-resources there is no warpgate template fallback,
	// so a region is required. Local --region flag wins over cfg.Region;
	// otherwise we error out via ResolveRegion rather than silently picking one.
	region := getFlagStringOpt(cmd, "region")
	if region == "" {
		var err error
		region, err = cfg.ResolveRegion()
		if err != nil {
			return nil, err
		}
	}
	profile := getFlagStringOpt(cmd, "profile")
	return ami.NewAWSClients(context.Background(), ami.ClientConfig{
		Region:  region,
		Profile: profile,
	})
}

func newAMIOperations(cmd *cobra.Command, cfg *config.Config) (*ami.AMIOperations, error) {
	clients, err := newAWSClients(cmd, cfg)
	if err != nil {
		return nil, err
	}
	return ami.NewAMIOperations(clients, &warpconfig.Config{}), nil
}

func runAMIList(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	ops, err := newAMIOperations(cmd, cfg)
	if err != nil {
		return fmt.Errorf("create AWS clients: %w", err)
	}

	filters := map[string]string{
		"tag-key": "warpgate:name",
	}
	if name := getFlagStringOpt(cmd, "filter-name"); name != "" {
		filters["tag:warpgate:name"] = name
		delete(filters, "tag-key")
	}

	ctx := context.Background()
	images, err := ops.ListAMIs(ctx, filters)
	if err != nil {
		return fmt.Errorf("list AMIs: %w", err)
	}

	if len(images) == 0 {
		color.Green("No warpgate AMIs found.")
		return nil
	}

	sort.Slice(images, func(i, j int) bool {
		return aws.ToString(images[i].CreationDate) > aws.ToString(images[j].CreationDate)
	})

	fmt.Printf("\nFound %d warpgate AMIs:\n\n", len(images))
	fmt.Printf("  %-23s %-40s %-15s %s\n", "AMI ID", "NAME", "STATE", "CREATED")
	fmt.Printf("  %-23s %-40s %-15s %s\n", "------", "----", "-----", "-------")
	for _, img := range images {
		name := amiTagValue(img.Tags, "warpgate:name")
		if name == "" {
			name = aws.ToString(img.Name)
		}
		created := aws.ToString(img.CreationDate)
		if len(created) > 16 {
			created = created[:16]
		}
		fmt.Printf("  %-23s %-40s %-15s %s\n",
			aws.ToString(img.ImageId),
			name,
			img.State,
			created,
		)
	}
	fmt.Println()
	return nil
}

func amiTagValue(tags []ec2types.Tag, key string) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			return aws.ToString(t.Value)
		}
	}
	return ""
}

func runAMIDelete(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	ops, err := newAMIOperations(cmd, cfg)
	if err != nil {
		return fmt.Errorf("create AWS clients: %w", err)
	}

	deleteSnapshots := getFlagBool(cmd, "delete-snapshots")
	skipConfirm := getFlagBool(cmd, "yes")

	ctx := context.Background()

	// Show what will be deleted
	fmt.Printf("\nAMIs to deregister (%d):\n\n", len(args))
	for _, id := range args {
		img, getErr := ops.GetAMI(ctx, id)
		if getErr != nil {
			fmt.Printf("  %-23s (not found: %s)\n", id, getErr)
			continue
		}
		name := amiTagValue(img.Tags, "warpgate:name")
		if name == "" {
			name = aws.ToString(img.Name)
		}
		fmt.Printf("  %-23s %s\n", aws.ToString(img.ImageId), name)
	}
	fmt.Println()

	if deleteSnapshots {
		color.Yellow("Associated EBS snapshots will also be deleted.")
	}

	if !skipConfirm {
		fmt.Print("\nProceed? [y/N] ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil {
			return fmt.Errorf("read input: %w", err)
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Println()
	var failed bool
	for _, id := range args {
		if err := ops.DeregisterAMI(ctx, id, deleteSnapshots); err != nil {
			color.Red("  x %-23s %s", id, err)
			failed = true
		} else {
			color.Green("  + %-23s deregistered", id)
		}
	}
	fmt.Println()

	if failed {
		return fmt.Errorf("one or more AMIs failed to deregister")
	}
	color.Green("Done.")
	return nil
}

func runAMIListResources(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	clients, err := newAWSClients(cmd, cfg)
	if err != nil {
		return fmt.Errorf("create AWS clients: %w", err)
	}

	cleaner := ami.NewResourceCleaner(clients)
	ctx := context.Background()

	resources, err := cleaner.ListWarpgateResources(ctx)
	if err != nil {
		return fmt.Errorf("list resources: %w", err)
	}

	if len(resources) == 0 {
		color.Green("No warpgate pipeline resources found.")
		return nil
	}

	fmt.Printf("\nFound %d pipeline resources:\n\n", len(resources))
	fmt.Printf("  %-30s %-25s %-10s %s\n", "TYPE", "NAME", "VERSION", "BUILD")
	fmt.Printf("  %-30s %-25s %-10s %s\n", "----", "----", "-------", "-----")
	for _, r := range resources {
		version := r.Version
		if version == "" {
			version = "-"
		}
		buildName := r.BuildName
		if buildName == "" {
			buildName = "-"
		}
		fmt.Printf("  %-30s %-25s %-10s %s\n", r.Type, r.Name, version, buildName)
	}
	fmt.Println()
	return nil
}

func runAMICleanResources(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	clients, err := newAWSClients(cmd, cfg)
	if err != nil {
		return fmt.Errorf("create AWS clients: %w", err)
	}

	cleaner := ami.NewResourceCleaner(clients)
	ctx := context.Background()

	var resources []ami.ResourceInfo
	if len(args) > 0 {
		resources, err = cleaner.ListResourcesForBuild(ctx, args[0])
		if err != nil {
			return fmt.Errorf("list resources: %w", err)
		}
		resources = filterExactBuild(resources, args[0])
	} else {
		resources, err = cleaner.ListWarpgateResources(ctx)
		if err != nil {
			return fmt.Errorf("list resources: %w", err)
		}
	}

	if len(resources) == 0 {
		color.Green("No warpgate pipeline resources found.")
		return nil
	}

	fmt.Printf("\nPipeline resources to delete (%d):\n\n", len(resources))
	for _, r := range resources {
		fmt.Printf("  %-30s %s\n", r.Type, r.Name)
	}
	fmt.Println()
	color.Yellow("NOTE: This deletes pipeline resources only, NOT the built AMIs.")

	skipConfirm, _ := cmd.Flags().GetBool("yes")
	if !skipConfirm {
		fmt.Print("\nProceed? [y/N] ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil {
			return fmt.Errorf("read input: %w", err)
		}
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Println()
	if err := cleaner.DeleteResources(ctx, resources); err != nil {
		return fmt.Errorf("clean-resources failed: %w", err)
	}

	color.Green("Clean-resources complete.")
	return nil
}

func resolveTemplatePath(cfg *config.Config, arg string) (string, error) {
	if info, err := os.Stat(arg); err == nil {
		if info.IsDir() {
			p := filepath.Join(arg, "warpgate.yaml")
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
			return "", fmt.Errorf("no warpgate.yaml in directory: %s", arg)
		}
		return arg, nil
	}

	p := filepath.Join(cfg.ProjectRoot, "warpgate-templates", arg, "warpgate.yaml")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("template not found: %s (tried as path and in warpgate-templates/)", arg)
}

func discoverWarpgateTemplates(projectRoot string) ([]string, error) {
	dir := filepath.Join(projectRoot, "warpgate-templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read warpgate-templates: %w", err)
	}

	var templates []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p := filepath.Join(dir, entry.Name(), "warpgate.yaml")
		if _, err := os.Stat(p); err == nil {
			templates = append(templates, p)
		}
	}
	return templates, nil
}

type templateWithVars struct {
	Variables map[string]string `yaml:"variables"`
}

func loadWarpgateTemplate(path, projectRoot string) (*builder.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var tmpl templateWithVars
	_ = yaml.Unmarshal(data, &tmpl)

	content := string(data)

	for k, v := range tmpl.Variables {
		content = strings.ReplaceAll(content, "${"+k+"}", v)
	}

	if _, ok := os.LookupEnv("PROVISION_REPO_PATH"); !ok && projectRoot != "" {
		if err := os.Setenv("PROVISION_REPO_PATH", projectRoot); err != nil {
			return nil, fmt.Errorf("set PROVISION_REPO_PATH: %w", err)
		}
	}

	content = envVarPattern.ReplaceAllStringFunc(content, func(match string) string {
		varName := match[2 : len(match)-1]
		if val, ok := os.LookupEnv(varName); ok {
			return val
		}
		return match
	})

	var cfg builder.Config
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	return &cfg, nil
}

// filterExactBuild removes resources that don't belong to the specified build.
// This works around warpgate's HasPrefix matching in ListResourcesForBuild
// which incorrectly includes e.g. "goad-dc-base-2016" when filtering for "goad-dc-base".
func filterExactBuild(resources []ami.ResourceInfo, buildName string) []ami.ResourceInfo {
	var exact []ami.ResourceInfo
	for _, r := range resources {
		if r.BuildName == buildName {
			exact = append(exact, r)
			continue
		}
		// Also match resources that have no BuildName tag but whose name
		// matches exactly (e.g. infra configs named just "goad-dc-base").
		if r.BuildName == "" && r.Name == buildName {
			exact = append(exact, r)
			continue
		}
	}
	return exact
}

func printBuildSummary(results []amiBuildResult) {
	for _, r := range results {
		name := filepath.Base(filepath.Dir(r.template))
		if r.err != nil {
			_, _ = color.New(color.FgRed).Fprintf(os.Stderr, "  x %-25s FAILED: %s\n", name, r.err)
		} else {
			_, _ = color.New(color.FgGreen).Fprintf(os.Stderr, "  + %-25s %s (%s)\n", name, r.amiID, r.duration)
		}
	}
}
