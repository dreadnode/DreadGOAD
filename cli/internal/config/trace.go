package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// TraceEntry represents a configuration key with its resolved value and source.
type TraceEntry struct {
	Key    string
	Value  string
	Source string
}

// TraceConfig returns trace entries for the main config keys, showing each
// key's effective value and which layer provided it.
func TraceConfig(cfg *Config, changedFlags map[string]bool) []TraceEntry {
	fileKeys := configFileKeys()
	cfgFile := viper.ConfigFileUsed()

	type item struct {
		key   string
		value string
	}

	items := []item{
		{"env", cfg.Env},
		{"region", cfg.Region},
		{"debug", fmt.Sprintf("%v", cfg.Debug)},
		{"max_retries", fmt.Sprintf("%d", cfg.MaxRetries)},
		{"retry_delay", fmt.Sprintf("%ds", cfg.RetryDelay)},
		{"idle_timeout", fmt.Sprintf("%ds", cfg.IdleTimeout)},
		{"log_dir", cfg.LogDir},
		{"project_root", cfg.ProjectRoot},
		{"infra.deployment", cfg.Infra.Deployment},
		{"infra.terragrunt_binary", cfg.Infra.TerragruntBinary},
		{"infra.terraform_binary", cfg.Infra.TerraformBinary},
	}

	var entries []TraceEntry
	for _, it := range items {
		value := it.value
		if value == "" {
			value = "(unset)"
		}
		source := resolveSource(it.key, changedFlags, fileKeys, cfgFile)
		entries = append(entries, TraceEntry{Key: it.key, Value: value, Source: source})
	}
	return entries
}

// resolveSource determines the layer that provided a viper key's current value.
// Precedence: cli flag > env var > config file > auto-detected > default.
func resolveSource(key string, changedFlags map[string]bool, fileKeys map[string]bool, _ string) string {
	if changedFlags[key] {
		return "cli flag (--" + viperKeyToFlag(key) + ")"
	}
	envKey := "DREADGOAD_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	if _, ok := os.LookupEnv(envKey); ok {
		return "env var (" + envKey + ")"
	}
	if fileKeys[key] {
		return "config file"
	}
	// Auto-detected values: the Config struct has a value but viper doesn't.
	if key == "project_root" || key == "log_dir" {
		if viper.GetString(key) == "" {
			return "auto-detected"
		}
	}
	return "default"
}

// viperKeyToFlag converts a Viper key to its CLI flag form.
func viperKeyToFlag(key string) string {
	return strings.ReplaceAll(key, "_", "-")
}

// configFileKeys parses the config file in isolation (no defaults, no env
// binding) and returns the set of flattened keys actually present in the file.
func configFileKeys() map[string]bool {
	cfgFile := viper.ConfigFileUsed()
	if cfgFile == "" {
		return nil
	}
	v := viper.New()
	v.SetConfigFile(cfgFile)
	if err := v.ReadInConfig(); err != nil {
		return nil
	}
	keys := make(map[string]bool, len(v.AllKeys()))
	for _, k := range v.AllKeys() {
		keys[k] = true
	}
	return keys
}
