package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"slices"

	"github.com/dreadnode/dreadgoad/internal/ansible"
	daws "github.com/dreadnode/dreadgoad/internal/aws"
	"github.com/dreadnode/dreadgoad/internal/azure"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/doctor"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/lab"
	"github.com/dreadnode/dreadgoad/internal/ludus"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/dreadnode/dreadgoad/internal/variant"
	"github.com/spf13/cobra"
)

// closableTunnel is the union return shape from maybeStartSOCKSTunnel. The
// Ludus and Azure paths wrap different transports but both expose a Close().
type closableTunnel interface{ Close() }

var provisionCmd = &cobra.Command{
	Use:   "provision",
	Short: "Run GOAD provisioning playbooks with retry logic",
	Long: `Runs Ansible playbooks to provision Active Directory infrastructure.

Executes the full playbook sequence (or a subset) with error-specific
retry strategies, SSM session management, and idle timeout monitoring.`,
	Example: `  dreadgoad provision
  dreadgoad provision --plays build.yml,ad-servers.yml
  dreadgoad provision --from ad-data.yml
  dreadgoad provision --env staging --debug
  dreadgoad provision --plays ad-data.yml --limit dc01
  dreadgoad provision --max-retries 5 --retry-delay 60`,
	RunE: runProvision,
}

var adUsersCmd = &cobra.Command{
	Use:   "ad-users",
	Short: "Ensure AD users exist (runs ad-data.yml)",
	RunE: func(cmd *cobra.Command, args []string) error {
		plays, _ := cmd.Flags().GetString("plays")
		if plays == "" {
			_ = cmd.Flags().Set("plays", "ad-data.yml")
		}
		return runProvision(cmd, args)
	},
}

func init() {
	rootCmd.AddCommand(provisionCmd)
	rootCmd.AddCommand(adUsersCmd)

	provisionCmd.Flags().String("plays", "", "Comma-separated playbooks to run (default: all)")
	provisionCmd.Flags().String("from", "", "Resume provisioning from this playbook onward")
	provisionCmd.Flags().String("limit", "", "Limit execution to specific hosts")
	provisionCmd.Flags().Int("max-retries", 0, "Max retry attempts (default: from config; 0 disables retries)")
	provisionCmd.Flags().Int("retry-delay", 0, "Delay between retries in seconds (default: from config; 0 disables delay)")
	provisionCmd.Flags().StringArrayP("extra-vars", "E", nil, extraVarsUsage)
	provisionCmd.MarkFlagsMutuallyExclusive("plays", "from")

	adUsersCmd.Flags().String("plays", "ad-data.yml", "Playbooks to run")
	adUsersCmd.Flags().String("limit", "", "Limit execution to specific hosts")
	adUsersCmd.Flags().Int("max-retries", 0, "Max retry attempts (0 disables retries)")
	adUsersCmd.Flags().Int("retry-delay", 0, "Delay between retries in seconds (0 disables delay)")
	adUsersCmd.Flags().StringArrayP("extra-vars", "E", nil, extraVarsUsage)
}

// extraVarsUsage is shared so the flag reads identically everywhere it appears.
const extraVarsUsage = "Ansible variable as key=value, repeatable " +
	"(e.g. -E ad_reconcile_check_only=true to report drift instead of correcting it)"

func resolvePlaybooks(cfg *config.Config, playsFlag, fromFlag string) ([]string, error) {
	if playsFlag != "" && fromFlag != "" {
		return nil, fmt.Errorf("--plays and --from are mutually exclusive")
	}

	var playbooks []string
	if playsFlag != "" {
		playbooks = strings.Split(playsFlag, ",")
	} else {
		playbooks = lab.PlaybooksForLab(cfg.ProjectRoot, "", cfg.Playbooks)
	}

	if fromFlag == "" {
		return playbooks, nil
	}

	for i, p := range playbooks {
		if p == fromFlag {
			return playbooks[i:], nil
		}
	}
	return nil, fmt.Errorf("playbook %q not found in playbook list: %v", fromFlag, playbooks)
}

func ensureVariant(cfg *config.Config) error {
	envCfg := cfg.ActiveEnvironment()
	if !envCfg.Variant {
		return nil
	}
	source, target := cfg.ResolvedVariantPaths()
	variantName := envCfg.VariantName
	if variantName == "" {
		variantName = "variant-1"
	}
	info, err := os.Stat(target)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("variant target exists but is not a directory: %s", target)
		}
		complete, err := variant.IsComplete(target)
		if err != nil {
			return fmt.Errorf("inspect variant target %s: %w", target, err)
		}
		if !complete {
			return fmt.Errorf("variant directory is incomplete (missing %s): %s; move or remove it, then rerun provisioning", variant.CompletionMarkerName, target)
		}
		slog.Info("Variant directory already exists, skipping generation", "target", target)
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect variant target %s: %w", target, err)
	}
	fmt.Printf("Environment %q has variant=true, generating variant...\n", cfg.Env)
	gen := variant.NewGenerator(source, target, variantName)
	if err := gen.Run(); err != nil {
		return fmt.Errorf("auto variant generation failed: %w", err)
	}
	fmt.Printf("Variant generated: %s\n", target)
	return nil
}

// isSSMInventory checks whether the current inventory uses AWS SSM connections.
// Returns false (non-SSM) if the inventory does not exist or cannot be parsed,
// so that non-AWS providers are never blocked by AWS-specific operations.
func isSSMInventory(cfg *config.Config) bool {
	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return false
	}
	return parsed.IsSSM()
}

// preflightChecks validates tooling, builds the Ansible collection, and
// prepares artifacts needed before provisioning playbooks run. limit is the
// Ansible host pattern the run is restricted to, or "" for the whole inventory;
// it only affects how strictly the inventory is validated.
func preflightChecks(ctx context.Context, cfg *config.Config, limit string) error {
	if err := doctor.CheckAnsibleCoreVersion(cfg.ResolvedProvider()); err != nil {
		return fmt.Errorf("ansible-core version check failed: %w", err)
	}
	if err := ansible.InstallRequirements(cfg.ProjectRoot); err != nil {
		return fmt.Errorf("ansible dependency install failed: %w", err)
	}
	if err := ansible.BuildCollection(cfg.ProjectRoot); err != nil {
		return fmt.Errorf("collection build failed: %w", err)
	}
	if err := ensureVariant(cfg); err != nil {
		return err
	}

	// Bootstrap the inventory file for all providers. For non-AWS providers
	// (Ludus, Proxmox) this renders the provider-specific template. For AWS
	// it copies from the .example template. This must happen before the
	// SSM-specific checks below, which depend on the inventory existing.
	if err := bootstrapInventory(cfg.InventoryPath()); err != nil {
		return fmt.Errorf("inventory bootstrap failed: %w", err)
	}

	// AWS-specific preflight: ensure the SSM transfer bucket exists, sync
	// inventory instance IDs, and generate IP mappings. Skipped for non-SSM
	// providers (Ludus, Proxmox, etc.) where none of this applies.
	if isSSMInventory(cfg) {
		if err := ensureSSMBucket(ctx, cfg); err != nil {
			slog.Warn("SSM bucket check failed", "error", err)
		}
		if err := ensureInventorySynced(ctx, cfg); err != nil {
			slog.Warn("inventory sync check failed", "error", err)
		}
		if err := generateInstanceMapping(ctx, ""); err != nil {
			slog.Warn("instance mapping generation failed, playbooks will use runtime detection", "error", err)
		}
	}

	// Azure: `env create` writes the inventory with PENDING addresses and no
	// other step fills them in, so resolve them from live NIC state here.
	// Failing here beats failing inside network_setup.yml once the Bastion
	// tunnel and playbook run are already underway.
	if cfg.ResolvedProvider() == provider.NameAzure {
		if err := inventorySyncFailure(syncAzureInventoryIPs(ctx, cfg), limit); err != nil {
			return err
		}
		if err := inventorySyncFailure(syncAzureInventoryPasswords(cfg), limit); err != nil {
			return err
		}
	}

	// Last gates before any playbook runs, for every provider.
	if err := validateInventoryResolved(cfg, limit); err != nil {
		return err
	}
	return validateInventoryCredentials(cfg)
}

// materializedLabConfigPath is the lab config Terraform actually read when it
// built the machines: infra_cmd.go's materializeLabConfig copies the resolved
// config here, and every Azure goad unit hardcodes this path to source
// admin_password. It is deliberately NOT cfg.ResolvedLabConfigPath() — that is
// what the *playbooks* will read, and the two can disagree, which is precisely
// the failure this check exists to catch.
func materializedLabConfigPath(cfg *config.Config) string {
	return filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "data", cfg.Env+"-config.json")
}

// validateInventoryCredentials checks that the password Ansible will present
// is the one the machines were actually built with.
//
// The Azure bootstrap creates the login Ansible uses with
// `net user ansible '${admin_password}'`, where admin_password comes from
// lab.hosts[<id>].local_admin_password in the materialized lab config. If the
// inventory carries a different value — because it was scaffolded from a
// provider template whose stock passwords were never reconciled with the
// generated config — every host fails WinRM auth. That surfaces as a wall of
// authentication errors with no hint that the inventory is the cause.
//
// Only a total mismatch is fatal. That is the unambiguous scaffolding bug, and
// it is what a broken environment looks like: a healthy one matches on every
// host. A partial mismatch is reported but allowed through, since a single host
// can legitimately drift after provisioning has already run.
func validateInventoryCredentials(cfg *config.Config) error {
	// Azure only. The link between lab.hosts[*].local_admin_password and the
	// account Ansible logs in as is Azure's bootstrap script; no other provider
	// makes that promise. AWS's stock inventory carries passwords that match no
	// config at all — it authenticates over SSM and never sends them — so
	// comparing there would block a working provider on a value nothing uses.
	if cfg.ResolvedProvider() != provider.NameAzure {
		return nil
	}
	want, err := materializedHostPasswords(cfg)
	if err != nil || len(want) == 0 {
		// No materialized config (infra never ran here) means there is nothing
		// to compare against. Absence is not a mismatch.
		slog.Debug("skipping credential check; no materialized lab config", "error", err)
		return nil
	}
	configPath := materializedLabConfigPath(cfg)

	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return nil // validateInventoryResolved already reported on this file
	}

	var mismatched []string
	compared := 0
	for name, host := range parsed.Hosts {
		expected, ok := want[strings.ToLower(name)]
		if !ok || host.Password == "" {
			continue
		}
		compared++
		if host.Password != expected {
			mismatched = append(mismatched, name)
		}
	}
	if compared == 0 || len(mismatched) == 0 {
		return nil
	}
	sort.Strings(mismatched)

	if len(mismatched) < compared {
		slog.Warn("some hosts' inventory password differs from the one they were built with",
			"hosts", strings.Join(mismatched, ","), "of", compared)
		return nil
	}
	return fmt.Errorf(
		"inventory %s has the wrong password for every host (%s)\n"+
			"  The machines were built with lab.hosts[*].local_admin_password from %s,\n"+
			"  but the inventory carries values scaffolded from a provider template.\n"+
			"  Ansible would fail WinRM authentication on all %d hosts.\n"+
			"  Fix: dreadgoad --env %s inventory sync",
		cfg.InventoryPath(), strings.Join(mismatched, ", "), configPath, compared, cfg.Env)
}

// inventorySyncFailure decides whether a failed inventory sync stops the run.
//
// Under --limit it must not. The sync fails when some host cannot be resolved,
// but a limited run may never target that host, and validateInventoryResolved
// applies the same policy a few lines later — so letting the sync hard-fail
// here would silently override the limit and block a legitimate partial run.
func inventorySyncFailure(err error, limit string) error {
	if err == nil {
		return nil
	}
	if limit != "" {
		slog.Warn("inventory sync did not resolve every host; continuing because the run is limited",
			"limit", limit, "error", err)
		return nil
	}
	return fmt.Errorf("inventory sync: %w", err)
}

// validateInventoryResolved refuses to hand Ansible an inventory that still
// carries scaffolding placeholders.
//
// Ansible does not validate ansible_host. Given "PENDING" it tries to resolve a
// host by that literal name and reports every play "unreachable" — which reads
// as a network, firewall, or credential fault and costs an apply cycle to trace
// back to the inventory.
//
// This runs for all providers rather than just the one that scaffolds PENDING,
// because each arrives here unresolved by a different route: Azure had no
// resolver at all, the AWS sync is warn-only at its call site above, and a
// Ludus or Proxmox inventory that already exists on disk is never re-rendered,
// so an unrendered {{ip_range}} survives bootstrap untouched.
//
// Under --limit an unresolved host may simply be out of scope, so this warns
// rather than fails: blocking a deliberate partial run would be worse than the
// unreachable error the operator gets anyway if the host is in scope.
func validateInventoryResolved(cfg *config.Config, limit string) error {
	data, err := os.ReadFile(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("read inventory: %w", err)
	}
	stale := placeholderHosts(string(data))
	if len(stale) == 0 {
		return nil
	}
	if limit != "" {
		slog.Warn("inventory has unresolved hosts; they will fail if the limit selects them",
			"hosts", strings.Join(stale, ","), "limit", limit)
		return nil
	}
	return fmt.Errorf(
		"inventory %s has no address for %s\n"+
			"  Ansible would treat the placeholder as a hostname and report these unreachable.\n"+
			"  Run `dreadgoad --env %s infra apply` if the machines are not up yet,\n"+
			"  then `dreadgoad --env %s inventory sync` to resolve their addresses",
		cfg.InventoryPath(), strings.Join(stale, ", "), cfg.Env, cfg.Env)
}

// materializedHostPasswords returns the local admin password each machine was
// built with, keyed by lowercased host id, read from the lab config Terraform
// actually consumed.
func materializedHostPasswords(cfg *config.Config) (map[string]string, error) {
	raw, err := os.ReadFile(materializedLabConfigPath(cfg))
	if err != nil {
		return nil, err
	}
	var lab struct {
		Lab struct {
			Hosts map[string]struct {
				LocalAdminPassword string `json:"local_admin_password"`
			} `json:"hosts"`
		} `json:"lab"`
	}
	if err := json.Unmarshal(raw, &lab); err != nil {
		return nil, fmt.Errorf("parse lab config: %w", err)
	}
	out := make(map[string]string, len(lab.Lab.Hosts))
	for name, h := range lab.Lab.Hosts {
		if h.LocalAdminPassword != "" {
			out[strings.ToLower(name)] = h.LocalAdminPassword
		}
	}
	return out, nil
}

// quoteInventoryValue wraps a value so the inventory parser reads it back
// intact. Reports false when the value contains both quote characters, which
// inventory.stripQuotes cannot represent — better to leave that host alone than
// to write a line that parses back as something else.
func quoteInventoryValue(v string) (string, bool) {
	if !strings.Contains(v, "'") {
		return "'" + v + "'", true
	}
	if !strings.Contains(v, `"`) {
		return `"` + v + `"`, true
	}
	return "", false
}

// syncAzureInventoryPasswords rewrites each host's ansible_password to the one
// its machine was actually built with.
//
// Azure's bootstrap creates the account Ansible logs in as with
// `net user ansible '${admin_password}'`, sourced from
// lab.hosts[<id>].local_admin_password in the materialized lab config. The
// inventory is scaffolded from a provider template carrying stock passwords
// that appear in no config — measured at 0 of 5 agreement for every variant in
// this repo, including ones generated correctly. Nothing else reconciles the
// two, so provisioning authenticates with a password no machine has.
//
// Done here rather than at scaffold time so it also repairs ranges that are
// already deployed, and so a regenerated lab config cannot leave the inventory
// behind.
func syncAzureInventoryPasswords(cfg *config.Config) error {
	want, err := materializedHostPasswords(cfg)
	if err != nil || len(want) == 0 {
		slog.Debug("no materialized lab config; leaving inventory passwords alone", "error", err)
		return nil
	}

	invPath := cfg.InventoryPath()
	data, err := os.ReadFile(invPath)
	if err != nil {
		return fmt.Errorf("read inventory: %w", err)
	}
	content := string(data)

	updated := 0
	for host, password := range want {
		quoted, ok := quoteInventoryValue(password)
		if !ok {
			slog.Warn("cannot represent this host's password in the inventory; leaving it unchanged", "host", host)
			continue
		}
		re := regexp.MustCompile(
			`(?mi)^(` + regexp.QuoteMeta(host) + `\s+[^\n]*?ansible_password=)('[^']*'|"[^"]*"|\S+)`)
		// ReplaceAllStringFunc, not ReplaceAllString: a password may contain $,
		// which the replacement template would read as a capture reference.
		next := re.ReplaceAllStringFunc(content, func(m string) string {
			i := strings.Index(m, "ansible_password=")
			return m[:i+len("ansible_password=")] + quoted
		})
		if next != content {
			content = next
			updated++
		}
	}

	if updated == 0 {
		return nil
	}
	// Mode applies only on create; an existing inventory keeps its own.
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write inventory: %w", err)
	}
	fmt.Printf("Reconciled ansible_password for %d host(s) from %s\n",
		updated, filepath.Base(materializedLabConfigPath(cfg)))
	return nil
}

// syncAzureInventoryIPs points every inventory host at its live private IP.
// Azure allocates those from the subnet's pool at create time, so they are not
// knowable when the environment is scaffolded and cannot be baked into the
// provider inventory template the way Ludus and Proxmox ranges can.
func syncAzureInventoryIPs(ctx context.Context, cfg *config.Config) error {
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}
	live, err := prov.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}
	if len(live) == 0 {
		return fmt.Errorf("no instances found for env=%s: run 'dreadgoad infra apply' first", cfg.Env)
	}
	instances := make([]instanceInfo, 0, len(live))
	for _, i := range live {
		instances = append(instances, instanceInfo{InstanceID: i.ID, Name: i.Name, PrivateIP: i.PrivateIP})
	}
	return applyInstanceUpdates(cfg.InventoryPath(), instances)
}

// bootstrapInventory creates the inventory file if it does not exist.
// For AWS, it copies from the .example template.
// For Proxmox and other providers, it renders the provider-specific
// inventory template from ad/LAB/providers/PROVIDER/inventory.
func bootstrapInventory(invPath string) error {
	if _, err := os.Stat(invPath); err == nil {
		return nil
	}

	cfg, cfgErr := config.Get()
	if cfgErr == nil && !cfg.IsAWS() {
		if err := bootstrapFromProviderTemplate(invPath, cfg); err == nil {
			return nil
		}
	}

	return bootstrapFromExample(invPath)
}

func bootstrapFromProviderTemplate(invPath string, cfg *config.Config) error {
	providerName := cfg.ResolvedProvider()

	// Resolve the lab tree that holds the provider inventory template. For a
	// variant environment, read from the variant target tree so the
	// bootstrapped inventory (which carries domain_name and the asset layout)
	// points at the variant's ad/<target>/ assets rather than the stock
	// ad/GOAD/ tree. Falls back to the stock/proxmox path for non-variants.
	var templatePath string
	if ec := cfg.ActiveEnvironment(); ec.Variant {
		if _, target := cfg.ResolvedVariantPaths(); target != "" {
			templatePath = filepath.Join(target, "providers", providerName, "inventory")
		}
	}
	if templatePath == "" {
		labName := "GOAD"
		if providerName == "proxmox" {
			labName = cfg.ProxmoxLab()
		}
		templatePath = filepath.Join(cfg.ProjectRoot, "ad", labName, "providers", providerName, "inventory")
	}

	data, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}

	ipRange, err := resolveIPRange(cfg, providerName)
	if err != nil {
		return err
	}

	rendered := strings.ReplaceAll(string(data), "{{ip_range}}", ipRange)
	if err := os.WriteFile(invPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write inventory: %w", err)
	}
	slog.Info("bootstrapped inventory from provider template", "path", invPath, "provider", providerName)
	return nil
}

func resolveIPRange(cfg *config.Config, providerName string) (string, error) {
	if providerName == "ludus" {
		ctx := context.Background()
		if prov, err := cfg.NewProvider(ctx); err == nil {
			type ipRanger interface {
				IPRange(ctx context.Context) (string, error)
			}
			if lr, ok := prov.(ipRanger); ok {
				if r, err := lr.IPRange(ctx); err == nil {
					return r, nil
				}
			}
		}
		return "", fmt.Errorf("ludus range not deployed yet; run 'dreadgoad infra apply' first to get IP range")
	}

	ipRange := cfg.Proxmox.IPRange
	if ipRange == "" {
		ipRange = "192.168.10"
	}
	return ipRange, nil
}

func bootstrapFromExample(invPath string) error {
	examplePath := invPath + ".example"
	if _, err := os.Stat(examplePath); err != nil {
		return fmt.Errorf("inventory file not found: %s (no .example template either)", invPath)
	}
	data, err := os.ReadFile(examplePath)
	if err != nil {
		return fmt.Errorf("read example inventory: %w", err)
	}
	if err := os.WriteFile(invPath, data, 0o644); err != nil {
		return fmt.Errorf("write inventory from example: %w", err)
	}
	slog.Info("bootstrapped inventory from example template", "path", invPath)
	return nil
}

// ensureSSMBucket creates the S3 bucket the Ansible SSM connection plugin
// uses to transfer files, if it does not already exist.
func ensureSSMBucket(ctx context.Context, cfg *config.Config) error {
	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}
	bucket := parsed.SSMBucketName()
	if bucket == "" {
		return nil
	}
	region := parsed.Region()
	if region == "" {
		region, err = cfg.ResolveRegion()
		if err != nil {
			return err
		}
	}
	client, err := daws.NewClient(ctx, region, "")
	if err != nil {
		return err
	}
	return client.EnsureSSMBucket(ctx, bucket)
}

// ensureInventorySynced compares inventory instance IDs against live EC2
// state and auto-syncs if they diverge. This prevents provisioning against
// stale instance IDs after an infra destroy/apply cycle.
// This is a no-op for non-SSM inventories (e.g. Ludus, Proxmox).
func ensureInventorySynced(ctx context.Context, cfg *config.Config) error {
	invPath := cfg.InventoryPath()
	if err := bootstrapInventory(invPath); err != nil {
		return err
	}
	parsed, err := inv.Parse(invPath)
	if err != nil {
		return fmt.Errorf("parse inventory: %w", err)
	}

	if !parsed.IsSSM() {
		return nil
	}

	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	liveInstances, err := prov.DiscoverInstances(ctx, cfg.Env)
	if err != nil {
		return fmt.Errorf("discover instances: %w", err)
	}
	if len(liveInstances) == 0 {
		return fmt.Errorf("no running instances found for env=%s", cfg.Env)
	}

	liveIDs := make(map[string]struct{}, len(liveInstances))
	for _, inst := range liveInstances {
		liveIDs[inst.ID] = struct{}{}
	}

	stale := false
	for _, host := range parsed.Hosts {
		if host.InstanceID == "" {
			continue
		}
		if _, ok := liveIDs[host.InstanceID]; !ok {
			stale = true
			break
		}
	}

	if !stale {
		return nil
	}

	slog.Info("inventory has stale instance IDs, auto-syncing from provider")
	var instances []instanceInfo
	for _, i := range liveInstances {
		instances = append(instances, instanceInfo{InstanceID: i.ID, Name: i.Name})
	}
	return applyInstanceUpdates(invPath, instances)
}

func runProvision(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}
	// Signal-aware context from the root: Ctrl+C/SIGTERM cancels ctx so
	// provisionPlaybooks unwinds and its deferred socksTunnel.Close() runs,
	// instead of the process dying with the Bastion tunnel orphaned.
	ctx := cmd.Context()

	playsFlag, _ := cmd.Flags().GetString("plays")
	fromFlag, _ := cmd.Flags().GetString("from")
	playbooks, err := resolvePlaybooks(cfg, playsFlag, fromFlag)
	if err != nil {
		return err
	}

	limit, _ := cmd.Flags().GetString("limit")
	retry, err := retryOverridesFromFlags(cmd)
	if err != nil {
		return err
	}
	extraVars, err := parseExtraVars(cmd)
	if err != nil {
		return err
	}

	return provisionPlaybooks(ctx, cfg, playbooks, limit, retry, extraVars)
}

type retryOverrides struct {
	maxRetries *int
	retryDelay *int
}

func retryOverridesFromFlags(cmd *cobra.Command) (retryOverrides, error) {
	maxRetries, err := optionalNonNegativeIntFlag(cmd, "max-retries")
	if err != nil {
		return retryOverrides{}, err
	}
	retryDelay, err := optionalNonNegativeIntFlag(cmd, "retry-delay")
	if err != nil {
		return retryOverrides{}, err
	}
	return retryOverrides{maxRetries: maxRetries, retryDelay: retryDelay}, nil
}

func optionalNonNegativeIntFlag(cmd *cobra.Command, name string) (*int, error) {
	if !cmd.Flags().Changed(name) {
		return nil, nil
	}
	value, err := cmd.Flags().GetInt(name)
	if err != nil {
		return nil, fmt.Errorf("read --%s: %w", name, err)
	}
	if value < 0 {
		return nil, fmt.Errorf("--%s must be zero or greater", name)
	}
	return &value, nil
}

func (r retryOverrides) apply(opts *ansible.RetryOptions) {
	if r.maxRetries != nil {
		opts.MaxRetries = *r.maxRetries
		opts.MaxRetriesSet = true
	}
	if r.retryDelay != nil {
		opts.RetryDelay = time.Duration(*r.retryDelay) * time.Second
		opts.RetryDelaySet = true
	}
}

// parseExtraVars reads the repeatable --extra-vars flag into the map the
// Ansible runner passes through as `-e key=value`.
//
// This is the only way to reach a role default from the command line, which
// matters most for the ones that are destructive by design: the `ad` role
// reconciles passwords and group membership on every ad-data.yml run, and
// `ad_reconcile_check_only=true` is what turns that into a report instead of a
// write. Without a flag, rehearsing a reset meant editing defaults/main.yml.
func parseExtraVars(cmd *cobra.Command) (map[string]string, error) {
	pairs, _ := cmd.Flags().GetStringArray("extra-vars")
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--extra-vars %q is not key=value", p)
		}
		out[k] = v
	}
	return out, nil
}

// applyExtraVars layers user-supplied vars over the SOCKS tunnel's, so an
// explicit -e always wins; the tunnel only sets connection plumbing, which
// nobody overrides by accident. It also echoes what it applied, because a var
// that silently failed to take effect is indistinguishable from one that did.
func applyExtraVars(socksVars, extraVars map[string]string) map[string]string {
	if len(extraVars) == 0 {
		return socksVars
	}
	out := make(map[string]string, len(socksVars)+len(extraVars))
	for k, v := range socksVars {
		out[k] = v
	}
	for k, v := range extraVars {
		out[k] = v
	}
	fmt.Printf("Extra vars: %s\n", strings.Join(sortedPairs(extraVars), " "))
	return out
}

// sortedPairs renders a var map as stable "k=v" strings for display.
func sortedPairs(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	slices.Sort(out)
	return out
}

type provisionFailure struct {
	Playbook string
	LogFile  string
	Err      error
}

func (e *provisionFailure) Error() string {
	return fmt.Sprintf("provisioning failed at %s: %v\n  see full log: %s", e.Playbook, e.Err, e.LogFile)
}

func (e *provisionFailure) Unwrap() error {
	return e.Err
}

// provisionPlaybooks runs preflight checks then executes the given playbooks
// with retry logic. Shared between `provision` and `lab reset`.
func provisionPlaybooks(ctx context.Context, cfg *config.Config, playbooks []string, limit string, retry retryOverrides, extraVars map[string]string) error {
	_ = os.MkdirAll(cfg.LogDir, 0o755)
	logFile := filepath.Join(cfg.LogDir, fmt.Sprintf("%s-dreadgoad-%s.log",
		cfg.Env, time.Now().Format("20060102_150405")))

	if err := preflightChecks(ctx, cfg, limit); err != nil {
		return err
	}

	fmt.Println("===============================================")
	fmt.Printf("DreadGOAD provisioning started at %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("Environment: %s\n", cfg.Env)
	fmt.Printf("Log file: %s\n", logFile)
	if limit != "" {
		fmt.Printf("Limited to hosts: %s\n", limit)
	}
	fmt.Println("===============================================")
	fmt.Println("\nPlaybooks to execute:")
	for _, p := range playbooks {
		fmt.Printf("  - ansible/playbooks/%s\n", p)
	}
	fmt.Println("-----------------------------------------------")

	// When running against a remote Ludus server or Azure (where private
	// VMs aren't reachable from the laptop), open a SOCKS5 proxy through
	// SSH and override Ansible to route WinRM via the psrp connection
	// plugin. For Azure, the proxy chain is: laptop → Bastion port-tunnel
	// → controller SSH → SOCKS5 → in-VNet GOAD VM:5985.
	var socksTunnel closableTunnel
	var socksVars map[string]string
	if tunnel, vars, err := maybeStartSOCKSTunnel(ctx, cfg); err != nil {
		return fmt.Errorf("SOCKS tunnel setup failed: %w", err)
	} else if tunnel != nil {
		socksTunnel = tunnel
		socksVars = vars
		defer socksTunnel.Close()
	}

	runVars := applyExtraVars(socksVars, extraVars)

	log := slog.Default()
	useSSM := isSSMInventory(cfg)

	// Clean up stale SSM sessions before starting provisioning to prevent
	// connection saturation from orphaned sessions of previous runs.
	if useSSM {
		log.Info("cleaning up stale SSM sessions before provisioning")
		ansible.CleanupSSMSessions(ctx, cfg.Env, log)
	}

	for i, playbook := range playbooks {
		opts := ansible.RetryOptions{
			Playbook:  playbook,
			Env:       cfg.Env,
			Limit:     limit,
			Debug:     cfg.Debug,
			LogFile:   logFile,
			ExtraVars: runVars,
		}
		retry.apply(&opts)

		if err := ansible.RunPlaybookWithRetry(ctx, opts); err != nil {
			log.Error("provisioning failed", "playbook", playbook, "log_file", logFile, "error", err)
			return &provisionFailure{Playbook: playbook, LogFile: logFile, Err: err}
		}

		// Between playbooks: clean up accumulated SSM sessions and wait
		// after reboot-inducing playbooks for SSM agents to reconnect.
		if i < len(playbooks)-1 {
			if useSSM {
				ansible.CleanupSSMSessions(ctx, cfg.Env, log)
			}
			if useSSM && slices.Contains(config.RebootPlaybooks, playbook) {
				log.Info("playbook may have caused reboots, waiting for SSM reconnection",
					"playbook", playbook, "delay", "120s")
				time.Sleep(120 * time.Second)
			}
		}
	}

	log.Info("provisioning complete", "playbooks", len(playbooks), "log_file", logFile)
	fmt.Println("===============================================")
	fmt.Printf("All playbooks completed successfully at %s\n", time.Now().Format(time.RFC3339))
	fmt.Printf("Full log: %s\n", logFile)
	fmt.Println("===============================================")
	return nil
}

// maybeStartSOCKSTunnel selects a provider-appropriate SOCKS5 tunnel for
// reaching private Windows hosts. Returns (nil, nil, nil) when the active
// provider doesn't need one (AWS SSM dial-in, Ludus in local mode, etc.).
//
// Each branch returns the same shape: a closable handle + the Ansible
// extra-vars that route the psrp connection plugin through the proxy.
func maybeStartSOCKSTunnel(ctx context.Context, cfg *config.Config) (closableTunnel, map[string]string, error) {
	switch cfg.ResolvedProvider() {
	case provider.NameAzure:
		return startAzureSOCKSTunnel(ctx, cfg)
	case "ludus":
		return startLudusSOCKSTunnel(cfg)
	default:
		return nil, nil, nil
	}
}

// startAzureSOCKSTunnel chains an Azure Bastion port-forward + an SSH SOCKS5
// proxy through the in-VNet Ansible controller, then returns the psrp vars
// Ansible needs to dial GOAD VM:5985 through that chain.
func startAzureSOCKSTunnel(ctx context.Context, cfg *config.Config) (closableTunnel, map[string]string, error) {
	prov, err := cfg.NewProvider(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create azure provider: %w", err)
	}
	azProv, ok := prov.(*azure.AzureProvider)
	if !ok {
		return nil, nil, fmt.Errorf("provider is not azure (got %T)", prov)
	}

	fmt.Println("Opening Azure Bastion → controller → SOCKS5 chain for WinRM access...")
	tunnel, err := azure.StartProvisionTunnel(ctx, azProv.Client(), cfg.Env)
	if err != nil {
		return nil, nil, err
	}
	fmt.Printf("  SOCKS5 proxy: %s\n", tunnel.ProxyURL())

	vars := map[string]string{
		"ansible_connection":           "psrp",
		"ansible_psrp_proxy":           tunnel.ProxyURL(),
		"ansible_psrp_auth":            "ntlm",
		"ansible_psrp_cert_validation": "ignore",
		"ansible_psrp_protocol":        "http",
		"ansible_port":                 "5985",
	}
	return tunnel, vars, nil
}

// startLudusSOCKSTunnel preserves the original Ludus-in-SSH-mode behavior:
// open SSH to the Ludus host, layer SOCKS5, route Ansible psrp through it.
func startLudusSOCKSTunnel(cfg *config.Config) (closableTunnel, map[string]string, error) {
	target := cfg.Ludus.SSHTarget()
	if target == "" {
		return nil, nil, nil
	}

	sshCfg := ludus.SSHConfig{
		Host:     target,
		User:     cfg.Ludus.SSHUser,
		KeyPath:  cfg.Ludus.SSHKeyPath,
		Password: cfg.Ludus.SSHPassword,
		Port:     cfg.Ludus.SSHPort,
	}

	fmt.Println("Starting SOCKS5 tunnel to Ludus host for WinRM access...")
	tunnel, err := ludus.StartSOCKSTunnel(sshCfg)
	if err != nil {
		return nil, nil, err
	}
	fmt.Printf("  SOCKS5 proxy listening on localhost:%d\n", tunnel.Port)

	// Override Ansible connection vars to route WinRM through the tunnel
	// using the psrp connection plugin (which supports SOCKS proxies).
	vars := map[string]string{
		"ansible_connection":           "psrp",
		"ansible_psrp_proxy":           tunnel.ProxyURL(),
		"ansible_psrp_auth":            "ntlm",
		"ansible_psrp_cert_validation": "ignore",
		"ansible_psrp_protocol":        "http",
		"ansible_port":                 "5985",
	}

	return tunnel, vars, nil
}
