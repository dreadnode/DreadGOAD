package lab

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Lab represents a discovered DreadGOAD lab definition.
type Lab struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Providers []string `json:"providers"`
	Hosts     []string `json:"hosts"`
}

// labConfig is the minimal structure of ad/<lab>/data/config.json.
type labConfig struct {
	Lab struct {
		Hosts map[string]json.RawMessage `json:"hosts"`
	} `json:"lab"`
}

// DiscoverLabs scans the ad/ directory for lab definitions.
// Excludes TEMPLATE and variant directories (containing "-variant-").
func DiscoverLabs(projectRoot string) ([]Lab, error) {
	adDir := filepath.Join(projectRoot, "ad")
	entries, err := os.ReadDir(adDir)
	if err != nil {
		return nil, fmt.Errorf("reading ad/ directory: %w", err)
	}

	var labs []Lab
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "TEMPLATE" || strings.Contains(name, "-variant-") {
			continue
		}

		labPath := filepath.Join(adDir, name)
		lab := Lab{
			Name: name,
			Path: labPath,
		}

		// Discover providers
		provDir := filepath.Join(labPath, "providers")
		if provEntries, err := os.ReadDir(provDir); err == nil {
			for _, p := range provEntries {
				if p.IsDir() {
					lab.Providers = append(lab.Providers, p.Name())
				}
			}
			sort.Strings(lab.Providers)
		}

		// Discover hosts from config.json
		configPath := filepath.Join(labPath, "data", "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var cfg labConfig
			if json.Unmarshal(data, &cfg) == nil {
				for host := range cfg.Lab.Hosts {
					lab.Hosts = append(lab.Hosts, host)
				}
				sort.Strings(lab.Hosts)
			}
		}

		labs = append(labs, lab)
	}

	sort.Slice(labs, func(i, j int) bool {
		return labs[i].Name < labs[j].Name
	})
	return labs, nil
}

// LoadPlaybookConfig reads playbooks.yml and returns lab-specific playbook lists.
// The "default" key is used as fallback for labs without explicit entries.
func LoadPlaybookConfig(projectRoot string) (map[string][]string, error) {
	path := filepath.Join(projectRoot, "playbooks.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading playbooks.yml: %w", err)
	}

	var raw map[string][]string
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing playbooks.yml: %w", err)
	}
	return raw, nil
}

// PlaybooksForLab returns the playbook list for a given lab name,
// falling back to "default" if no lab-specific entry exists.
// Returns the config default playbooks if playbooks.yml cannot be loaded.
func PlaybooksForLab(projectRoot, labName string, fallback []string) []string {
	cfg, err := LoadPlaybookConfig(projectRoot)
	if err != nil {
		return fallback
	}
	if pbs, ok := cfg[labName]; ok {
		return pbs
	}
	if pbs, ok := cfg["default"]; ok {
		return pbs
	}
	return fallback
}
