package variant

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// CompletionMarkerName is written only after a variant generation run reaches
// the end successfully. Its absence means an existing target may be partial.
const CompletionMarkerName = ".dreadgoad-variant-complete"

const completionMarkerContent = "complete\n"

// IsComplete reports whether target contains a valid completion marker.
func IsComplete(target string) (bool, error) {
	marker := filepath.Join(target, CompletionMarkerName)
	data, err := os.ReadFile(marker)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect completion marker: %w", err)
	}
	if string(data) != completionMarkerContent {
		return false, fmt.Errorf("completion marker has invalid contents: %s", marker)
	}
	return true, nil
}

func writeCompletionMarker(target string) error {
	marker := filepath.Join(target, CompletionMarkerName)
	tmp := marker + ".tmp"
	if err := os.WriteFile(tmp, []byte(completionMarkerContent), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, marker); err != nil {
		if removeErr := os.Remove(tmp); removeErr != nil {
			return fmt.Errorf("rename marker: %w; cleanup: %v", err, removeErr)
		}
		return fmt.Errorf("rename marker: %w", err)
	}
	return nil
}

// LabConfig is the top-level structure of a GOAD config.json.
// All known fields are modeled; if a config adds new top-level keys they
// must be added here to survive the transform round-trip in transformFile.
type LabConfig struct {
	Lab struct {
		Hosts   map[string]*HostConfig   `json:"hosts"`
		Domains map[string]*DomainConfig `json:"domains"`
	} `json:"lab"`
}

// HostConfig represents a single host entry in the lab config.
type HostConfig struct {
	Hostname           string              `json:"hostname"`
	Type               string              `json:"type"`
	LocalAdminPassword string              `json:"local_admin_password"`
	Domain             string              `json:"domain"`
	Path               string              `json:"path"`
	UseLaps            *bool               `json:"use_laps,omitempty"`
	LocalGroups        map[string][]string `json:"local_groups,omitempty"`
	Scripts            []string            `json:"scripts,omitempty"`
	Vulns              []string            `json:"vulns,omitempty"`
	VulnsVars          map[string]any      `json:"vulns_vars,omitempty"`
	Security           []string            `json:"security,omitempty"`
	SecurityVars       map[string]any      `json:"security_vars,omitempty"`
	MSSQL              *MSSQLConfig        `json:"mssql,omitempty"`
	// RemoteDesktopUsers appears at host top-level in some upstream GOAD configs.
	RemoteDesktopUsers []string `json:"Remote Desktop Users,omitempty"`
	// OS marks non-Windows hosts (DRACARYS uses "linux" for lx01).
	OS string `json:"os,omitempty"`
	// VulnsADCSTemplates lists the certificate templates published for this
	// host; read by cli/internal/ansible/prepare.go to drive the
	// vulns_adcs_templates role.
	VulnsADCSTemplates []string `json:"vulns_adcs_templates,omitempty"`
}

// MSSQLConfig holds MSSQL server configuration for a host.
type MSSQLConfig struct {
	SAPassword     string                        `json:"sa_password"`
	SVCAccount     string                        `json:"svcaccount"`
	SysAdmins      []string                      `json:"sysadmins"`
	ExecuteAsLogin map[string]string             `json:"executeaslogin,omitempty"`
	ExecuteAsUser  map[string]ExecuteAsUserEntry `json:"executeasuser,omitempty"`
	LinkedServers  map[string]LinkedServerConfig `json:"linked_servers,omitempty"`
}

// ExecuteAsUserEntry describes an impersonation mapping in MSSQL.
type ExecuteAsUserEntry struct {
	User        string `json:"user"`
	DB          string `json:"db"`
	Impersonate string `json:"impersonate"`
}

// LinkedServerConfig describes a linked MSSQL server.
type LinkedServerConfig struct {
	DataSrc      string                `json:"data_src"`
	UsersMapping []LinkedServerMapping `json:"users_mapping"`
}

// LinkedServerMapping maps a local login to a remote login on a linked server.
type LinkedServerMapping struct {
	LocalLogin     string `json:"local_login"`
	RemoteLogin    string `json:"remote_login"`
	RemotePassword string `json:"remote_password"`
}

// DomainConfig represents a single domain entry in the lab config.
type DomainConfig struct {
	DC                      string                 `json:"dc"`
	DomainPassword          string                 `json:"domain_password"`
	NetBIOSName             string                 `json:"netbios_name"`
	CAServer                string                 `json:"ca_server,omitempty"`
	Trust                   string                 `json:"trust"`
	LapsPath                string                 `json:"laps_path,omitempty"`
	OrganisationUnits       map[string]OUConfig    `json:"organisation_units"`
	LapsReaders             []string               `json:"laps_readers,omitempty"`
	Groups                  GroupsConfig           `json:"groups"`
	MultiDomainGroupsMember map[string][]string    `json:"multi_domain_groups_member,omitempty"`
	GMSA                    map[string]GMSAConfig  `json:"gmsa,omitempty"`
	ACLs                    map[string]ACLConfig   `json:"acls"`
	Users                   map[string]*UserConfig `json:"users"`
	// CAWebEnrollment is a pointer so an explicit false survives the round-trip.
	// ansible/playbooks/adcs.yml treats absent as true, so dropping a false here
	// silently turns CA web enrollment back on in the generated variant.
	CAWebEnrollment *bool `json:"ca_web_enrollment,omitempty"`
	// SCCM holds the MECM/SCCM site configuration (SCCM lab only). Kept as a raw
	// map because the generator has no reason to reshape it; entity names inside
	// are still randomized by the text-level replacement pass.
	SCCM map[string]any `json:"sccm,omitempty"`
}

// OUConfig represents an organisational unit.
type OUConfig struct {
	Path string `json:"path"`
}

// GroupsConfig holds groups categorized by scope.
type GroupsConfig struct {
	Universal   map[string]GroupConfig `json:"universal"`
	Global      map[string]GroupConfig `json:"global"`
	DomainLocal map[string]GroupConfig `json:"domainlocal"`
}

// GroupConfig represents a single AD group.
type GroupConfig struct {
	ManagedBy string   `json:"managed_by,omitempty"`
	Path      string   `json:"path"`
	Members   []string `json:"members,omitempty"`
}

// GMSAConfig represents a group Managed Service Account.
type GMSAConfig struct {
	Name      string   `json:"gMSA_Name"`
	FQDN      string   `json:"gMSA_FQDN"`
	SPNs      []string `json:"gMSA_SPNs"`
	HostNames []string `json:"gMSA_HostNames"`
}

// ACLConfig represents a single ACL entry.
type ACLConfig struct {
	For         string `json:"for"`
	To          string `json:"to"`
	Right       string `json:"right"`
	Inheritance string `json:"inheritance"`
}

// UserConfig represents a single AD user.
type UserConfig struct {
	Firstname   string   `json:"firstname"`
	Surname     string   `json:"surname"`
	Password    string   `json:"password"`
	City        string   `json:"city"`
	Description string   `json:"description"`
	Groups      []string `json:"groups"`
	Path        string   `json:"path"`
	SPNs        []string `json:"spns,omitempty"`
}

// Mappings holds all entity-to-entity name mappings.
type Mappings struct {
	Domains   map[string]string      `json:"domains"`
	NetBIOS   map[string]string      `json:"netbios"`
	Hosts     map[string]HostMapping `json:"hosts"`
	Users     map[string]string      `json:"users"`
	Passwords map[string]string      `json:"passwords"`
	Groups    map[string]string      `json:"groups"`
	OUs       map[string]string      `json:"ous"`
	ACLs      map[string]string      `json:"acls"`
	Misc      map[string]string      `json:"misc"`
}

// HostMapping holds old/new hostname info for a single host.
type HostMapping struct {
	OldHostname string `json:"old_hostname"`
	NewHostname string `json:"new_hostname"`
	OldFQDN     string `json:"old_fqdn"`
	NewFQDN     string `json:"new_fqdn"`
	OldDomain   string `json:"old_domain"`
	NewDomain   string `json:"new_domain"`
}

// replacement is an ordered old->new string replacement.
type replacement struct {
	Old          string
	New          string
	WordBoundary bool           // use word-boundary regex instead of plain replacement
	re           *regexp.Regexp // precompiled word-boundary pattern (nil for plain replacements)
}

// Generator creates GOAD variants with randomized entity names.
type Generator struct {
	SourcePath  string
	TargetPath  string
	VariantName string

	nameGen         *NameGenerator
	mappings        Mappings
	replacements    []replacement
	userPasswordMap map[string]string // new_username -> new_password
	preservedUsers  map[string]bool
	pwdInDescUsers  map[string]bool // new_username -> has password in description
	nameComponents  map[string]bool // Misc keys that are firstname/surname components
}

// hostnameAliases maps canonical hostnames to known typos/aliases in upstream GOAD.
var hostnameAliases = map[string][]string{
	"braavos": {"Bravos"},
	"meereen": {"Meren"},
}

// NewGenerator creates a new variant generator.
func NewGenerator(source, target, name string) *Generator {
	return &Generator{
		SourcePath:  source,
		TargetPath:  target,
		VariantName: name,
		nameGen:     NewNameGenerator(),
		mappings: Mappings{
			Domains:   make(map[string]string),
			NetBIOS:   make(map[string]string),
			Hosts:     make(map[string]HostMapping),
			Users:     make(map[string]string),
			Passwords: make(map[string]string),
			Groups:    make(map[string]string),
			OUs:       make(map[string]string),
			ACLs:      make(map[string]string),
			Misc:      make(map[string]string),
		},
		userPasswordMap: make(map[string]string),
		preservedUsers:  map[string]bool{"sql_svc": true},
		pwdInDescUsers:  make(map[string]bool),
		nameComponents:  make(map[string]bool),
	}
}

// Run executes the full variant generation process.
func (g *Generator) Run() error {
	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("GOAD Variant Generator - %s\n", g.VariantName)
	fmt.Printf("%s\n", strings.Repeat("=", 60))
	fmt.Printf("Source: %s\n", g.SourcePath)
	fmt.Printf("Target: %s\n", g.TargetPath)
	fmt.Printf("%s\n\n", strings.Repeat("=", 60))

	config, err := g.loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	g.generateMappings(config)
	g.buildOrderedReplacements()
	if err := createFreshTarget(g.TargetPath); err != nil {
		return err
	}

	if err := g.copyAndTransform(); err != nil {
		return fmt.Errorf("transform: %w", err)
	}

	if err := g.saveMappings(); err != nil {
		return fmt.Errorf("save mappings: %w", err)
	}

	if err := g.validate(); err != nil {
		return fmt.Errorf("variant validation failed: %w", err)
	}
	if err := g.createDocumentation(); err != nil {
		return fmt.Errorf("create documentation: %w", err)
	}
	if err := writeCompletionMarker(g.TargetPath); err != nil {
		return fmt.Errorf("write completion marker: %w", err)
	}

	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Println("Variant generation complete and validated!")
	fmt.Printf("%s\n\n", strings.Repeat("=", 60))

	return nil
}

func createFreshTarget(target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create variant target parent: %w", err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("variant target already exists: %s; move or remove it before generating", target)
		}
		return fmt.Errorf("create variant target: %w", err)
	}
	return nil
}

// loadConfig reads the source GOAD config.json.
func (g *Generator) loadConfig() (*LabConfig, error) {
	data, err := os.ReadFile(filepath.Join(g.SourcePath, "data", "config.json"))
	if err != nil {
		return nil, err
	}
	var config LabConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// generateMappings extracts entities and creates all mappings.
func (g *Generator) generateMappings(config *LabConfig) {
	fmt.Println("\n=== Generating Mappings ===")

	fmt.Println("\nMapping domains...")
	g.mapDomains()

	fmt.Println("\nMapping hosts...")
	g.mapHosts(config)

	fmt.Println("\nMapping users...")
	g.mapUsers(config)

	fmt.Println("\nMapping groups...")
	g.mapGroups(config)

	fmt.Println("\nMapping OUs...")
	g.mapOUs(config)

	fmt.Println("\nMapping passwords...")
	g.mapPasswords(config)

	fmt.Println("\nMapping gMSA accounts...")
	g.mapGMSAAccounts(config)

	fmt.Println("\nMapping cities...")
	g.mapCities(config)

	fmt.Println("\nMapping shares...")
	g.mapShares(config)

	// Reconcile name-component Misc entries that conflict with Groups.
	// Group names are explicit AD entities and take precedence over
	// capitalized-surname convenience entries (e.g., "Targaryen" is both
	// a group and a surname). Only remove entries that are name components
	// (firstname/surname) — other Misc entries like hostnames, gMSA names,
	// and cities must be preserved.
	for groupName := range g.mappings.Groups {
		if g.nameComponents[groupName] {
			delete(g.mappings.Misc, groupName)
			delete(g.nameComponents, groupName)
		}
	}

	fmt.Println("\n=== Mapping Generation Complete ===")
}

func (g *Generator) mapDomains() {
	rootNew := g.nameGen.GenerateDomainName()
	rootFull := rootNew + ".local"

	childPrefix := g.nameGen.GenerateSubdomainName()
	childFull := childPrefix + "." + rootFull

	externalNew := g.nameGen.GenerateDomainName()
	externalFull := externalNew + ".local"

	g.mappings.Domains["sevenkingdoms.local"] = rootFull
	g.mappings.Domains["north.sevenkingdoms.local"] = childFull
	g.mappings.Domains["essos.local"] = externalFull

	rootNB := rootNew[:min(len(rootNew), maxNetBIOSLength)]
	childNB := childPrefix[:min(len(childPrefix), maxNetBIOSLength)]
	extNB := externalNew[:min(len(externalNew), maxNetBIOSLength)]

	g.mappings.NetBIOS["SEVENKINGDOMS"] = strings.ToUpper(rootNB)
	g.mappings.NetBIOS["NORTH"] = strings.ToUpper(childNB)
	g.mappings.NetBIOS["ESSOS"] = strings.ToUpper(extNB)

	g.mappings.NetBIOS["sevenkingdoms"] = strings.ToLower(rootNB)
	g.mappings.NetBIOS["north"] = strings.ToLower(childNB)
	g.mappings.NetBIOS["essos"] = strings.ToLower(extNB)

	g.mappings.NetBIOS["Sevenkingdoms"] = capitalize(rootNB)
	g.mappings.NetBIOS["North"] = capitalize(childNB)
	g.mappings.NetBIOS["Essos"] = capitalize(extNB)

	fmt.Printf("  sevenkingdoms.local -> %s\n", rootFull)
	fmt.Printf("  north.sevenkingdoms.local -> %s\n", childFull)
	fmt.Printf("  essos.local -> %s\n", externalFull)
}

func (g *Generator) mapHosts(config *LabConfig) {
	for hostID, host := range config.Lab.Hosts {
		oldHostname := host.Hostname
		oldDomain := host.Domain
		newHostname := g.nameGen.GenerateHostname()
		newDomain := g.mappings.Domains[oldDomain]

		g.mappings.Hosts[hostID] = HostMapping{
			OldHostname: oldHostname,
			NewHostname: newHostname,
			OldFQDN:     oldHostname + "." + oldDomain,
			NewFQDN:     newHostname + "." + newDomain,
			OldDomain:   oldDomain,
			NewDomain:   newDomain,
		}

		g.mappings.Misc[oldHostname+"$"] = newHostname + "$"
		g.mappings.Misc[strings.ToUpper(oldHostname)] = strings.ToUpper(newHostname)
		g.mappings.Misc[capitalize(oldHostname)] = capitalize(newHostname)

		for _, alias := range hostnameAliases[oldHostname] {
			g.mappings.Misc[alias] = capitalize(newHostname)
		}

		fmt.Printf("  %s: %s -> %s\n", hostID, oldHostname, newHostname)
	}
}

func (g *Generator) mapUsers(config *LabConfig) {
	for _, domain := range config.Lab.Domains {
		for username, user := range domain.Users {
			if g.preservedUsers[username] {
				g.mappings.Users[username] = username
				fmt.Printf("  %s -> %s (preserved)\n", username, username)
				continue
			}

			newUsername := g.nameGen.GenerateUsername()
			// Preserve single-name pattern: if original has no ".", use only firstname.
			// Pass through EnsureUnique to avoid collisions when multiple dotless
			// usernames truncate to the same firstname.
			if !strings.Contains(username, ".") {
				newUsername = g.nameGen.ensureUnique(strings.Split(newUsername, ".")[0])
			}
			g.mappings.Users[username] = newUsername

			if user != nil && user.Password != "" && user.Description != "" &&
				strings.Contains(strings.ToLower(user.Description), strings.ToLower(user.Password)) {
				g.pwdInDescUsers[newUsername] = true
			}

			g.mapUserNameComponents(user, newUsername)

			fmt.Printf("  %s -> %s\n", username, newUsername)
		}
	}
}

func (g *Generator) mapUserNameComponents(user *UserConfig, newUsername string) {
	if user == nil {
		return
	}
	if user.Firstname != "" && user.Firstname != "sql" {
		newFirst := strings.Split(newUsername, ".")[0]
		g.setNameComponent(user.Firstname, newFirst)
	}

	if user.Surname != "" && user.Surname != "-" {
		parts := strings.SplitN(newUsername, ".", 2)
		newSurname := parts[0]
		if len(parts) > 1 {
			newSurname = parts[1]
		}
		g.setNameComponent(user.Surname, newSurname)
	}
}

// setNameComponent adds a name mapping (and its case variant) to Misc,
// skipping entries that already exist to prevent collisions.
func (g *Generator) setNameComponent(original, replacement string) {
	if _, exists := g.mappings.Misc[original]; !exists {
		g.mappings.Misc[original] = replacement
		g.nameComponents[original] = true
	}
	// Add the opposite case variant (lowercase ↔ capitalized).
	if isAllLower(original) {
		cap := capitalize(original)
		if _, exists := g.mappings.Misc[cap]; !exists {
			g.mappings.Misc[cap] = capitalize(replacement)
			g.nameComponents[cap] = true
		}
	} else {
		lower := strings.ToLower(original)
		if _, exists := g.mappings.Misc[lower]; !exists {
			g.mappings.Misc[lower] = strings.ToLower(replacement)
			g.nameComponents[lower] = true
		}
	}
}

func (g *Generator) mapGroups(config *LabConfig) {
	builtins := map[string]bool{"Domain Admins": true, "Protected Users": true}

	for _, domain := range config.Lab.Domains {
		allGroups := []map[string]GroupConfig{
			domain.Groups.Universal,
			domain.Groups.Global,
			domain.Groups.DomainLocal,
		}

		for _, typeGroups := range allGroups {
			for groupName := range typeGroups {
				if builtins[groupName] {
					g.mappings.Groups[groupName] = groupName
					continue
				}
				newName := g.nameGen.GenerateGroupName()
				g.mappings.Groups[groupName] = newName
				fmt.Printf("  %s -> %s\n", groupName, newName)
			}
		}
	}
}

func (g *Generator) mapOUs(config *LabConfig) {
	for _, domain := range config.Lab.Domains {
		for ouName := range domain.OrganisationUnits {
			newName := g.nameGen.GenerateOUName()
			g.mappings.OUs[ouName] = newName
			fmt.Printf("  %s -> %s\n", ouName, newName)
		}
	}
}

func (g *Generator) mapPasswords(config *LabConfig) {
	passwords := make(map[string]bool)

	collectDomainPasswords(config.Lab.Domains, passwords)
	collectHostPasswords(config.Lab.Hosts, passwords)

	for pw := range passwords {
		newPW := g.nameGen.GeneratePassword(pw)
		g.mappings.Passwords[pw] = newPW
		truncOld := pw
		truncNew := newPW
		if len(truncOld) > 20 {
			truncOld = truncOld[:20]
		}
		if len(truncNew) > 20 {
			truncNew = truncNew[:20]
		}
		fmt.Printf("  %s... -> %s...\n", truncOld, truncNew)
	}

	g.buildUserPasswordMap(config.Lab.Domains)
}

func collectDomainPasswords(domains map[string]*DomainConfig, passwords map[string]bool) {
	for _, domain := range domains {
		if domain.DomainPassword != "" {
			passwords[domain.DomainPassword] = true
		}
		for _, user := range domain.Users {
			if user != nil && user.Password != "" {
				passwords[user.Password] = true
			}
		}
	}
}

func collectHostPasswords(hosts map[string]*HostConfig, passwords map[string]bool) {
	for _, host := range hosts {
		if host.LocalAdminPassword != "" {
			passwords[host.LocalAdminPassword] = true
		}
		collectMSSQLPasswords(host.MSSQL, passwords)
		collectVulnPasswords(host.VulnsVars, passwords)
	}
}

func collectMSSQLPasswords(mssql *MSSQLConfig, passwords map[string]bool) {
	if mssql == nil {
		return
	}
	if mssql.SAPassword != "" {
		passwords[mssql.SAPassword] = true
	}
	for _, ls := range mssql.LinkedServers {
		for _, mapping := range ls.UsersMapping {
			if mapping.RemotePassword != "" {
				passwords[mapping.RemotePassword] = true
			}
		}
	}
}

// collectVulnPasswords extracts passwords from the variable-schema vulns_vars map.
func collectVulnPasswords(vulnsVars map[string]any, passwords map[string]bool) {
	if vulnsVars == nil {
		return
	}
	if creds, ok := vulnsVars["credentials"].(map[string]any); ok {
		for _, credData := range creds {
			credInfo, ok := credData.(map[string]any)
			if !ok {
				continue
			}
			if pw, ok := credInfo["secret"].(string); ok {
				passwords[pw] = true
			}
			if pw, ok := credInfo["runas_password"].(string); ok {
				passwords[pw] = true
			}
		}
	}
	if autologon, ok := vulnsVars["autologon"].(map[string]any); ok {
		for _, autoData := range autologon {
			autoInfo, ok := autoData.(map[string]any)
			if !ok {
				continue
			}
			if pw, ok := autoInfo["password"].(string); ok {
				passwords[pw] = true
			}
		}
	}
}

func (g *Generator) buildUserPasswordMap(domains map[string]*DomainConfig) {
	for _, domain := range domains {
		for username, user := range domain.Users {
			if user == nil || user.Password == "" {
				continue
			}
			newUsername := g.mappings.Users[username]
			if newUsername == "" {
				newUsername = username
			}
			newPW := g.mappings.Passwords[user.Password]
			if newPW == "" {
				newPW = user.Password
			}
			g.userPasswordMap[newUsername] = newPW
		}
	}
}

func (g *Generator) mapGMSAAccounts(config *LabConfig) {
	for _, domain := range config.Lab.Domains {
		for _, gmsa := range domain.GMSA {
			if gmsa.Name != "" {
				newName := g.nameGen.GenerateGMSAName()
				g.mappings.Misc[gmsa.Name] = newName
				g.mappings.Misc[gmsa.Name+"$"] = newName + "$"
				fmt.Printf("  %s -> %s\n", gmsa.Name, newName)
			}
		}
	}
}

func (g *Generator) mapCities(config *LabConfig) {
	cities := make(map[string]bool)
	for _, domain := range config.Lab.Domains {
		for _, user := range domain.Users {
			if user != nil && user.City != "" && user.City != "-" {
				cities[user.City] = true
			}
		}
	}

	for city := range cities {
		newCity := g.nameGen.GenerateCityName()
		g.mappings.Misc[city] = newCity
		fmt.Printf("  %s -> %s\n", city, newCity)
	}
}

// mapShares extracts share names from VulnsVars and generates new names.
// Share names like "thewall" are GOAD-themed and must be randomized to
// prevent agents from recognizing the original lab structure.
func (g *Generator) mapShares(config *LabConfig) {
	seen := make(map[string]bool)
	for _, host := range config.Lab.Hosts {
		shares, ok := host.VulnsVars["shares"]
		if !ok {
			continue
		}
		sharesMap, ok := shares.(map[string]any)
		if !ok {
			continue
		}
		for shareName := range sharesMap {
			if seen[shareName] {
				continue
			}
			seen[shareName] = true
			newName := g.nameGen.GenerateShareName()
			g.mappings.Misc[shareName] = newName
			fmt.Printf("  share %s -> %s\n", shareName, newName)
		}
	}
}

// rebuildShareKeys renames share map keys in VulnsVars after text
// replacement has updated the share values but not the JSON map keys.
func (g *Generator) rebuildShareKeys(config *LabConfig) {
	for _, host := range config.Lab.Hosts {
		shares, ok := host.VulnsVars["shares"]
		if !ok {
			continue
		}
		sharesMap, ok := shares.(map[string]any)
		if !ok {
			continue
		}
		newShares := make(map[string]any)
		for oldKey, val := range sharesMap {
			newKey := oldKey
			if mapped, ok := g.mappings.Misc[oldKey]; ok {
				newKey = mapped
			}
			newShares[newKey] = val
		}
		host.VulnsVars["shares"] = newShares
	}
}

// buildOrderedReplacements builds the ordered replacement list (longest first).
func (g *Generator) buildOrderedReplacements() {
	fmt.Println("\n=== Building Ordered Replacements ===")

	var repls []replacement

	repls = g.appendHostReplacements(repls)
	repls = g.appendQualifiedUserReplacements(repls)
	repls = g.appendDNReplacements(repls)
	repls = appendMapReplacements(repls, g.mappings.Misc, withSuffix("$"))
	repls = g.appendDomainReplacements(repls)
	repls = appendMapReplacements(repls, g.mappings.Users, nil)
	repls = appendMapReplacements(repls, g.mappings.Groups, nil)
	repls = appendMapReplacements(repls, g.mappings.OUs, nil)
	repls = appendMapReplacements(repls, g.mappings.Passwords, nil)
	repls = appendMapReplacements(repls, g.mappings.NetBIOS, nil)
	repls = appendMapReplacements(repls, g.mappings.Misc, withoutSuffix("$"))

	// Tag name-component replacements for word-boundary matching.
	// Skip word-boundary for entries that also exist as group names —
	// group names need plain string replacement to catch compound strings
	// like "StarkWallpaper".
	for i := range repls {
		if _, isGroup := g.mappings.Groups[repls[i].Old]; isGroup {
			repls[i].WordBoundary = false
		} else {
			repls[i].WordBoundary = g.isNameComponent(repls[i].Old)
		}
	}

	sort.Slice(repls, func(i, j int) bool {
		return len(repls[i].Old) > len(repls[j].Old)
	})

	seen := make(map[string]bool)
	var unique []replacement
	for _, r := range repls {
		key := r.Old + "\x00" + r.New
		if !seen[key] {
			seen[key] = true
			unique = append(unique, r)
		}
	}

	// Precompile word-boundary regexes so applyReplacements doesn't
	// recompile on every file.
	for i := range unique {
		if unique[i].WordBoundary {
			pattern := `\b` + regexp.QuoteMeta(unique[i].Old) + `\b`
			unique[i].re, _ = regexp.Compile(pattern)
		}
	}

	g.replacements = unique
	fmt.Printf("Built %d ordered replacements\n", len(g.replacements))
}

func withSuffix(s string) func(string) bool {
	return func(key string) bool { return strings.HasSuffix(key, s) }
}

func withoutSuffix(s string) func(string) bool {
	return func(key string) bool { return !strings.HasSuffix(key, s) }
}

func appendMapReplacements(repls []replacement, m map[string]string, filter func(string) bool) []replacement {
	for old, new := range m {
		if filter != nil && !filter(old) {
			continue
		}
		repls = append(repls, replacement{Old: old, New: new})
	}
	return repls
}

func (g *Generator) appendHostReplacements(repls []replacement) []replacement {
	for _, hm := range g.mappings.Hosts {
		repls = append(repls, replacement{Old: hm.OldFQDN, New: hm.NewFQDN})
	}
	for _, hm := range g.mappings.Hosts {
		repls = append(repls, replacement{Old: hm.OldHostname, New: hm.NewHostname})
	}
	return repls
}

func (g *Generator) appendQualifiedUserReplacements(repls []replacement) []replacement {
	netbiosUpperMap := make(map[string]string)
	for old, new := range g.mappings.NetBIOS {
		if strings.ToUpper(old) == old {
			netbiosUpperMap[old] = new
		}
	}

	for oldDomain, newDomain := range g.mappings.Domains {
		oldNB := strings.ToUpper(strings.Split(oldDomain, ".")[0])
		newNB := netbiosUpperMap[oldNB]
		if newNB == "" {
			newNB = strings.ToUpper(strings.Split(newDomain, ".")[0])
		}
		for oldUser, newUser := range g.mappings.Users {
			repls = append(repls,
				replacement{Old: oldNB + "\\\\" + oldUser, New: newNB + "\\\\" + newUser},
				replacement{Old: oldDomain + "\\\\" + oldUser, New: newDomain + "\\\\" + newUser},
			)
		}
	}
	return repls
}

func (g *Generator) appendDNReplacements(repls []replacement) []replacement {
	for oldDomain, newDomain := range g.mappings.Domains {
		oldParts := strings.Split(strings.TrimSuffix(oldDomain, ".local"), ".")
		newParts := strings.Split(strings.TrimSuffix(newDomain, ".local"), ".")

		var oldDCs, newDCs []string
		for _, p := range oldParts {
			oldDCs = append(oldDCs, "DC="+p)
		}
		for _, p := range newParts {
			newDCs = append(newDCs, "DC="+p)
		}
		repls = append(repls, replacement{
			Old: strings.Join(oldDCs, ",") + ",DC=local",
			New: strings.Join(newDCs, ",") + ",DC=local",
		})
	}
	return repls
}

func (g *Generator) appendDomainReplacements(repls []replacement) []replacement {
	type domainPair struct{ old, new string }
	var pairs []domainPair
	for old, new := range g.mappings.Domains {
		pairs = append(pairs, domainPair{old, new})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return len(pairs[i].old) > len(pairs[j].old)
	})
	for _, dp := range pairs {
		repls = append(repls, replacement{Old: dp.old, New: dp.new})
	}
	return repls
}

// applyReplacements applies all replacements to content.
func (g *Generator) applyReplacements(content string) string {
	for _, r := range g.replacements {
		if r.Old == r.New {
			continue
		}

		if r.WordBoundary && r.re != nil {
			// Match name at boundaries, treating underscore as a word
			// separator. Go's \b considers _ a word character, but GOAD
			// uses underscore-delimited keys (e.g., "GenericWrite_missandei_viserys").
			// We pre-process underscores to temporary placeholders so \b works,
			// then restore them.
			const placeholder = "\x00USCORE\x00"
			temp := strings.ReplaceAll(content, "_", placeholder)
			temp = r.re.ReplaceAllString(temp, r.New)
			content = strings.ReplaceAll(temp, placeholder, "_")
		} else {
			content = strings.ReplaceAll(content, r.Old, r.New)
		}
	}
	return content
}

// isNameComponent returns true if old is a firstname/surname component needing word-boundary protection.
func (g *Generator) isNameComponent(old string) bool {
	return g.nameComponents[old]
}

// fixUserFirstnameSurname corrects firstname/surname fields to match generated usernames.
func (g *Generator) fixUserFirstnameSurname(config *LabConfig) {
	for _, domain := range config.Lab.Domains {
		for username, user := range domain.Users {
			if g.preservedUsers[username] || user == nil {
				continue
			}
			if strings.Contains(username, ".") {
				parts := strings.SplitN(username, ".", 2)
				user.Firstname = parts[0]
				if len(parts) > 1 {
					user.Surname = parts[1]
				}
				if user.Description != "" {
					displayName := capitalize(parts[0]) + " " + capitalize(parts[1])
					if g.pwdInDescUsers[username] {
						if pw, ok := g.userPasswordMap[username]; ok {
							user.Description = displayName + " (Password : " + pw + ")"
						} else {
							user.Description = displayName
						}
					} else {
						user.Description = displayName
					}
				}
			}
		}
	}
}

// fixPasswords corrects password fields corrupted by global text replacement.
func (g *Generator) fixPasswords(config *LabConfig) {
	for _, domain := range config.Lab.Domains {
		for username, user := range domain.Users {
			if user == nil {
				continue
			}
			if newPW, ok := g.userPasswordMap[username]; ok {
				user.Password = newPW
			}
		}
	}
}

// rebuildACLKeys rebuilds ACL dictionary keys using new entity names.
func (g *Generator) rebuildACLKeys(config *LabConfig) {
	for _, domain := range config.Lab.Domains {
		if domain.ACLs == nil {
			continue
		}

		newACLs := make(map[string]ACLConfig)
		for oldKey, acl := range domain.ACLs {
			forSimple := simplifyEntity(acl.For)
			toSimple := simplifyEntity(acl.To)

			keyParts := strings.SplitN(oldKey, "_", 3)
			var newKey string
			if len(keyParts) >= 3 {
				newKey = keyParts[0] + "_" + forSimple + "_" + toSimple
			} else {
				newKey = oldKey
			}

			newACLs[newKey] = acl
		}

		domain.ACLs = newACLs
	}
}

// simplifyEntity extracts a simplified name from an LDAP entity string.
func simplifyEntity(entity string) string {
	parts := strings.Split(entity, "\\")
	s := parts[len(parts)-1]
	s = strings.Split(s, ",")[0]
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "_")
	for _, prefix := range []string{"cn=", "ou=", "dc="} {
		s = strings.ReplaceAll(s, prefix, "")
	}
	return s
}

var textExtensions = map[string]bool{
	".json": true, ".yml": true, ".yaml": true, ".ps1": true, ".tf": true,
	".txt": true, ".md": true, ".sh": true, ".py": true, ".rb": true,
	".cfg": true, ".conf": true, ".ini": true,
}

var textFilenames = map[string]bool{
	"Vagrantfile": true, "inventory": true, "Makefile": true,
}

// transformFile transforms a single file with replacements and writes to target.
func (g *Generator) transformFile(srcPath, relPath string) (transformed bool, err error) {
	// Rename file paths based on entity mappings (e.g., arya.txt -> thomas.txt).
	newRelPath := g.applyReplacements(relPath)
	ext := filepath.Ext(srcPath)
	base := filepath.Base(srcPath)
	targetFile := filepath.Join(g.TargetPath, newRelPath)

	if err := os.MkdirAll(filepath.Dir(targetFile), 0o755); err != nil {
		return false, fmt.Errorf("create target directory for %s: %w", relPath, err)
	}

	isText := textExtensions[ext] || textFilenames[base] || (ext == "" && g.isTextFile(srcPath))
	if !isText {
		if err := copyFile(srcPath, targetFile); err != nil {
			return false, err
		}
		return false, nil
	}

	content, err := os.ReadFile(srcPath)
	if err != nil {
		if cpErr := copyFile(srcPath, targetFile); cpErr != nil {
			return false, fmt.Errorf("read %s: %v; fallback copy: %w", srcPath, err, cpErr)
		}
		return false, nil
	}

	newContent := g.applyReplacements(string(content))
	if ext == ".ps1" {
		newContent = g.fixSecureStrings(newContent)
	}
	newContent = g.transformConfigJSON(base, newContent)
	if strings.HasPrefix(base, "inventory") {
		newContent = g.repointDomainName(newContent)
	}

	if err := os.WriteFile(targetFile, []byte(newContent), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", targetFile, err)
	}
	return true, nil
}

// repointDomainName rewrites the Ansible `domain_name` inventory variable to
// the variant target folder basename. Playbooks resolve vuln scripts/files as
// ad/{{ domain_name }}/..., so for a variant this must point at the variant's
// own ad/<target>/ tree rather than the stock ad/GOAD/ tree. Existing values
// are replaced in place; missing values are inserted under [all:vars], with
// the section appended when the provider template does not define it.
func (g *Generator) repointDomainName(content string) string {
	target := filepath.Base(g.TargetPath)
	re := regexp.MustCompile(`(?m)^(\s*domain_name\s*=\s*).*$`)
	if re.MatchString(content) {
		return re.ReplaceAllStringFunc(content, func(line string) string {
			if idx := strings.Index(line, "="); idx >= 0 {
				valueStart := idx + 1
				for valueStart < len(line) && (line[valueStart] == ' ' || line[valueStart] == '\t') {
					valueStart++
				}
				return line[:valueStart] + target
			}
			return line
		})
	}

	header := regexp.MustCompile(`(?m)^\[all:vars\][ \t]*`)
	loc := header.FindStringIndex(content)
	lineEnding := "\n"
	if strings.Contains(content, "\r\n") {
		lineEnding = "\r\n"
	}
	if loc == nil {
		separator := ""
		if content != "" {
			separator = lineEnding + lineEnding
			if strings.HasSuffix(content, lineEnding) {
				separator = lineEnding
			}
			if strings.HasSuffix(content, lineEnding+lineEnding) {
				separator = ""
			}
		}
		return content + separator + "[all:vars]" + lineEnding + "domain_name=" + target + lineEnding
	}

	insertAt := loc[1]
	if strings.HasPrefix(content[insertAt:], "\r\n") {
		insertAt += 2
	} else if strings.HasPrefix(content[insertAt:], "\n") {
		insertAt++
	} else {
		return content[:insertAt] + lineEnding + "domain_name=" + target + content[insertAt:]
	}

	return content[:insertAt] + "domain_name=" + target + lineEnding + content[insertAt:]
}

// copyAndTransform copies the source directory, transforming text files.
// transformConfigJSON applies structural transformations to GOAD config JSON
// files (firstname/surname fixup, password remapping, ACL/share key rebuilds).
// Returns content unchanged if the file is not a full config JSON.
func (g *Generator) transformConfigJSON(base, content string) string {
	isFullConfig := (base == "config.json" || strings.HasSuffix(base, "-config.json")) &&
		!strings.HasSuffix(base, "-overlay.json")
	if !isFullConfig {
		return content
	}
	var configData LabConfig
	if err := json.Unmarshal([]byte(content), &configData); err != nil {
		return content
	}
	g.fixUserFirstnameSurname(&configData)
	g.fixPasswords(&configData)
	g.rebuildACLKeys(&configData)
	g.rebuildShareKeys(&configData)
	if pretty, err := json.MarshalIndent(configData, "", "  "); err == nil {
		return string(pretty)
	}
	return content
}

func (g *Generator) copyAndTransform() error {
	fmt.Println("\n=== Copying and Transforming Files ===")

	var total, transformed, copied int

	err := filepath.WalkDir(g.SourcePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip git metadata and inherited completion markers, but preserve
		// .gitkeep placeholders required to retain otherwise-empty directories.
		rel, err := filepath.Rel(g.SourcePath, path)
		if err != nil {
			return fmt.Errorf("resolve relative path for %s: %w", path, err)
		}
		if strings.Contains(rel, ".git") && d.Name() != ".gitkeep" {
			return nil
		}
		if d.Name() == CompletionMarkerName || d.Name() == CompletionMarkerName+".tmp" {
			return nil
		}

		total++
		didTransform, err := g.transformFile(path, rel)
		if err != nil {
			return fmt.Errorf("process %s: %w", rel, err)
		}
		if didTransform {
			transformed++
		} else {
			copied++
		}

		if total%10 == 0 {
			fmt.Printf("Processed %d files...\n", total)
		}
		return nil
	})

	fmt.Printf("\nTransformation complete:\n")
	fmt.Printf("  Total files: %d\n", total)
	fmt.Printf("  Transformed: %d\n", transformed)
	fmt.Printf("  Copied as-is: %d\n", copied)

	return err
}

// saveMappings writes the mapping file to target/mapping.json.
func (g *Generator) saveMappings() error {
	data, err := json.MarshalIndent(g.mappings, "", "  ")
	if err != nil {
		return err
	}
	outPath := filepath.Join(g.TargetPath, "mapping.json")
	fmt.Printf("Mappings saved to %s\n", outPath)
	return os.WriteFile(outPath, data, 0o644)
}

var originalNames = []string{
	"sevenkingdoms", "essos",
	"kingslanding", "winterfell", "meereen", "castelblack", "braavos",
	"stark", "lannister", "baratheon", "targaryen", "drogo", "snow",
	"tywin", "jaime", "cersei", "tyron", "robert", "joffrey",
	"arya", "eddard", "catelyn", "robb", "sansa", "brandon",
	"daenerys", "viserys", "khal", "jorah", "mormont",
}

type violation struct {
	file string
	name string
}

// validate checks that no original GOAD names appear in variant files.
func (g *Generator) validate() error {
	fmt.Println("\n=== Validating Variant ===")

	violations, filesChecked, err := g.findNameViolations()
	if err != nil {
		return fmt.Errorf("scan generated files: %w", err)
	}
	fmt.Printf("Checked %d text files\n", filesChecked)
	printViolations(violations)
	if len(violations) > 0 {
		return fmt.Errorf("found %d original-name violations", len(violations))
	}

	fmt.Println("\nValidating structure...")
	return g.validateStructureCounts()
}

func (g *Generator) findNameViolations() ([]violation, int, error) {
	var violations []violation
	filesChecked := 0
	skipFiles := map[string]bool{"mapping.json": true, "README.md": true}

	err := filepath.WalkDir(g.TargetPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if skipFiles[d.Name()] {
			return nil
		}
		ext := filepath.Ext(path)
		if !textExtensions[ext] && !textFilenames[d.Name()] && (ext != "" || !g.isTextFile(path)) {
			return nil
		}
		filesChecked++
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		lower := strings.ToLower(string(content))
		rel, err := filepath.Rel(g.TargetPath, path)
		if err != nil {
			return fmt.Errorf("resolve relative path for %s: %w", path, err)
		}
		for _, name := range originalNames {
			if strings.Contains(lower, name) {
				re, err := regexp.Compile(`\b` + regexp.QuoteMeta(name) + `\b`)
				if err == nil && re.MatchString(lower) {
					violations = append(violations, violation{rel, name})
				}
			}
		}
		return nil
	})
	return violations, filesChecked, err
}

func printViolations(violations []violation) {
	if len(violations) == 0 {
		fmt.Println("No original names found in variant files")
		return
	}
	fmt.Printf("\nFound %d potential issues:\n", len(violations))
	limit := len(violations)
	if limit > 20 {
		limit = 20
	}
	for _, v := range violations[:limit] {
		fmt.Printf("  %s: contains '%s'\n", v.file, v.name)
	}
	if len(violations) > 20 {
		fmt.Printf("  ... and %d more\n", len(violations)-20)
	}
}

func (g *Generator) validateStructureCounts() error {
	origConfig, err := g.loadConfig()
	if err != nil {
		return fmt.Errorf("load source structure: %w", err)
	}
	varData, err := os.ReadFile(filepath.Join(g.TargetPath, "data", "config.json"))
	if err != nil {
		return fmt.Errorf("read generated structure: %w", err)
	}
	var varConfig LabConfig
	if err := json.Unmarshal(varData, &varConfig); err != nil {
		return fmt.Errorf("parse generated structure: %w", err)
	}
	origHosts := len(origConfig.Lab.Hosts)
	varHosts := len(varConfig.Lab.Hosts)
	origDomains := len(origConfig.Lab.Domains)
	varDomains := len(varConfig.Lab.Domains)

	checkMark := func(a, b int) string {
		if a == b {
			return "OK"
		}
		return "MISMATCH"
	}
	fmt.Printf("  Hosts: %d -> %d %s\n", origHosts, varHosts, checkMark(origHosts, varHosts))
	fmt.Printf("  Domains: %d -> %d %s\n", origDomains, varDomains, checkMark(origDomains, varDomains))
	if origHosts != varHosts || origDomains != varDomains {
		return fmt.Errorf("structure count mismatch: hosts %d -> %d, domains %d -> %d", origHosts, varHosts, origDomains, varDomains)
	}
	return nil
}

// createDocumentation generates a README for the variant.
func (g *Generator) createDocumentation() error {
	readme := fmt.Sprintf(`# GOAD %s

This is a graph-isomorphic variant of the GOAD (Game of Active Directory) lab environment.

## About This Variant

- **All entity names have been randomized** while preserving the complete structure
- **Attack paths remain identical** to the original GOAD
- **All vulnerabilities preserved** with the same relationships
- **All 7 provider configs included**: VirtualBox, VMware, VMware ESXi, Proxmox, AWS, Azure, Ludus

## Structure

- **3 domains** with parent-child and trust relationships
- **5 VMs**: 3 Domain Controllers, 2 Servers
- **40+ users** with randomized names
- **18+ groups**, **8 OUs**, **20+ ACLs**

## Usage

Deploy exactly like the original GOAD:

    # Navigate to provider directory
    cd providers/virtualbox  # or vmware, proxmox, aws, azure, ludus

    # Follow provider-specific setup instructions
    # Provisioning works identically to GOAD

## Mapping Reference

See mapping.json for the complete entity mapping from GOAD to this variant.

## Notes

- Service account sql_svc preserved for MSSQL functionality
- gMSA account randomized to gmsa<Animal> format
- All passwords changed with equivalent complexity
- VM identifiers (dc01, dc02, srv02, etc.) unchanged for compatibility
- Directory structure identical to original GOAD

---

Generated by GOAD Variant Generator
`, strings.ToUpper(g.VariantName))

	readmePath := filepath.Join(g.TargetPath, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", readmePath, err)
	}
	fmt.Printf("Documentation created at %s\n", readmePath)
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", dst, err)
	}
	return nil
}

// isTextFile returns true if the file appears to contain text (valid UTF-8 with
// no NUL bytes). Used as a fallback for extensionless files like "inventory_disable_vagrant".
func (g *Generator) isTextFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	// Only read the first 8 KiB — enough to detect binary content.
	buf := make([]byte, 8192)
	n, _ := f.Read(buf)
	if n == 0 {
		return false
	}
	check := buf[:n]
	if bytes.ContainsRune(check, 0) {
		return false
	}
	return utf8.Valid(check)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}

func isAllLower(s string) bool {
	return s == strings.ToLower(s)
}
