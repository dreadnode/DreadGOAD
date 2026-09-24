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

// These values describe identities created by a specific infrastructure
// provider. A stale reference inventory must not carry them across providers.
var providerOwnedInventoryVars = []string{"admin_user"}

type inventorySection struct {
	members []string
}

type canonicalInventory struct {
	order     []string
	sections  map[string]inventorySection
	varOrder  []string
	variables map[string]string
}

type inventoryRepairPlan struct {
	missingMembers  map[string][]string
	missingSections map[string]bool
	missingVars     []string
	groupsAdded     int
	membersAdded    int
}

func (p inventoryRepairPlan) empty() bool {
	return p.groupsAdded == 0 && p.membersAdded == 0 && len(p.missingVars) == 0
}

// ensureInventoryTopology restores the lab group graph and range defaults to an
// existing runtime inventory. Runtime inventories own connection details (live
// IPs, instance IDs, credentials); ad/<lab>/data/inventory owns host-to-group
// membership and non-connection [all:vars] defaults. Keeping those
// responsibilities separate lets this repair stale inventories without
// replacing values that were synchronized from the provider.
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
	if err := ensureInventoryTopologyFromSource(
		cfg.InventoryPath(),
		filepath.Join(labRoot, "data", "inventory"),
	); err != nil {
		return err
	}
	return ensureProviderInventoryVariables(cfg)
}

// ensureInventoryTopologyFromSource adds missing topology sections, members,
// and non-connection [all:vars] defaults from canonicalPath to inventoryPath.
// It never removes runtime content or replaces existing values. Canonical
// members absent from the runtime host set are filtered out, preserving
// deliberately reduced lab topologies. Canonical ansible_* variables are not
// copied because provider-synchronized connection settings belong exclusively
// to the runtime inventory.
func ensureInventoryTopologyFromSource(inventoryPath, canonicalPath string) error {
	canonical, isAD, err := loadCanonicalInventory(canonicalPath)
	if err != nil {
		return err
	}
	if !isAD {
		return nil
	}

	runtimeRaw, activeHosts, err := loadRuntimeInventory(inventoryPath)
	if err != nil {
		return err
	}
	if err := filterCanonicalTopology(canonical.sections, activeHosts, inventoryPath); err != nil {
		return err
	}

	_, runtimeSections := topologySections(string(runtimeRaw))
	_, runtimeVars := inventoryAllVars(string(runtimeRaw))
	plan := planInventoryRepair(canonical, runtimeSections, runtimeVars)
	if plan.empty() {
		return validateInventoryContract(inventoryPath, canonical.sections, canonical.variables)
	}

	updated := addInventoryTopology(
		string(runtimeRaw), canonical.order, plan.missingSections, plan.missingMembers,
	)
	updated = addInventoryVariables(updated, plan.missingVars)
	if err := writeInventoryAtomically(inventoryPath, []byte(updated)); err != nil {
		return err
	}
	if err := validateInventoryContract(inventoryPath, canonical.sections, canonical.variables); err != nil {
		return err
	}
	slog.Info("repaired inventory topology",
		"inventory", inventoryPath,
		"source", canonicalPath,
		"groups_added", plan.groupsAdded,
		"members_added", plan.membersAdded,
		"variables_added", len(plan.missingVars))
	return nil
}

func loadCanonicalInventory(path string) (canonicalInventory, bool, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return canonicalInventory{}, false, fmt.Errorf("canonical AD inventory topology is missing: %s", path)
	}
	if err != nil {
		return canonicalInventory{}, false, fmt.Errorf("read canonical inventory topology: %w", err)
	}

	order, sections := topologySections(string(raw))
	varOrder, variables := inventoryAllVars(string(raw))
	_, hasDomain := sections["domain"]
	_, hasDC := sections["dc"]
	if !hasDomain && !hasDC {
		return canonicalInventory{}, false, nil
	}
	if !hasDomain || !hasDC {
		return canonicalInventory{}, false,
			fmt.Errorf("canonical inventory %s has incomplete AD topology (need [domain] and [dc])", path)
	}
	return canonicalInventory{
		order: order, sections: sections, varOrder: varOrder, variables: variables,
	}, true, nil
}

func loadRuntimeInventory(path string) ([]byte, map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read runtime inventory: %w", err)
	}
	parsed, err := inv.Parse(path)
	if err != nil {
		return nil, nil, fmt.Errorf("parse runtime inventory: %w", err)
	}
	if len(parsed.Hosts) == 0 {
		return nil, nil, fmt.Errorf("inventory %s has AD topology but no runtime hosts", path)
	}
	activeHosts := make(map[string]bool, len(parsed.Hosts))
	for host := range parsed.Hosts {
		activeHosts[strings.ToLower(host)] = true
	}
	return raw, activeHosts, nil
}

// filterCanonicalTopology preserves deliberately reduced GOAD-Light/Mini and
// --hosts deployments while retaining every applicable canonical group.
func filterCanonicalTopology(sections map[string]inventorySection, activeHosts map[string]bool, inventoryPath string) error {
	for name, section := range sections {
		filtered := section.members[:0]
		for _, host := range section.members {
			if activeHosts[strings.ToLower(host)] {
				filtered = append(filtered, host)
			}
		}
		section.members = filtered
		sections[name] = section
	}
	if len(sections["dc"].members) == 0 {
		return fmt.Errorf("inventory %s has no deployed host belonging to canonical [dc] topology", inventoryPath)
	}
	return nil
}

func planInventoryRepair(
	canonical canonicalInventory,
	runtimeSections map[string]inventorySection,
	runtimeVars map[string]string,
) inventoryRepairPlan {
	plan := inventoryRepairPlan{
		missingMembers:  make(map[string][]string),
		missingSections: make(map[string]bool),
		missingVars:     make([]string, 0, len(canonical.variables)),
	}
	for _, name := range canonical.order {
		expected := canonical.sections[name]
		current, exists := runtimeSections[name]
		if !exists {
			plan.missingSections[name] = true
			plan.groupsAdded++
		}
		seen := make(map[string]bool, len(current.members))
		for _, member := range current.members {
			seen[strings.ToLower(member)] = true
		}
		for _, member := range expected.members {
			if !seen[strings.ToLower(member)] {
				plan.missingMembers[name] = append(plan.missingMembers[name], member)
				plan.membersAdded++
			}
		}
	}
	for _, key := range canonical.varOrder {
		if strings.HasPrefix(strings.ToLower(key), "ansible_") {
			continue
		}
		if _, exists := runtimeVars[key]; exists {
			continue
		}
		plan.missingVars = append(plan.missingVars, canonical.variables[key])
	}
	return plan
}

// ensureProviderInventoryVariables corrects values that belong to the active
// infrastructure provider rather than to live host connectivity. In
// particular, Azure expects the built-in Administrator identity while AWS
// uses goadmin. Copying an inventory from another provider must not preserve a
// stale admin_user and then authenticate AD mutations as the wrong principal.
func ensureProviderInventoryVariables(cfg *config.Config) error {
	sourceVars, sourcePath, err := loadProviderOwnedInventoryVariables(cfg)
	if err != nil {
		return err
	}

	runtimePath := cfg.InventoryPath()
	runtimeRaw, err := os.ReadFile(runtimePath)
	if err != nil {
		return fmt.Errorf("read runtime provider variables: %w", err)
	}
	updated := string(runtimeRaw)
	changed := 0
	for _, key := range providerOwnedInventoryVars {
		assignment, exists := sourceVars[key]
		if !exists {
			continue
		}
		var replaced bool
		updated, replaced = replaceInventoryVariable(updated, key, assignment)
		if replaced {
			changed++
		}
	}
	if changed == 0 {
		return nil
	}
	if err := writeInventoryAtomically(runtimePath, []byte(updated)); err != nil {
		return err
	}
	_, actual := inventoryAllVars(updated)
	for _, key := range providerOwnedInventoryVars {
		expected, exists := sourceVars[key]
		if exists && actual[key] != expected {
			return fmt.Errorf("inventory %s has incorrect provider-owned variable %s", runtimePath, key)
		}
	}
	slog.Info("reconciled provider inventory variables",
		"inventory", runtimePath,
		"source", sourcePath,
		"variables_updated", changed)
	return nil
}

func loadProviderOwnedInventoryVariables(cfg *config.Config) (map[string]string, string, error) {
	providerPath := providerInventoryVariableSourcePath(cfg)
	rangeRoot := filepath.Dir(filepath.Dir(filepath.Dir(providerPath)))
	candidates := []string{providerPath, filepath.Join(rangeRoot, "data", "inventory")}
	variables := make(map[string]string, len(providerOwnedInventoryVars))
	sourcePath := ""
	for _, candidate := range candidates {
		raw, err := os.ReadFile(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("read provider inventory variables: %w", err)
		}
		_, available := inventoryAllVars(string(raw))
		for _, key := range providerOwnedInventoryVars {
			if _, found := variables[key]; found {
				continue
			}
			if assignment, found := available[key]; found {
				variables[key] = assignment
				if sourcePath == "" {
					sourcePath = candidate
				}
			}
		}
	}
	if len(variables) == 0 {
		return nil, "", fmt.Errorf(
			"provider-owned inventory variables are missing from %s and its lab defaults", providerPath,
		)
	}
	return variables, sourcePath, nil
}

// providerInventoryVariableSourcePath uses the authored variant source for
// provider-owned identities. Generated targets own randomized topology, but
// older targets may themselves contain stale provider values and therefore
// cannot safely act as the authority for admin_user.
func providerInventoryVariableSourcePath(cfg *config.Config) string {
	providerName := cfg.ResolvedProvider()
	if cfg.ActiveEnvironment().Variant {
		if source, _ := cfg.ResolvedVariantPaths(); source != "" {
			return filepath.Join(source, "providers", providerName, "inventory")
		}
	}
	return providerInventoryTemplatePath(cfg)
}

func replaceInventoryVariable(content, key, assignment string) (string, bool) {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	inAllVars := false
	found := false
	changed := false
	for i, line := range lines {
		if match := inventorySectionRe.FindStringSubmatch(line); match != nil {
			inAllVars = strings.EqualFold(strings.TrimSpace(match[1]), "all:vars")
			continue
		}
		if !inAllVars {
			continue
		}
		equals := strings.Index(line, "=")
		if equals <= 0 || strings.TrimSpace(line[:equals]) != key {
			continue
		}
		found = true
		if strings.TrimSpace(line) != assignment {
			lines[i] = assignment
			changed = true
		}
	}
	if !found {
		return addInventoryVariables(content, []string{assignment}), true
	}
	if !changed {
		return content, false
	}
	return strings.Join(lines, "\n") + "\n", true
}

// inventoryAllVars returns assignments from [all:vars] in source order. The
// original assignment line is retained so list and quoted values survive
// without a lossy parse/render cycle.
func inventoryAllVars(content string) ([]string, map[string]string) {
	order := []string{}
	variables := make(map[string]string)
	inAllVars := false
	for _, line := range strings.Split(content, "\n") {
		if match := inventorySectionRe.FindStringSubmatch(line); match != nil {
			inAllVars = strings.EqualFold(strings.TrimSpace(match[1]), "all:vars")
			continue
		}
		if !inAllVars {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		equals := strings.Index(trimmed, "=")
		if equals <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:equals])
		if key == "" {
			continue
		}
		if _, exists := variables[key]; !exists {
			order = append(order, key)
		}
		variables[key] = trimmed
	}
	return order, variables
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

func addInventoryVariables(content string, missing []string) string {
	if len(missing) == 0 {
		return content
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	out := make([]string, 0, len(lines)+len(missing)+2)
	inAllVars := false
	inserted := false
	insert := func() {
		if !inAllVars || inserted {
			return
		}
		out = append(out, missing...)
		inserted = true
	}
	for _, line := range lines {
		if match := inventorySectionRe.FindStringSubmatch(line); match != nil {
			insert()
			inAllVars = strings.EqualFold(strings.TrimSpace(match[1]), "all:vars")
		}
		out = append(out, line)
	}
	insert()
	if !inserted {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, "[all:vars]")
		out = append(out, missing...)
	}
	return strings.Join(out, "\n") + "\n"
}

func validateInventoryContract(
	inventoryPath string,
	canonical map[string]inventorySection,
	canonicalVars map[string]string,
) error {
	if err := validateInventoryTopology(inventoryPath, canonical); err != nil {
		return err
	}
	raw, err := os.ReadFile(inventoryPath)
	if err != nil {
		return fmt.Errorf("read repaired inventory variables: %w", err)
	}
	_, actual := inventoryAllVars(string(raw))
	for key := range canonicalVars {
		if strings.HasPrefix(strings.ToLower(key), "ansible_") {
			continue
		}
		if _, exists := actual[key]; !exists {
			return fmt.Errorf("inventory %s is missing required [all:vars] value %s", inventoryPath, key)
		}
	}
	return nil
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
