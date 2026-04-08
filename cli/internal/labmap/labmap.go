package labmap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// HostInfo holds hostname and domain mappings for a single host (variant support).
type HostInfo struct {
	OldHostname string `json:"old_hostname"`
	NewHostname string `json:"new_hostname"`
	OldFQDN     string `json:"old_fqdn"`
	NewFQDN     string `json:"new_fqdn"`
	OldDomain   string `json:"old_domain"`
	NewDomain   string `json:"new_domain"`
}

// HostConfig represents a host from config.json lab.hosts.
type HostConfig struct {
	Hostname  string                     `json:"hostname"`
	Type      string                     `json:"type"` // "dc" or "server"
	OS        string                     `json:"os"`   // empty = windows, "linux" for linux
	Domain    string                     `json:"domain"`
	Path      string                     `json:"path"`
	Scripts   []string                   `json:"scripts"`
	Vulns     []string                   `json:"vulns"`
	VulnsVars map[string]json.RawMessage `json:"vulns_vars"`
	Security  []string                   `json:"security"`
	UseLAPS   bool                       `json:"use_laps"`
	MSSQL     *MSSQLConfig               `json:"mssql"`
}

type MSSQLLinkedServer struct {
	DataSrc string `json:"data_src"`
}

type MSSQLConfig struct {
	SAPassword     string                       `json:"sa_password"`
	ServiceAccount string                       `json:"svcaccount"`
	SysAdmins      []string                     `json:"sysadmins"`
	ExecuteAsLogin map[string]string            `json:"executeaslogin"`
	LinkedServers  map[string]MSSQLLinkedServer `json:"linked_servers"`
}

// UserConfig represents a user from config.json domains[*].users[*].
type UserConfig struct {
	FirstName   string   `json:"firstname"`
	Surname     string   `json:"surname"`
	Password    string   `json:"password"`
	Description string   `json:"description"`
	Groups      []string `json:"groups"`
	Path        string   `json:"path"`
	SPNs        []string `json:"spns"`
}

// ACLConfig represents an ACL entry from config.json domains[*].acls[*].
type ACLConfig struct {
	Name        string // key from the acls map
	For         string `json:"for"`
	To          string `json:"to"`
	Right       string `json:"right"`
	Inheritance string `json:"inheritance"`
}

type GMSAConfig struct {
	Name      string   `json:"gMSA_Name"`
	FQDN      string   `json:"gMSA_FQDN"`
	SPNs      []string `json:"gMSA_SPNs"`
	HostNames []string `json:"gMSA_HostNames"`
}

// DomainConfig represents a domain from config.json lab.domains.
type DomainConfig struct {
	DC              string                `json:"dc"` // host role key
	NetBIOSName     string                `json:"netbios_name"`
	Trust           string                `json:"trust"`
	CAServer        string                `json:"ca_server"`
	CAWebEnrollment *bool                 `json:"ca_web_enrollment"`
	LAPSReaders     []string              `json:"laps_readers"`
	GMSA            map[string]GMSAConfig `json:"gmsa"`
	Users           map[string]UserConfig `json:"users"`
	ACLs            map[string]ACLConfig  `json:"acls"`
}

// LabMap holds the resolved lab configuration for any GOAD-style lab.
type LabMap struct {
	// Domain shortcuts — populated from DomainConfigs for backward compat.
	// Empty if the lab doesn't have that many domains.
	RootDomain   string
	ChildDomain  string
	ForestDomain string

	// Host name/domain mappings (variant-aware).
	Hosts map[string]HostInfo // keyed by role (dc01, srv02, etc.)
	// Variant user mapping (old -> new). Nil for non-variant.
	Users map[string]string

	// Full parsed config data.
	HostConfigs   map[string]HostConfig   // keyed by role
	DomainConfigs map[string]DomainConfig // keyed by domain FQDN
}

// FQDN returns the FQDN for a host role.
func (m *LabMap) FQDN(role string) string {
	if h, ok := m.Hosts[role]; ok {
		return h.NewFQDN
	}
	return ""
}

// Hostname returns the hostname for a host role.
func (m *LabMap) Hostname(role string) string {
	if h, ok := m.Hosts[role]; ok {
		return h.NewHostname
	}
	return ""
}

// User returns the mapped username, or the original if no mapping exists.
func (m *LabMap) User(original string) string {
	if m.Users != nil {
		if mapped, ok := m.Users[original]; ok {
			return mapped
		}
	}
	return original
}

// HostRoles returns all host role keys sorted alphabetically.
func (m *LabMap) HostRoles() []string {
	roles := make([]string, 0, len(m.HostConfigs))
	for role := range m.HostConfigs {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles
}

// DCs returns host roles that are domain controllers.
func (m *LabMap) DCs() []string {
	var dcs []string
	for role, hc := range m.HostConfigs {
		if hc.Type == "dc" {
			dcs = append(dcs, role)
		}
	}
	sort.Strings(dcs)
	return dcs
}

// WindowsServers returns host roles that are Windows servers (not DCs, not linux).
func (m *LabMap) WindowsServers() []string {
	var servers []string
	for role, hc := range m.HostConfigs {
		if hc.Type == "server" && !strings.EqualFold(hc.OS, "linux") {
			servers = append(servers, role)
		}
	}
	sort.Strings(servers)
	return servers
}

// WindowsHosts returns all host roles that are Windows (DCs + Windows servers).
func (m *LabMap) WindowsHosts() []string {
	var hosts []string
	for role, hc := range m.HostConfigs {
		if !strings.EqualFold(hc.OS, "linux") {
			_ = hc
			hosts = append(hosts, role)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// DCForDomain returns the host role that is the DC for a given domain FQDN.
func (m *LabMap) DCForDomain(domain string) string {
	if dc, ok := m.DomainConfigs[domain]; ok {
		return dc.DC
	}
	return ""
}

// DomainForHost returns the domain FQDN for a given host role.
func (m *LabMap) DomainForHost(role string) string {
	if hc, ok := m.HostConfigs[role]; ok {
		return hc.Domain
	}
	return ""
}

// Domains returns all domain FQDNs sorted alphabetically.
func (m *LabMap) Domains() []string {
	domains := make([]string, 0, len(m.DomainConfigs))
	for d := range m.DomainConfigs {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	return domains
}

// --- Vulnerability fact queries ---

// UserFact holds a user with context about which domain they belong to.
type UserFact struct {
	Username string
	Domain   string
	DCRole   string // host role of the domain's DC
	User     UserConfig
}

// TrustFact holds a trust relationship between two domains.
type TrustFact struct {
	SourceDomain string
	TargetDomain string
	SourceDCRole string
	TargetDCRole string
}

// ACLFact holds an ACL entry with its domain context.
type ACLFact struct {
	Domain string
	DCRole string
	ACL    ACLConfig
}

// UsersWithPasswordInDescription returns users whose description contains their password.
func (m *LabMap) UsersWithPasswordInDescription() []UserFact {
	var facts []UserFact
	for domain, dc := range m.DomainConfigs {
		for username, user := range dc.Users {
			if user.Password != "" && user.Description != "" &&
				strings.Contains(strings.ToLower(user.Description), strings.ToLower(user.Password)) {
				facts = append(facts, UserFact{
					Username: m.User(username),
					Domain:   domain,
					DCRole:   dc.DC,
					User:     user,
				})
			}
		}
	}
	return facts
}

// UsersWithSPNs returns users that have SPNs configured (kerberoastable).
func (m *LabMap) UsersWithSPNs() []UserFact {
	var facts []UserFact
	for domain, dc := range m.DomainConfigs {
		for username, user := range dc.Users {
			if len(user.SPNs) > 0 {
				facts = append(facts, UserFact{
					Username: m.User(username),
					Domain:   domain,
					DCRole:   dc.DC,
					User:     user,
				})
			}
		}
	}
	return facts
}

// HostsWithScript returns host roles that have the given script name in their scripts list.
func (m *LabMap) HostsWithScript(scriptPattern string) []string {
	var hosts []string
	pattern := strings.ToLower(scriptPattern)
	for role, hc := range m.HostConfigs {
		for _, script := range hc.Scripts {
			if strings.Contains(strings.ToLower(script), pattern) {
				hosts = append(hosts, role)
				break
			}
		}
	}
	sort.Strings(hosts)
	return hosts
}

// HostsWithVuln returns host roles that have the given vuln tag.
func (m *LabMap) HostsWithVuln(vuln string) []string {
	var hosts []string
	for role, hc := range m.HostConfigs {
		for _, v := range hc.Vulns {
			if v == vuln {
				hosts = append(hosts, role)
				break
			}
		}
	}
	sort.Strings(hosts)
	return hosts
}

// HostsWithMSSQL returns host roles that have MSSQL configured.
func (m *LabMap) HostsWithMSSQL() []string {
	var hosts []string
	for role, hc := range m.HostConfigs {
		if hc.MSSQL != nil {
			hosts = append(hosts, role)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// ADCSHosts returns host roles that serve as ADCS CA servers, along with the domain.
func (m *LabMap) ADCSHosts() []string {
	caHosts := make(map[string]bool)
	// Find hosts that are CA servers for any domain
	for _, dc := range m.DomainConfigs {
		if dc.CAServer == "" {
			continue
		}
		// CA server name matches a hostname
		for role, hc := range m.HostConfigs {
			if strings.EqualFold(hc.Hostname, dc.CAServer) {
				caHosts[role] = true
			}
		}
	}
	// Also find hosts with adcs_templates in vulns_vars
	for role, hc := range m.HostConfigs {
		if _, ok := hc.VulnsVars["adcs_templates"]; ok {
			caHosts[role] = true
		}
	}
	var hosts []string
	for role := range caHosts {
		hosts = append(hosts, role)
	}
	sort.Strings(hosts)
	return hosts
}

// CAWebEnrollment returns true if any domain has CA web enrollment enabled.
// Defaults to true unless explicitly set to false.
func (m *LabMap) CAWebEnrollment() bool {
	for _, dc := range m.DomainConfigs {
		if dc.CAServer != "" {
			if dc.CAWebEnrollment != nil && !*dc.CAWebEnrollment {
				continue
			}
			return true // default is enabled
		}
	}
	return false
}

// DomainTrusts returns all configured trust relationships.
func (m *LabMap) DomainTrusts() []TrustFact {
	seen := make(map[string]bool)
	var facts []TrustFact
	for domain, dc := range m.DomainConfigs {
		if dc.Trust == "" {
			continue
		}
		// Deduplicate bidirectional trusts
		key := domain + "|" + dc.Trust
		reverseKey := dc.Trust + "|" + domain
		if seen[key] || seen[reverseKey] {
			continue
		}
		seen[key] = true
		targetDC := ""
		if tdc, ok := m.DomainConfigs[dc.Trust]; ok {
			targetDC = tdc.DC
		}
		facts = append(facts, TrustFact{
			SourceDomain: domain,
			TargetDomain: dc.Trust,
			SourceDCRole: dc.DC,
			TargetDCRole: targetDC,
		})
	}
	return facts
}

// AllACLs returns all ACL entries across all domains.
func (m *LabMap) AllACLs() []ACLFact {
	var facts []ACLFact
	for domain, dc := range m.DomainConfigs {
		for name, acl := range dc.ACLs {
			acl.Name = name
			facts = append(facts, ACLFact{
				Domain: domain,
				DCRole: dc.DC,
				ACL:    acl,
			})
		}
	}
	return facts
}

type GMSAFact struct {
	Domain string
	DCRole string
	GMSA   GMSAConfig
}

type LAPSFact struct {
	Domain  string
	DCRole  string
	Readers []string
}

type MSSQLFact struct {
	HostRole string
	Hostname string
	MSSQL    *MSSQLConfig
}

func (m *LabMap) DomainsWithGMSA() []GMSAFact {
	var facts []GMSAFact
	for domain, dc := range m.DomainConfigs {
		for _, gmsa := range dc.GMSA {
			if gmsa.Name != "" {
				facts = append(facts, GMSAFact{
					Domain: domain,
					DCRole: dc.DC,
					GMSA:   gmsa,
				})
			}
		}
	}
	return facts
}

func (m *LabMap) DomainsWithLAPSReaders() []LAPSFact {
	var facts []LAPSFact
	for domain, dc := range m.DomainConfigs {
		if len(dc.LAPSReaders) > 0 {
			facts = append(facts, LAPSFact{
				Domain:  domain,
				DCRole:  dc.DC,
				Readers: dc.LAPSReaders,
			})
		}
	}
	return facts
}

func (m *LabMap) HostsWithMSSQLConfig() []MSSQLFact {
	var facts []MSSQLFact
	roles := m.HostsWithMSSQL()
	for _, role := range roles {
		hc := m.HostConfigs[role]
		facts = append(facts, MSSQLFact{
			HostRole: role,
			Hostname: hc.Hostname,
			MSSQL:    hc.MSSQL,
		})
	}
	return facts
}

func (m *LabMap) HostsWithLAPS() []string {
	var hosts []string
	for role, hc := range m.HostConfigs {
		if hc.UseLAPS {
			hosts = append(hosts, role)
		}
	}
	sort.Strings(hosts)
	return hosts
}

// --- Config parsing ---

// rawLabConfig mirrors the full config.json structure.
type rawLabConfig struct {
	Lab struct {
		Hosts   map[string]json.RawMessage `json:"hosts"`
		Domains map[string]DomainConfig    `json:"domains"`
	} `json:"lab"`
}

// variantMapping mirrors mapping.json.
type variantMapping struct {
	Domains map[string]string   `json:"domains"`
	Hosts   map[string]HostInfo `json:"hosts"`
	Users   map[string]string   `json:"users"`
}

// LoadFromSource reads config.json and builds a fully populated LabMap.
func LoadFromSource(sourceDir string) (*LabMap, error) {
	path := filepath.Join(sourceDir, "data", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read lab config: %w", err)
	}

	return parseConfig(data)
}

// LoadFromVariant reads mapping.json from a variant target directory.
// It also loads the full config from the variant's own data/config.json if it exists,
// falling back to just the mapping data.
func LoadFromVariant(variantTargetDir string) (*LabMap, error) {
	// Try loading the variant's own config.json for full config data
	configPath := filepath.Join(variantTargetDir, "data", "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		lm, parseErr := parseConfig(data)
		if parseErr == nil {
			// Also load mapping.json for the user mapping table
			mappingPath := filepath.Join(variantTargetDir, "mapping.json")
			if mData, mErr := os.ReadFile(mappingPath); mErr == nil {
				var vm variantMapping
				if json.Unmarshal(mData, &vm) == nil {
					lm.Users = vm.Users
					// Use variant host info for name mappings
					if len(vm.Hosts) > 0 {
						lm.Hosts = vm.Hosts
						resolveDomains(lm)
					}
				}
			}
			return lm, nil
		}
	}

	// Fallback: mapping.json only (no full config)
	mappingPath := filepath.Join(variantTargetDir, "mapping.json")
	mData, err := os.ReadFile(mappingPath)
	if err != nil {
		return nil, fmt.Errorf("read variant mapping: %w", err)
	}

	var vm variantMapping
	if err := json.Unmarshal(mData, &vm); err != nil {
		return nil, fmt.Errorf("parse variant mapping: %w", err)
	}

	lm := &LabMap{
		Hosts:         vm.Hosts,
		Users:         vm.Users,
		HostConfigs:   make(map[string]HostConfig),
		DomainConfigs: make(map[string]DomainConfig),
	}
	resolveDomains(lm)
	return lm, nil
}

func parseConfig(data []byte) (*LabMap, error) {
	var raw rawLabConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse lab config: %w", err)
	}

	lm := &LabMap{
		Hosts:         make(map[string]HostInfo, len(raw.Lab.Hosts)),
		HostConfigs:   make(map[string]HostConfig, len(raw.Lab.Hosts)),
		DomainConfigs: raw.Lab.Domains,
	}

	if lm.DomainConfigs == nil {
		lm.DomainConfigs = make(map[string]DomainConfig)
	}

	// Parse each host
	for role, rawHost := range raw.Lab.Hosts {
		var hc HostConfig
		if err := json.Unmarshal(rawHost, &hc); err != nil {
			return nil, fmt.Errorf("parse host %s: %w", role, err)
		}
		lm.HostConfigs[role] = hc

		fqdn := hc.Hostname + "." + hc.Domain
		lm.Hosts[role] = HostInfo{
			OldHostname: hc.Hostname, NewHostname: hc.Hostname,
			OldFQDN: fqdn, NewFQDN: fqdn,
			OldDomain: hc.Domain, NewDomain: hc.Domain,
		}
	}

	resolveDomains(lm)
	return lm, nil
}

// resolveDomains sets RootDomain/ChildDomain/ForestDomain from the domain configs.
// For labs with 1 domain, only RootDomain is set.
// For labs with 2 domains, RootDomain and ChildDomain are set.
// For labs with 3+ domains, all three are set.
func resolveDomains(lm *LabMap) {
	// Collect domains ordered by their DC role (dc01 < dc02 < dc03)
	type domainEntry struct {
		role   string
		domain string
	}
	var entries []domainEntry

	// From DomainConfigs (preferred — has explicit dc mapping)
	if len(lm.DomainConfigs) > 0 {
		for domain, dc := range lm.DomainConfigs {
			entries = append(entries, domainEntry{role: dc.DC, domain: domain})
		}
	} else {
		// Fallback: derive from Hosts
		seen := make(map[string]bool)
		for role, h := range lm.Hosts {
			domain := h.NewDomain
			if domain != "" && !seen[domain] {
				seen[domain] = true
				entries = append(entries, domainEntry{role: role, domain: domain})
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].role < entries[j].role
	})

	if len(entries) > 0 {
		lm.RootDomain = entries[0].domain
	}
	if len(entries) > 1 {
		lm.ChildDomain = entries[1].domain
	}
	if len(entries) > 2 {
		lm.ForestDomain = entries[2].domain
	}
}
