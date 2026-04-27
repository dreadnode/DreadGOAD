package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/jsonmerge"
	"github.com/spf13/viper"
)

// ExtensionConfig holds metadata for a lab extension.
type ExtensionConfig struct {
	Description   string   `mapstructure:"description"`
	Machines      []string `mapstructure:"machines"`
	Compatibility []string `mapstructure:"compatibility"`
	Impact        string   `mapstructure:"impact"`
	Playbook      string   `mapstructure:"playbook"`
	DataDir       string   `mapstructure:"data_dir"`
}

// EnvironmentConfig holds per-environment settings.
type EnvironmentConfig struct {
	Variant           bool     `mapstructure:"variant"`
	VariantSource     string   `mapstructure:"variant_source"`
	VariantTarget     string   `mapstructure:"variant_target"`
	VariantName       string   `mapstructure:"variant_name"`
	EnabledExtensions []string `mapstructure:"enabled_extensions"`
	VpcCidr           string   `mapstructure:"vpc_cidr"`
}

// InfraConfig holds infrastructure/terragrunt settings.
type InfraConfig struct {
	Deployment       string `mapstructure:"deployment"`
	TerragruntBinary string `mapstructure:"terragrunt_binary"`
	TerraformBinary  string `mapstructure:"terraform_binary"`
}

// ProxmoxConfig holds Proxmox-specific settings.
type ProxmoxConfig struct {
	APIURL        string            `mapstructure:"api_url"`
	User          string            `mapstructure:"user"`
	Password      string            `mapstructure:"password"`
	Node          string            `mapstructure:"node"`
	Pool          string            `mapstructure:"pool"`
	FullClone     string            `mapstructure:"full_clone"`
	Storage       string            `mapstructure:"storage"`
	VLAN          string            `mapstructure:"vlan"`
	NetworkBridge string            `mapstructure:"network_bridge"`
	NetworkModel  string            `mapstructure:"network_model"`
	IPRange       string            `mapstructure:"ip_range"`
	Lab           string            `mapstructure:"lab"`
	TemplateIDs   map[string]string `mapstructure:"template_ids"`
}

// LudusConfig holds Ludus-specific settings.
//
// Host is the preferred way to point at a remote Ludus server: it accepts an
// ssh_config Host alias (so the user's existing ~/.ssh/config — including
// IdentityAgent, ProxyJump, etc. — drives the connection) or a raw hostname.
// The SSH* fields are explicit overrides for CI / automation contexts where
// ssh_config can't be relied on.
type LudusConfig struct {
	APIKey           string `mapstructure:"api_key"`
	UseImpersonation bool   `mapstructure:"use_impersonation"`
	Host             string `mapstructure:"host"`         // ssh_config alias or hostname (preferred)
	SSHHost          string `mapstructure:"ssh_host"`     // Explicit hostname override
	SSHUser          string `mapstructure:"ssh_user"`     // SSH user override (default: root)
	SSHKeyPath       string `mapstructure:"ssh_key_path"` // Explicit private key path
	SSHPassword      string `mapstructure:"ssh_password"` // SSH password (uses sshpass)
	SSHPort          int    `mapstructure:"ssh_port"`     // SSH port override (default: 22)
}

// SSHTarget returns the ssh connection target — preferring Host over the
// legacy SSHHost field — or the empty string if SSH mode isn't configured.
func (l LudusConfig) SSHTarget() string {
	if l.Host != "" {
		return l.Host
	}
	return l.SSHHost
}

// Config holds all CLI configuration.
type Config struct {
	Env          string                       `mapstructure:"env"`
	Provider     string                       `mapstructure:"provider"`
	Region       string                       `mapstructure:"region"`
	Debug        bool                         `mapstructure:"debug"`
	MaxRetries   int                          `mapstructure:"max_retries"`
	RetryDelay   int                          `mapstructure:"retry_delay"`
	IdleTimeout  int                          `mapstructure:"idle_timeout"`
	LogDir       string                       `mapstructure:"log_dir"`
	Playbooks    []string                     `mapstructure:"playbooks"`
	ProjectRoot  string                       `mapstructure:"project_root"`
	Environments map[string]EnvironmentConfig `mapstructure:"environments"`
	Extensions   map[string]ExtensionConfig   `mapstructure:"extensions"`
	Infra        InfraConfig                  `mapstructure:"infra"`
	Proxmox      ProxmoxConfig                `mapstructure:"proxmox"`
	Ludus        LudusConfig                  `mapstructure:"ludus"`
}

var (
	cfg          *Config
	once         sync.Once
	configMissing bool
)

// ConfigMissing returns true if no dreadgoad.yaml was found during Init.
// Commands that depend on provider configuration should check this and warn
// the user (e.g. "no config found, using defaults; run 'dreadgoad init'").
func ConfigMissing() bool { return configMissing }

// Init initializes Viper configuration. Called from PersistentPreRunE.
func Init() error {
	if cfgFile := viper.GetString("config"); cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolving home directory: %w", err)
		}
		viper.AddConfigPath(filepath.Join(home, ".config", "dreadgoad"))
		// Search project root (walk up from cwd looking for ansible/ dir)
		// so the config is found regardless of which subdirectory we run from.
		if root, err := findProjectRoot(); err == nil {
			viper.AddConfigPath(root)
		}
		viper.AddConfigPath(".")
		viper.SetConfigName("dreadgoad")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("DREADGOAD")
	viper.AutomaticEnv()

	setDefaults()

	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("reading config: %w", err)
		}
		configMissing = true
	}
	return nil
}

// Get returns the current configuration, loading it once.
func Get() (*Config, error) {
	var initErr error
	once.Do(func() {
		cfg = &Config{}
		if err := viper.Unmarshal(cfg); err != nil {
			initErr = fmt.Errorf("unmarshaling config: %w", err)
			return
		}

		if cfg.ProjectRoot == "" {
			root, err := findProjectRoot()
			if err != nil {
				initErr = fmt.Errorf("finding project root: %w", err)
				return
			}
			cfg.ProjectRoot = root
		}

		if cfg.LogDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				initErr = fmt.Errorf("resolving home directory: %w", err)
				return
			}
			cfg.LogDir = filepath.Join(home, ".ansible", "logs", "goad")
		}
	})
	return cfg, initErr
}

// Reset clears the cached config (for testing).
func Reset() {
	once = sync.Once{}
	cfg = nil
}

// InventoryPath returns the path to the inventory file for the current env.
func (c *Config) InventoryPath() string {
	return filepath.Join(c.ProjectRoot, c.Env+"-inventory")
}

// LabConfigPath returns the path to the environment's lab config JSON.
// It delegates to ResolvedLabConfigPath (which supports overlay merging)
// and falls back to the legacy direct path on error.
func (c *Config) LabConfigPath() string {
	if p, err := c.ResolvedLabConfigPath(); err == nil {
		return p
	}
	return filepath.Join(c.labConfigDataDir(), c.Env+"-config.json")
}

// ResolvedLabConfigPath returns the path to a ready-to-use lab config JSON.
// When an overlay file ({env}-overlay.json) exists alongside the base
// config.json, it merges them using RFC 7386 JSON Merge Patch semantics
// and caches the result under .dreadgoad/cache/. Falls back to a legacy
// {env}-config.json if present, then to the base config.json.
func (c *Config) ResolvedLabConfigPath() (string, error) {
	dataDir := c.labConfigDataDir()

	overlayPath := filepath.Join(dataDir, c.Env+"-overlay.json")
	basePath := filepath.Join(dataDir, "config.json")

	if fileExists(overlayPath) && fileExists(basePath) {
		return c.mergedConfigPath(basePath, overlayPath)
	}

	// Legacy: full {env}-config.json exists.
	legacyPath := filepath.Join(dataDir, c.Env+"-config.json")
	if fileExists(legacyPath) {
		return legacyPath, nil
	}

	// Fallback: base config.json.
	if fileExists(basePath) {
		return basePath, nil
	}

	return "", fmt.Errorf("no lab config found in %s", dataDir)
}

// labConfigDataDir returns the data directory for the active environment's
// lab config (variant target or base GOAD).
func (c *Config) labConfigDataDir() string {
	ec := c.ActiveEnvironment()
	if ec.Variant {
		_, target := c.ResolvedVariantPaths()
		if target != "" {
			d := filepath.Join(target, "data")
			if info, err := os.Stat(d); err == nil && info.IsDir() {
				return d
			}
		}
	}
	return filepath.Join(c.ProjectRoot, "ad", "GOAD", "data")
}

// mergedConfigPath merges base + overlay and caches the result. Returns
// the cached file path. The cache is invalidated when either source file
// is newer than the cached output.
func (c *Config) mergedConfigPath(basePath, overlayPath string) (string, error) {
	cacheDir := filepath.Join(c.ProjectRoot, ".dreadgoad", "cache")
	cachePath := filepath.Join(cacheDir, c.Env+"-config.json")

	// Check if cache is fresh.
	if cacheInfo, err := os.Stat(cachePath); err == nil {
		cacheMod := cacheInfo.ModTime()
		if cacheMod.After(fileMtime(basePath)) && cacheMod.After(fileMtime(overlayPath)) {
			return cachePath, nil
		}
	}

	base, err := os.ReadFile(basePath)
	if err != nil {
		return "", fmt.Errorf("read base config: %w", err)
	}
	overlay, err := os.ReadFile(overlayPath)
	if err != nil {
		return "", fmt.Errorf("read overlay: %w", err)
	}

	merged, err := jsonmerge.MergePatchBytes(base, overlay)
	if err != nil {
		return "", fmt.Errorf("merge config: %w", err)
	}

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("create cache dir: %w", err)
	}

	// Atomic write: temp file + rename.
	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, merged, 0o644); err != nil {
		return "", fmt.Errorf("write cache: %w", err)
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		if rmErr := os.Remove(tmp); rmErr != nil {
			return "", fmt.Errorf("rename cache: %w; cleanup: %w", err, rmErr)
		}
		return "", fmt.Errorf("rename cache: %w", err)
	}

	return cachePath, nil
}

func fileMtime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// AnsibleCfgPath returns the path to the ansible.cfg file.
func (c *Config) AnsibleCfgPath() string {
	return filepath.Join(c.ProjectRoot, "ansible", "ansible.cfg")
}

// AnsibleEnv returns environment variables needed for ansible-playbook execution.
func (c *Config) AnsibleEnv() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	return map[string]string{
		"ANSIBLE_CONFIG":                  c.AnsibleCfgPath(),
		"ANSIBLE_CACHE_PLUGIN_CONNECTION": filepath.Join(home, ".ansible", "cache", c.Env+"_dreadgoad_facts"),
		"ANSIBLE_HOST_KEY_CHECKING":       "False",
		"ANSIBLE_RETRY_FILES_ENABLED":     "True",
		"ANSIBLE_GATHER_TIMEOUT":          "60",
	}, nil
}

// ActiveEnvironment returns the EnvironmentConfig for the currently selected env.
// Returns a zero-value EnvironmentConfig if not defined (variant: false).
func (c *Config) ActiveEnvironment() EnvironmentConfig {
	if c.Environments == nil {
		return EnvironmentConfig{}
	}
	return c.Environments[c.Env]
}

// ResolvedVariantPaths returns absolute source/target paths for the active
// environment's variant config. Returns empty strings if variant is false.
func (c *Config) ResolvedVariantPaths() (source, target string) {
	ec := c.ActiveEnvironment()
	if !ec.Variant {
		return "", ""
	}
	src := ec.VariantSource
	if src == "" {
		src = "ad/GOAD"
	}
	tgt := ec.VariantTarget
	if tgt == "" {
		tgt = "ad/GOAD-variant-1"
	}
	if !filepath.IsAbs(src) {
		src = filepath.Join(c.ProjectRoot, src)
	}
	if !filepath.IsAbs(tgt) {
		tgt = filepath.Join(c.ProjectRoot, tgt)
	}
	return src, tgt
}

// ExtensionInventoryTemplate returns the path to an extension's inventory template
// within the Ansible collection (ansible/playbooks/templates/extensions/<name>/).
func (c *Config) ExtensionInventoryTemplate(name string) string {
	return filepath.Join(c.ProjectRoot, "ansible", "playbooks", "templates", "extensions", name, "inventory.j2")
}

// ExtensionDataDir returns the path to an extension's data directory
// within the Ansible collection (ansible/playbooks/files/extensions/<name>/).
func (c *Config) ExtensionDataDir(name string) string {
	return filepath.Join(c.ProjectRoot, "ansible", "playbooks", "files", "extensions", name)
}

// ExtensionProviderPath returns the path to an extension's provider-specific config
// at the repository root (extensions/<name>/<provider>/).
func (c *Config) ExtensionProviderPath(name, provider string) string {
	return filepath.Join(c.ProjectRoot, "extensions", name, provider)
}

// IsExtensionCompatible checks if an extension is compatible with the given lab.
func (c *Config) IsExtensionCompatible(name, lab string) bool {
	ext, ok := c.Extensions[name]
	if !ok {
		return false
	}
	for _, compat := range ext.Compatibility {
		if compat == "*" || compat == lab {
			return true
		}
	}
	return false
}

// EnabledExtensionsForEnv returns the enabled extensions for the active environment.
func (c *Config) EnabledExtensionsForEnv() []string {
	return c.ActiveEnvironment().EnabledExtensions
}

// VpcCIDR returns the VPC CIDR for the given environment. It checks the
// environment config first, falling back to deterministic generation.
func (c *Config) VpcCIDR(envName string) string {
	if ec, ok := c.Environments[envName]; ok && ec.VpcCidr != "" {
		return ec.VpcCidr
	}
	// Generate a deterministic second octet from env name (range 10-250)
	var hash byte
	for _, ch := range envName {
		hash = hash*31 + byte(ch)
	}
	octet := int(hash)%240 + 10
	return fmt.Sprintf("10.%d.0.0/16", octet)
}

// ResolvedProvider returns the provider name, defaulting to "aws" for backward compatibility.
func (c *Config) ResolvedProvider() string {
	if c.Provider == "" {
		return "aws"
	}
	return c.Provider
}

// IsAWS returns true if the configured provider is AWS.
func (c *Config) IsAWS() bool {
	return c.ResolvedProvider() == "aws"
}

// ResolveRegion returns the configured AWS region or an actionable error if
// none is set. This is the single source of truth for region resolution: every
// command that needs to talk to AWS should call it (or ResolveRegionWithInventory)
// rather than hardcoding a default.
func (c *Config) ResolveRegion() (string, error) {
	if c.Region == "" {
		return "", fmt.Errorf("AWS region not configured: set 'region' in dreadgoad.yaml, export DREADGOAD_REGION, or pass --region")
	}
	return c.Region, nil
}

// ResolveRegionWithInventory resolves the AWS region for talking to a deployed
// lab, preferring the parsed Ansible inventory's own region (most authoritative
// — the lab knows where it lives) and falling back to ResolveRegion.
func (c *Config) ResolveRegionWithInventory(inv *inventory.Inventory) (string, error) {
	if inv != nil {
		if r := inv.Region(); r != "" {
			return r, nil
		}
	}
	return c.ResolveRegion()
}

// InfraBasePath returns the base path for a deployment's infra directory.
func (c *Config) InfraBasePath() string {
	return filepath.Join(c.ProjectRoot, "infra", c.Infra.Deployment)
}

// InfraWorkDir returns the working directory for terragrunt operations
// at the region level: infra/{deployment}/{env}/{region}/
func (c *Config) InfraWorkDir() (string, error) {
	region, err := c.ResolveRegion()
	if err != nil {
		return "", err
	}
	return filepath.Join(c.InfraBasePath(), c.Env, region), nil
}

// InfraModulePath returns the path for a specific module within the infra working directory.
func (c *Config) InfraModulePath(module string) (string, error) {
	workDir, err := c.InfraWorkDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(workDir, module), nil
}

// ProxmoxWorkDir returns the working directory for Proxmox Terraform operations.
// Files are rendered to .dreadgoad/proxmox/{env}/.
func (c *Config) ProxmoxWorkDir() string {
	return filepath.Join(c.ProjectRoot, ".dreadgoad", "proxmox", c.Env)
}

// ProxmoxLab returns the lab name for Proxmox deployments.
func (c *Config) ProxmoxLab() string {
	if c.Proxmox.Lab != "" {
		return c.Proxmox.Lab
	}
	return "GOAD"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "ansible")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return cwd, nil
}
