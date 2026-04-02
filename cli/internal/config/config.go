package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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
}

// Config holds all CLI configuration.
type Config struct {
	Env          string                       `mapstructure:"env"`
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
}

var (
	cfg  *Config
	once sync.Once
)

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

// ExtensionPath returns the base path for an extension's infra content.
func (c *Config) ExtensionPath(name string) string {
	return filepath.Join(c.ProjectRoot, "extensions", name)
}

// ExtensionInventoryTemplate returns the path to an extension's inventory template.
func (c *Config) ExtensionInventoryTemplate(name string) string {
	return filepath.Join(c.ExtensionPath(name), "inventory.j2")
}

// ExtensionProviderPath returns the path to an extension's provider-specific config.
func (c *Config) ExtensionProviderPath(name, provider string) string {
	return filepath.Join(c.ExtensionPath(name), "providers", provider)
}

// ExtensionDataDir returns the path to an extension's data directory.
func (c *Config) ExtensionDataDir(name string) string {
	return filepath.Join(c.ExtensionPath(name), "data")
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

func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	// Walk up from cwd looking for ansible/ directory
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
	// Fallback to cwd
	return cwd, nil
}
