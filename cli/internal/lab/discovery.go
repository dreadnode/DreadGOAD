package lab

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
	"go.yaml.in/yaml/v3"
)

// Lab represents a discovered DreadGOAD lab definition.
type Lab struct {
	Name             string                              `json:"name"`
	DisplayName      string                              `json:"display_name"`
	Path             string                              `json:"path"`
	Kind             string                              `json:"kind"`
	Providers        []string                            `json:"providers"`
	ProviderSettings map[string]rangeconfig.ProviderSpec `json:"provider_settings"`
	Hosts            []string                            `json:"hosts"`
	VariantSupported bool                                `json:"variant_supported"`
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
		// Generated variants are not base ranges. Their names are user-defined,
		// so a filename marker is authoritative while the substring check above
		// keeps compatibility with old incomplete GOAD-variant-* directories.
		if fileExists(filepath.Join(labPath, "mapping.json")) ||
			fileExists(filepath.Join(labPath, ".dreadgoad-variant-complete")) {
			continue
		}

		manifest, found, err := rangeconfig.Load(labPath)
		if err != nil {
			return nil, err
		}
		if !found {
			// Pre-manifest labs are Active Directory ranges for compatibility.
			manifest = &rangeconfig.Manifest{Kind: rangeconfig.KindActiveDirectory}
		}
		lab := Lab{
			Name:             name,
			DisplayName:      manifest.EffectiveDisplayName(name),
			Path:             labPath,
			Kind:             manifest.Kind,
			ProviderSettings: make(map[string]rangeconfig.ProviderSpec),
			VariantSupported: manifest.SupportsVariants(),
		}

		provDir := filepath.Join(labPath, "providers")
		if provEntries, err := os.ReadDir(provDir); err == nil {
			for _, p := range provEntries {
				if p.IsDir() {
					provider := p.Name()
					lab.Providers = append(lab.Providers, provider)
					if spec, ok := manifest.Provider(provider); ok {
						lab.ProviderSettings[provider] = spec
					}
				}
			}
			sort.Strings(lab.Providers)
		}

		configReady := false
		configPath := filepath.Join(labPath, "data", "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			var cfg labConfig
			if json.Unmarshal(data, &cfg) == nil {
				configReady = true
				for host := range cfg.Lab.Hosts {
					lab.Hosts = append(lab.Hosts, host)
				}
				sort.Strings(lab.Hosts)
			}
		}

		// A provider directory means the authored lab has provider assets; it
		// does not guarantee the modern env-create template can represent every
		// host. Only advertise creation metadata when the template is complete.
		for provider, spec := range lab.ProviderSettings {
			inventory := filepath.Join(labPath, "providers", provider, "inventory")
			if !configReady || !fileExists(inventory) || !templateSupportsLab(projectRoot, provider, spec, lab.Hosts) {
				delete(lab.ProviderSettings, provider)
			}
		}

		labs = append(labs, lab)
	}

	sort.Slice(labs, func(i, j int) bool {
		return labs[i].Name < labs[j].Name
	})
	return labs, nil
}

func templateSupportsLab(projectRoot, provider string, spec rangeconfig.ProviderSpec, hosts []string) bool {
	base := filepath.Join(projectRoot, "infra", spec.Deployment)
	if provider == "azure" {
		base = filepath.Join(projectRoot, "infra", "azure", spec.Deployment)
	}
	templateRoot := filepath.Join(base, spec.TemplateEnvironment)
	var regionDir string
	if spec.DefaultRegion != "" {
		candidate := filepath.Join(templateRoot, spec.DefaultRegion)
		if fileExists(filepath.Join(candidate, "region.hcl")) {
			regionDir = candidate
		}
	} else {
		entries, err := os.ReadDir(templateRoot)
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if entry.IsDir() && fileExists(filepath.Join(templateRoot, entry.Name(), "region.hcl")) {
				regionDir = filepath.Join(templateRoot, entry.Name())
				break
			}
		}
	}
	if regionDir == "" || !fileExists(filepath.Join(templateRoot, "env.hcl")) {
		return false
	}
	if spec.ScaffoldProfile != rangeconfig.ProfileActiveDir {
		return true
	}
	for _, host := range hosts {
		if !fileExists(filepath.Join(regionDir, "goad", host, "terragrunt.hcl")) {
			return false
		}
	}
	return true
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
