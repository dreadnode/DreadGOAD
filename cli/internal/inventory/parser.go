package inventory

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Host represents a single host in the Ansible inventory.
type Host struct {
	Name       string
	InstanceID string // ansible_host value (e.g. i-0e428dfc02f5007dd or 10.8.1.8 for IP-based connections)
	DictKey    string
	DNSDomain  string
	User       string
	Password   string // ansible_password (single/double quotes stripped)
	Groups     []string
}

// Inventory represents a parsed Ansible inventory file.
type Inventory struct {
	Hosts    map[string]*Host
	Groups   map[string][]string // group name -> host names
	Vars     map[string]string   // [all:vars] section
	FilePath string
}

var (
	sectionRe  = regexp.MustCompile(`^\[([^\]]+)\]`)
	hostLineRe = regexp.MustCompile(`^(\w[\w.-]+)\s+(.+)`)
	varRe      = regexp.MustCompile(`(\w+)=(\S+)`)
)

// Parse reads and parses an Ansible INI-style inventory file.
func Parse(path string) (_ *Inventory, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open inventory %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close inventory %s: %w", path, cerr)
		}
	}()

	inv := &Inventory{
		Hosts:    make(map[string]*Host),
		Groups:   make(map[string][]string),
		Vars:     make(map[string]string),
		FilePath: path,
	}

	scanner := bufio.NewScanner(f)
	currentSection := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}

		if m := sectionRe.FindStringSubmatch(line); m != nil {
			currentSection = m[1]
			continue
		}

		inv.parseLine(line, currentSection)
	}

	return inv, scanner.Err()
}

func (inv *Inventory) parseLine(line, section string) {
	switch section {
	case "all:vars":
		if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
			inv.Vars[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	case "default":
		inv.parseHostDef(line)
	case "":
		// no section yet
	default:
		// Standard Ansible INI: a line in a [group] section can be either a
		// bare hostname (group membership only) or `host k=v k=v` (host def
		// + implicit membership). Routing both shapes through parseHostDef
		// matches what `ansible-inventory` does and is what provisioned
		// inventories like [goad_hosts] rely on.
		if strings.Contains(line, "=") {
			inv.parseHostDef(line)
			if name := strings.Fields(line)[0]; inv.Hosts[name] != nil {
				inv.Groups[section] = append(inv.Groups[section], name)
				inv.Hosts[name].Groups = append(inv.Hosts[name].Groups, section)
			}
		} else {
			inv.parseGroupMembership(line, section)
		}
	}
}

func (inv *Inventory) parseHostDef(line string) {
	m := hostLineRe.FindStringSubmatch(line)
	if m == nil {
		return
	}
	host := &Host{Name: m[1]}
	for _, vm := range varRe.FindAllStringSubmatch(m[2], -1) {
		switch vm[1] {
		case "ansible_host":
			host.InstanceID = vm[2]
		case "dict_key":
			host.DictKey = vm[2]
		case "dns_domain":
			host.DNSDomain = vm[2]
		case "ansible_user":
			host.User = vm[2]
		case "ansible_password":
			host.Password = stripQuotes(vm[2])
		}
	}
	inv.Hosts[host.Name] = host
	inv.Groups["default"] = append(inv.Groups["default"], host.Name)
}

func (inv *Inventory) parseGroupMembership(line, section string) {
	name := strings.Fields(line)[0]
	if _, exists := inv.Hosts[name]; exists {
		inv.Groups[section] = append(inv.Groups[section], name)
		inv.Hosts[name].Groups = append(inv.Hosts[name].Groups, section)
	}
}

// InstanceIDs returns all unique instance IDs from the inventory.
func (inv *Inventory) InstanceIDs() []string {
	seen := make(map[string]struct{})
	var ids []string
	for _, h := range inv.Hosts {
		if h.InstanceID != "" {
			if _, exists := seen[h.InstanceID]; !exists {
				seen[h.InstanceID] = struct{}{}
				ids = append(ids, h.InstanceID)
			}
		}
	}
	return ids
}

// IsSSM returns true if the inventory uses AWS SSM connections.
// Non-SSM inventories (e.g. Ludus, Proxmox, WinRM, SSH) do not
// need AWS-specific operations like session cleanup or instance sync.
func (inv *Inventory) IsSSM() bool {
	conn, ok := inv.Vars["ansible_connection"]
	if !ok {
		return false
	}
	return strings.Contains(conn, "aws_ssm")
}

// Region returns the AWS SSM region from inventory vars, or an empty string
// if the inventory does not specify one. Callers should fall back to
// config.Config.ResolveRegion() rather than hardcoding a default — silently
// picking a region for the user causes confusing "no instances found" errors
// when they're actually deployed in a different region.
func (inv *Inventory) Region() string {
	if r, ok := inv.Vars["ansible_aws_ssm_region"]; ok {
		return r
	}
	return ""
}

// HostByName returns a host by its name (case-insensitive).
func (inv *Inventory) HostByName(name string) *Host {
	name = strings.ToLower(name)
	for k, h := range inv.Hosts {
		if strings.ToLower(k) == name {
			return h
		}
	}
	return nil
}

// HostByInstanceID returns a host by its instance ID.
func (inv *Inventory) HostByInstanceID(id string) *Host {
	for _, h := range inv.Hosts {
		if h.InstanceID == id {
			return h
		}
	}
	return nil
}

// stripQuotes removes one matching pair of surrounding single or double
// quotes from s. Inventory values like ansible_password='abc' come through
// the var regex with the quotes attached.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '\'' && last == '\'') || (first == '"' && last == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
