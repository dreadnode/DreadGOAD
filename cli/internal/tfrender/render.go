// Package tfrender renders Terraform template files for non-Terragrunt providers
// (Proxmox, Azure, etc.) by substituting template variables like {{ip_range}},
// {{windows_vms}}, {{linux_vms}}, and {{config.get_value(...)}} calls.
package tfrender

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ProxmoxConfig holds the Proxmox-specific values needed for template rendering.
type ProxmoxConfig struct {
	APIURL        string
	User          string
	Node          string
	Pool          string
	FullClone     string
	Storage       string
	VLAN          string
	NetworkBridge string
	NetworkModel  string
	TemplateIDs   map[string]string // template name -> VMID
}

// RenderOptions configures the template rendering.
type RenderOptions struct {
	// ProjectRoot is the DreadGOAD project root directory.
	ProjectRoot string

	// LabName is the lab to render (e.g. "GOAD", "GOAD-Light").
	LabName string

	// Provider is the provider name (e.g. "proxmox").
	Provider string

	// IPRange is the network prefix (e.g. "192.168.10").
	IPRange string

	// LabIdentifier is the lab instance identifier for naming.
	LabIdentifier string

	// OutputDir is where rendered files are written.
	OutputDir string

	// Proxmox holds Proxmox-specific config values for template substitution.
	Proxmox ProxmoxConfig
}

// Render reads template files and lab provider files, substitutes variables,
// and writes the result to OutputDir.
func Render(opts RenderOptions) error {
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	// Read lab-specific VM definitions (windows.tf, linux.tf from ad/LAB/providers/PROVIDER/).
	labProviderDir := filepath.Join(opts.ProjectRoot, "ad", opts.LabName, "providers", opts.Provider)
	windowsVMs, err := readFileOrEmpty(filepath.Join(labProviderDir, "windows.tf"))
	if err != nil {
		return fmt.Errorf("read lab windows.tf: %w", err)
	}
	linuxVMs, err := readFileOrEmpty(filepath.Join(labProviderDir, "linux.tf"))
	if err != nil {
		return fmt.Errorf("read lab linux.tf: %w", err)
	}

	// Substitute ip_range in VM definitions.
	windowsVMs = strings.ReplaceAll(windowsVMs, "{{ip_range}}", opts.IPRange)
	linuxVMs = strings.ReplaceAll(linuxVMs, "{{ip_range}}", opts.IPRange)

	// Read and render template provider files.
	templateDir := filepath.Join(opts.ProjectRoot, "template", "provider", opts.Provider)
	entries, err := os.ReadDir(templateDir)
	if err != nil {
		return fmt.Errorf("read template dir %s: %w", templateDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tf") {
			continue
		}

		content, err := os.ReadFile(filepath.Join(templateDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read template %s: %w", entry.Name(), err)
		}

		rendered := string(content)
		rendered = strings.ReplaceAll(rendered, "{{windows_vms}}", windowsVMs)
		rendered = strings.ReplaceAll(rendered, "{{linux_vms}}", linuxVMs)
		rendered = strings.ReplaceAll(rendered, "{{ip_range}}", opts.IPRange)
		rendered = strings.ReplaceAll(rendered, "{{lab_identifier}}", opts.LabIdentifier)

		// Substitute config.get_value() calls for Proxmox.
		if opts.Provider == "proxmox" {
			rendered = substituteProxmoxConfig(rendered, opts.Proxmox)
		}

		outPath := filepath.Join(opts.OutputDir, entry.Name())
		if err := os.WriteFile(outPath, []byte(rendered), 0o644); err != nil {
			return fmt.Errorf("write rendered %s: %w", entry.Name(), err)
		}
	}

	return nil
}

// RenderInventory reads the lab's provider-specific inventory template,
// substitutes {{ip_range}}, and writes it to the output path.
func RenderInventory(opts RenderOptions, outputPath string) error {
	labProviderDir := filepath.Join(opts.ProjectRoot, "ad", opts.LabName, "providers", opts.Provider)
	invTemplate := filepath.Join(labProviderDir, "inventory")

	data, err := os.ReadFile(invTemplate)
	if err != nil {
		return fmt.Errorf("read inventory template %s: %w", invTemplate, err)
	}

	rendered := strings.ReplaceAll(string(data), "{{ip_range}}", opts.IPRange)

	if err := os.WriteFile(outputPath, []byte(rendered), 0o644); err != nil {
		return fmt.Errorf("write inventory: %w", err)
	}
	return nil
}

// configGetValuePattern matches {{config.get_value('section', 'key', fallback)}}
// and {{config.get_value('section', 'key', 'fallback')}}.
var configGetValuePattern = regexp.MustCompile(`\{\{config\.get_value\('([^']+)',\s*'([^']+)'(?:,\s*(?:'([^']*)'|(\d+)))?\)\}\}`)

func proxmoxConfigValues(cfg ProxmoxConfig) map[string]string {
	return map[string]string{
		"pm_api_url":        cfg.APIURL,
		"pm_user":           cfg.User,
		"pm_node":           cfg.Node,
		"pm_pool":           cfg.Pool,
		"pm_full_clone":     cfg.FullClone,
		"pm_storage":        cfg.Storage,
		"pm_vlan":           cfg.VLAN,
		"pm_network_bridge": cfg.NetworkBridge,
		"pm_network_model":  cfg.NetworkModel,
	}
}

func substituteProxmoxConfig(content string, cfg ProxmoxConfig) string {
	values := proxmoxConfigValues(cfg)
	return configGetValuePattern.ReplaceAllStringFunc(content, func(match string) string {
		groups := configGetValuePattern.FindStringSubmatch(match)
		if len(groups) < 3 {
			return match
		}
		section := groups[1]
		key := groups[2]
		fallback := groups[3]
		if fallback == "" {
			fallback = groups[4] // numeric fallback
		}

		if section == "proxmox" {
			if v, ok := values[key]; ok {
				return valueOrFallback(v, fallback)
			}
		}

		if section == "proxmox_templates_id" {
			if id, ok := cfg.TemplateIDs[key]; ok {
				return id
			}
			for k, v := range cfg.TemplateIDs {
				if strings.EqualFold(k, key) {
					return v
				}
			}
		}

		return fallback
	})
}

func valueOrFallback(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func readFileOrEmpty(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}
