package cmd

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/rangeconfig"
)

// inventorySectionRe intentionally accepts the same broad INI section syntax
// as Ansible. Group names such as all:vars are filtered by topologySections,
// not rejected by the parser.
var inventorySectionRe = regexp.MustCompile(`^\s*\[([^]]+)]\s*$`)

type inventorySection struct {
	members []string
}

// ensureInventoryTopology restores the lab group graph to an existing runtime
// inventory. Runtime inventories own connection details (live IPs, instance
// IDs, credentials); ad/<lab>/data/inventory owns host-to-group membership.
// Keeping those responsibilities separate lets this repair stale inventories
// without replacing values that were synchronized from the provider.
func ensureInventoryTopology(cfg *config.Config) error {
	manifest, found, err := rangeconfig.Load(cfg.LabPath())
	if err != nil {
		return fmt.Errorf("resolve inventory topology policy: %w", err)
	}
	if found && manifest.Kind != rangeconfig.KindActiveDirectory {
		return nil
	}

	labRoot := cfg.LabPath()
	if cfg.ActiveEnvironment().Variant {
		_, target := cfg.ResolvedVariantPaths()
		if target != "" {
			labRoot = target
		}
	}
	return ensureInventoryTopologyFromSource(
		cfg.InventoryPath(),
		filepath.Join(labRoot, "data", "inventory"),
	)
}

// ensureInventoryTopologyFromSource adds missing topology sections and members
// from canonicalPath to inventoryPath. It never removes runtime groups or
// changes host definitions, variables, addresses, usernames, or passwords.
// Canonical members absent from the runtime [default] host set are filtered
// out, preserving deliberately reduced lab topologies.
func ensureInventoryTopologyFromSource(inventoryPath, canonicalPath string) error {
	canonicalRaw, err := os.ReadFile(canonicalPath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("canonical AD inventory topology is missing: %s", canonicalPath)
	}
	if err != nil {
		return fmt.Errorf("read canonical inventory topology: %w", err)
	}

	canonicalOrder, canonical := topologySections(string(canonicalRaw))
	_, hasDomain := canonical["domain"]
	_, hasDC := canonical["dc"]
	if !hasDomain && !hasDC {
		return nil // This is not an Active Directory inventory.
	}
	if !hasDomain || !hasDC {
		return fmt.Errorf("canonical inventory %s has incomplete AD topology (need [domain] and [dc])", canonicalPath)
	}

	runtimeRaw, err := os.ReadFile(inventoryPath)
	if err != nil {
		return fmt.Errorf("read runtime inventory: %w", err)
	}
	parsed, err := inv.Parse(inventoryPath)
	if err != nil {
		return fmt.Errorf("parse runtime inventory: %w", err)
	}
	if len(parsed.Hosts) == 0 {
		return fmt.Errorf("inventory %s has AD topology but no runtime hosts", inventoryPath)
	}
	activeHosts := make(map[string]bool, len(parsed.Hosts))
	for host := range parsed.Hosts {
		activeHosts[strings.ToLower(host)] = true
	}

	// Filter canonical membership to the hosts actually deployed in this
	// environment. This is essential for GOAD-Light/Mini and --hosts ranges.
	for name, section := range canonical {
		filtered := section.members[:0]
		for _, host := range section.members {
			if activeHosts[strings.ToLower(host)] {
				filtered = append(filtered, host)
			}
		}
		section.members = filtered
		canonical[name] = section
	}
	if len(canonical["dc"].members) == 0 {
		return fmt.Errorf("inventory %s has no deployed host belonging to canonical [dc] topology", inventoryPath)
	}

	_, runtime := topologySections(string(runtimeRaw))
	missingMembers := make(map[string][]string)
	missingSections := make(map[string]bool)
	groupsAdded := 0
	membersAdded := 0

	for _, name := range canonicalOrder {
		expected := canonical[name]
		current, exists := runtime[name]
		if !exists {
			missingSections[name] = true
			groupsAdded++
		}
		seen := make(map[string]bool, len(current.members))
		for _, member := range current.members {
			seen[strings.ToLower(member)] = true
		}
		for _, member := range expected.members {
			if !seen[strings.ToLower(member)] {
				missingMembers[name] = append(missingMembers[name], member)
				membersAdded++
			}
		}
	}

	if groupsAdded == 0 && membersAdded == 0 {
		return validateInventoryTopology(inventoryPath, canonical)
	}

	updated := addInventoryTopology(
		string(runtimeRaw), canonicalOrder, missingSections, missingMembers,
	)
	if err := writeInventoryAtomically(inventoryPath, []byte(updated)); err != nil {
		return err
	}
	if err := validateInventoryTopology(inventoryPath, canonical); err != nil {
		return err
	}
	slog.Info("repaired inventory topology",
		"inventory", inventoryPath,
		"source", canonicalPath,
		"groups_added", groupsAdded,
		"members_added", membersAdded)
	return nil
}

// topologySections returns ordinary inventory groups in source order. Variable
// sections and host-definition sections are not topology and are excluded.
func topologySections(content string) ([]string, map[string]inventorySection) {
	order := []string{}
	sections := make(map[string]inventorySection)
	current := ""
	for _, line := range strings.Split(content, "\n") {
		if match := inventorySectionRe.FindStringSubmatch(line); match != nil {
			name := strings.ToLower(strings.TrimSpace(match[1]))
			if name == "default" || name == "all" || strings.Contains(name, ":") {
				current = ""
				continue
			}
			current = name
			if _, exists := sections[name]; !exists {
				sections[name] = inventorySection{}
				order = append(order, name)
			}
			continue
		}
		if current == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			continue
		}
		section := sections[current]
		section.members = append(section.members, fields[0])
		sections[current] = section
	}
	return order, sections
}

func addInventoryTopology(
	content string,
	canonicalOrder []string,
	missingSections map[string]bool,
	missingMembers map[string][]string,
) string {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	out := make([]string, 0, len(lines)+len(canonicalOrder)*2)
	current := ""
	flushed := make(map[string]bool)
	flush := func() {
		if current == "" || missingSections[current] || flushed[current] {
			return
		}
		out = append(out, missingMembers[current]...)
		flushed[current] = true
	}

	for _, line := range lines {
		if match := inventorySectionRe.FindStringSubmatch(line); match != nil {
			flush()
			current = strings.ToLower(strings.TrimSpace(match[1]))
		}
		out = append(out, line)
	}
	flush()

	for _, name := range canonicalOrder {
		if !missingSections[name] {
			continue
		}
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, "["+name+"]")
		out = append(out, missingMembers[name]...)
	}
	return strings.Join(out, "\n") + "\n"
}

func validateInventoryTopology(inventoryPath string, canonical map[string]inventorySection) error {
	raw, err := os.ReadFile(inventoryPath)
	if err != nil {
		return fmt.Errorf("read repaired inventory: %w", err)
	}
	_, actual := topologySections(string(raw))
	for name, expected := range canonical {
		current, exists := actual[name]
		if !exists {
			return fmt.Errorf("inventory %s is missing required [%s] topology group", inventoryPath, name)
		}
		seen := make(map[string]bool, len(current.members))
		for _, member := range current.members {
			seen[strings.ToLower(member)] = true
		}
		for _, member := range expected.members {
			if !seen[strings.ToLower(member)] {
				return fmt.Errorf("inventory %s group [%s] is missing host %s", inventoryPath, name, member)
			}
		}
	}
	return nil
}

func writeInventoryAtomically(path string, content []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat inventory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".inventory-topology-*")
	if err != nil {
		return fmt.Errorf("create temporary inventory: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temporary inventory permissions: %w", err)
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary inventory: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary inventory: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary inventory: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace inventory: %w", err)
	}
	return nil
}
