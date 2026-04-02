package config

import "github.com/spf13/viper"

// DefaultPlaybooks is the ordered list of all GOAD playbooks.
var DefaultPlaybooks = []string{
	"network_setup.yml",
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
	// Extension defaults
	viper.SetDefault("extensions.elk.description", "Add an ELK stack to the current lab")
	viper.SetDefault("extensions.elk.machines", []string{"elk"})
	viper.SetDefault("extensions.elk.compatibility", []string{"*"})
	viper.SetDefault("extensions.elk.impact", "add a linux machine and add a logbeat agent on all windows machines")
	viper.SetDefault("extensions.elk.playbook", "ext-elk.yml")

	viper.SetDefault("extensions.exchange.description", "Add an Exchange server to the GOAD lab")
	viper.SetDefault("extensions.exchange.machines", []string{"srv01"})
	viper.SetDefault("extensions.exchange.compatibility", []string{"GOAD", "GOAD-Light", "GOAD-Mini"})
	viper.SetDefault("extensions.exchange.impact", "modifies AD schema and adds a server (heavy)")
	viper.SetDefault("extensions.exchange.playbook", "ext-exchange.yml")
	viper.SetDefault("extensions.exchange.data_dir", "exchange/data")

	viper.SetDefault("extensions.guacamole.description", "Add Apache Guacamole for remote access")
	viper.SetDefault("extensions.guacamole.machines", []string{"guacamole"})
	viper.SetDefault("extensions.guacamole.compatibility", []string{"*"})
	viper.SetDefault("extensions.guacamole.impact", "none")
	viper.SetDefault("extensions.guacamole.playbook", "ext-guacamole.yml")

	viper.SetDefault("extensions.lx01.description", "Add a Linux machine enrolled to the domain")
	viper.SetDefault("extensions.lx01.machines", []string{"lx01"})
	viper.SetDefault("extensions.lx01.compatibility", []string{"GOAD", "GOAD-Light", "GOAD-Mini"})
	viper.SetDefault("extensions.lx01.impact", "none")
	viper.SetDefault("extensions.lx01.playbook", "ext-lx01.yml")
	viper.SetDefault("extensions.lx01.data_dir", "lx01/data")

	viper.SetDefault("extensions.wazuh.description", "Add the Wazuh EDR into the lab")
	viper.SetDefault("extensions.wazuh.machines", []string{"wazuh"})
	viper.SetDefault("extensions.wazuh.compatibility", []string{"*"})
	viper.SetDefault("extensions.wazuh.impact", "add a wazuh machine and agent on all windows machines")
	viper.SetDefault("extensions.wazuh.playbook", "ext-wazuh.yml")

	viper.SetDefault("extensions.ws01.description", "Add a hardened workstation into the lab")
	viper.SetDefault("extensions.ws01.machines", []string{"ws01"})
	viper.SetDefault("extensions.ws01.compatibility", []string{"GOAD", "GOAD-Light", "GOAD-Mini"})
	viper.SetDefault("extensions.ws01.impact", "AWS uses Windows Server 2019 instead of Windows 10")
	viper.SetDefault("extensions.ws01.playbook", "ext-ws01.yml")
	viper.SetDefault("extensions.ws01.data_dir", "ws01/data")

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
