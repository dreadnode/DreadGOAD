package config

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/spf13/viper"
)

// EnvironmentConfig holds per-environment settings.
type EnvironmentConfig struct {
	Variant       bool   `mapstructure:"variant"`
	VariantSource string `mapstructure:"variant_source"`
	VariantTarget string `mapstructure:"variant_target"`
	VariantName   string `mapstructure:"variant_name"`
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
}

var (
	cfg  *Config
	once sync.Once
)

// Init initializes Viper configuration. Called by cobra.OnInitialize.
func Init() {
	if cfgFile := viper.GetString("config"); cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, _ := os.UserHomeDir()
		viper.AddConfigPath(filepath.Join(home, ".config", "dreadgoad"))
		viper.AddConfigPath(".")
		viper.SetConfigName("dreadgoad")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("DREADGOAD")
	viper.AutomaticEnv()

	setDefaults()

	// Config file is optional
	_ = viper.ReadInConfig()
}

// Get returns the current configuration, loading it once.
func Get() *Config {
	once.Do(func() {
		cfg = &Config{}
		_ = viper.Unmarshal(cfg)

		// Resolve project root (directory containing ansible/)
		if cfg.ProjectRoot == "" {
			cfg.ProjectRoot = findProjectRoot()
		}

		// Expand log dir
		if cfg.LogDir == "" {
			home, _ := os.UserHomeDir()
			cfg.LogDir = filepath.Join(home, ".ansible", "logs", "goad")
		}
	})
	return cfg
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
func (c *Config) AnsibleEnv() map[string]string {
	home, _ := os.UserHomeDir()
	return map[string]string{
		"ANSIBLE_CONFIG":                  c.AnsibleCfgPath(),
		"ANSIBLE_CACHE_PLUGIN_CONNECTION": filepath.Join(home, ".ansible", "cache", c.Env+"_dreadgoad_facts"),
		"ANSIBLE_HOST_KEY_CHECKING":       "False",
		"ANSIBLE_RETRY_FILES_ENABLED":     "True",
		"ANSIBLE_GATHER_TIMEOUT":          "60",
	}
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

func findProjectRoot() string {
	// Walk up from cwd looking for ansible/ directory
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "ansible")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	// Fallback to cwd
	cwd, _ := os.Getwd()
	return cwd
}
