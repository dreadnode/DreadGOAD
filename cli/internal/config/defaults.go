package config

import "github.com/spf13/viper"

// DefaultPlaybooks is the ordered list of all GOAD playbooks.
var DefaultPlaybooks = []string{
	"build.yml",
	"ad-servers.yml",
	"ad-parent_domain.yml",
	"ad-child_domain.yml",
	"ad-members.yml",
	"ad-trusts.yml",
	"ad-data.yml",
	"ad-gmsa.yml",
	"laps.yml",
	"ad-relations.yml",
	"adcs.yml",
	"ad-acl.yml",
	"servers.yml",
	"security.yml",
	"vulnerabilities.yml",
}

// RebootPlaybooks are playbooks that may trigger Windows reboots.
var RebootPlaybooks = []string{
	"ad-parent_domain.yml",
	"ad-child_domain.yml",
	"ad-members.yml",
	"ad-trusts.yml",
}

func setDefaults() {
	viper.SetDefault("env", "staging")
	viper.SetDefault("region", "")
	viper.SetDefault("debug", false)
	viper.SetDefault("max_retries", 3)
	viper.SetDefault("retry_delay", 30)
	viper.SetDefault("idle_timeout", 1200)
	viper.SetDefault("log_dir", "")
	viper.SetDefault("playbooks", DefaultPlaybooks)
	viper.SetDefault("environments", map[string]interface{}{
		"dev": map[string]interface{}{
			"variant":        true,
			"variant_source": "ad/GOAD",
			"variant_target": "ad/GOAD-variant-1",
			"variant_name":   "variant-1",
		},
		"staging": map[string]interface{}{
			"variant": false,
		},
	})
}
