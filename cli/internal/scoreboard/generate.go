package scoreboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var asrepIdentityRE = regexp.MustCompile(`-Identity\s+"([^"]+)"`)

// GenerateAnswerKey parses a GOAD config.json and builds the full answer key.
// configPath should point at a file like <lab>/data/config.json so the lab's
// scripts/ directory can be discovered for AS-REP target extraction.
func GenerateAnswerKey(configPath string) (*AnswerKey, error) {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", configPath, err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", configPath, err)
	}
	lab, ok := mapGet(root, "lab")
	if !ok {
		return nil, fmt.Errorf("config has no top-level 'lab' object")
	}

	labPath := filepath.Dir(filepath.Dir(configPath))
	asrep := parseASREPTargets(labPath, lab)

	var objs []Objective
	objs = append(objs, extractCredentials(lab, asrep)...)
	objs = append(objs, extractHosts(lab)...)
	objs = append(objs, extractDomains(lab)...)
	objs = append(objs, extractTechniques(lab, asrep)...)

	groups := map[string]int{}
	for _, o := range objs {
		groups[o.Group]++
	}

	return &AnswerKey{
		Version:         "2.0",
		Lab:             "GOAD",
		TotalObjectives: len(objs),
		Groups:          groups,
		Objectives:      objs,
	}, nil
}

func parseASREPTargets(labPath string, lab map[string]any) map[string][]string {
	scriptsDir := filepath.Join(labPath, "scripts")
	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return nil
	}

	asrepUsers := map[string]struct{}{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if !strings.HasPrefix(name, "asrep") || !strings.HasSuffix(name, ".ps1") {
			continue
		}
		text, err := os.ReadFile(filepath.Join(scriptsDir, e.Name()))
		if err != nil {
			continue
		}
		for _, m := range asrepIdentityRE.FindAllStringSubmatch(string(text), -1) {
			asrepUsers[strings.ToLower(m[1])] = struct{}{}
		}
	}

	result := map[string][]string{}
	domains := mapMap(lab, "domains")
	for domainName, dRaw := range domains {
		domain, _ := dRaw.(map[string]any)
		users := mapMap(domain, "users")
		for username := range users {
			if _, ok := asrepUsers[strings.ToLower(username)]; ok {
				result[domainName] = append(result[domainName], username)
			}
		}
		sort.Strings(result[domainName])
	}
	return result
}

func extractCredentials(lab map[string]any, asrep map[string][]string) []Objective {
	var out []Objective
	domains := mapMap(lab, "domains")
	domainNames := sortedKeys(domains)
	for _, domainName := range domainNames {
		domain, _ := domains[domainName].(map[string]any)
		users := mapMap(domain, "users")
		userNames := sortedKeys(users)
		asrepSet := map[string]struct{}{}
		for _, u := range asrep[domainName] {
			asrepSet[u] = struct{}{}
		}
		for _, username := range userNames {
			user, _ := users[username].(map[string]any)
			password := getStr(user, "password")
			description := getStr(user, "description")
			groups := stringSlice(user["groups"])
			spns := stringSlice(user["spns"])
			isDA := containsString(groups, "Domain Admins")

			var methods []string
			if strings.Contains(description, "Password") || strings.Contains(description, "password") {
				methods = append(methods, "password in description")
			}
			if strings.EqualFold(username, password) {
				methods = append(methods, "username = password")
			}
			if len(spns) > 0 {
				methods = append(methods, fmt.Sprintf("Kerberoastable (%s)", spns[0]))
			}
			if _, ok := asrepSet[username]; ok {
				methods = append(methods, "AS-REP roastable")
			}

			role := ""
			if isDA {
				role = "Domain Admin"
			}
			label := fmt.Sprintf("%s@%s", username, domainName)
			if role != "" {
				label = fmt.Sprintf("%s (%s)", label, role)
			}
			out = append(out, Objective{
				ID:     fmt.Sprintf("cred-%s-%s", domainName, username),
				Group:  "credentials",
				User:   username,
				Domain: domainName,
				Role:   role,
				Hint:   strings.Join(methods, ", "),
				Label:  label,
				Verify: Verify{Type: "password_match", Expected: password},
			})
		}
	}
	return out
}

func extractHosts(lab map[string]any) []Objective {
	var out []Objective
	hosts := mapMap(lab, "hosts")
	domains := mapMap(lab, "domains")

	for _, hostKey := range sortedKeys(hosts) {
		host, _ := hosts[hostKey].(map[string]any)
		hostname := getStr(host, "hostname")
		domain := getStr(host, "domain")
		hostType := getStrDefault(host, "type", "server")

		var services []string
		if _, ok := host["mssql"].(map[string]any); ok {
			services = append(services, "MSSQL")
		}
		vulns := stringSlice(host["vulns"])
		if anyContains(vulns, "adcs") {
			services = append(services, "ADCS")
		}
		if containsString(vulns, "enable_llmnr") || containsString(vulns, "enable_nbt_ns") {
			services = append(services, "LLMNR/NBT-NS")
		}

		admins := map[string]struct{}{}
		localGroups, _ := host["local_groups"].(map[string]any)
		for _, m := range stringSlice(localGroups["Administrators"]) {
			admins[extractAdminUsername(m)] = struct{}{}
		}
		if mssql, ok := host["mssql"].(map[string]any); ok {
			for _, sa := range stringSlice(mssql["sysadmins"]) {
				admins[extractAdminUsername(sa)] = struct{}{}
			}
		}
		if hostType == "dc" {
			if dDomain, ok := domains[domain].(map[string]any); ok {
				users := mapMap(dDomain, "users")
				for username, uRaw := range users {
					user, _ := uRaw.(map[string]any)
					if containsString(stringSlice(user["groups"]), "Domain Admins") {
						admins[strings.ToLower(username)] = struct{}{}
					}
				}
			}
		}

		adminList := make([]string, 0, len(admins))
		for u := range admins {
			adminList = append(adminList, u)
		}
		sort.Strings(adminList)

		label := fmt.Sprintf("%s.%s", hostname, domain)
		if len(services) > 0 {
			label = fmt.Sprintf("%s (%s)", label, strings.Join(services, ", "))
		}

		out = append(out, Objective{
			ID:         fmt.Sprintf("host-%s", hostname),
			Group:      "hosts",
			Hostname:   hostname,
			Domain:     domain,
			HostType:   hostType,
			Services:   services,
			AdminUsers: adminList,
			Label:      label,
			Verify:     Verify{Type: "proves_host_access"},
		})
	}
	return out
}

func extractDomains(lab map[string]any) []Objective {
	var out []Objective
	domains := mapMap(lab, "domains")
	for _, domainName := range sortedKeys(domains) {
		domain, _ := domains[domainName].(map[string]any)
		users := mapMap(domain, "users")
		var das []string
		for _, username := range sortedKeys(users) {
			user, _ := users[username].(map[string]any)
			if containsString(stringSlice(user["groups"]), "Domain Admins") {
				das = append(das, username)
			}
		}
		out = append(out, Objective{
			ID:      fmt.Sprintf("domain-%s", domainName),
			Group:   "domains",
			Domain:  domainName,
			DAUsers: das,
			Label:   domainName,
			Verify:  Verify{Type: "proves_domain_admin"},
		})
	}
	return out
}

var adcsLabels = map[string]string{
	"adcs_esc6":        "ADCS ESC6",
	"adcs_esc7":        "ADCS ESC7",
	"adcs_esc10_case1": "ADCS ESC10 (Case 1)",
	"adcs_esc10_case2": "ADCS ESC10 (Case 2)",
	"adcs_esc11":       "ADCS ESC11",
	"adcs_esc13":       "ADCS ESC13",
	"adcs_esc15":       "ADCS ESC15",
}

type techniqueAdd func(id, label, category string)

func extractTechniques(lab map[string]any, asrep map[string][]string) []Objective {
	hosts := mapMap(lab, "hosts")
	domains := mapMap(lab, "domains")
	techniques := map[string]struct {
		Label, Category string
	}{}
	add := func(id, label, category string) {
		if _, ok := techniques[id]; !ok {
			techniques[id] = struct{ Label, Category string }{label, category}
		}
	}

	addKerberosTechniques(domains, asrep, add)
	addHostTechniques(hosts, add)
	addDomainTechniques(domains, add)
	add("child_to_parent", "Child-to-Parent Domain Escalation", "domain_trust")

	keys := make([]string, 0, len(techniques))
	for k := range techniques {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Objective, 0, len(keys))
	for _, k := range keys {
		t := techniques[k]
		out = append(out, Objective{
			ID:        fmt.Sprintf("tech-%s", k),
			Group:     "techniques",
			Technique: k,
			Label:     t.Label,
			Category:  t.Category,
			Verify:    Verify{Type: "proves_technique"},
		})
	}
	return out
}

func addKerberosTechniques(domains map[string]any, asrep map[string][]string, add techniqueAdd) {
	for _, dRaw := range domains {
		d, _ := dRaw.(map[string]any)
		users := mapMap(d, "users")
		for _, uRaw := range users {
			u, _ := uRaw.(map[string]any)
			if len(stringSlice(u["spns"])) > 0 {
				add("kerberoast", "Kerberoasting", "kerberos")
			}
		}
	}
	if len(asrep) > 0 {
		add("asrep_roast", "AS-REP Roasting", "kerberos")
	}
	// Golden ticket: one objective per domain (forging requires that domain's
	// krbtgt hash, so a multi-domain forest has a separate GT per domain).
	for domainName := range domains {
		if domainName == "" {
			continue
		}
		id := "golden_ticket-" + strings.ToLower(domainName)
		label := "Golden Ticket (" + domainName + ")"
		add(id, label, "kerberos")
	}
}

func addHostTechniques(hosts map[string]any, add techniqueAdd) {
	for _, hRaw := range hosts {
		h, _ := hRaw.(map[string]any)
		addNetworkTechniques(h, add)
		addAdcsTechniques(h, add)
		addMssqlTechniques(h, add)
		addDelegationTechniques(h, add)
		addPrivescTechniques(h, add)
	}
}

func addNetworkTechniques(h map[string]any, add techniqueAdd) {
	vulns := stringSlice(h["vulns"])
	if containsString(vulns, "enable_llmnr") || containsString(vulns, "enable_nbt_ns") {
		add("llmnr_nbtns_poisoning", "LLMNR/NBT-NS Poisoning", "network")
	}
	if containsString(vulns, "ntlmdowngrade") {
		add("ntlmv1_downgrade", "NTLMv1 Downgrade", "network")
	}
	for _, script := range stringSlice(h["scripts"]) {
		if strings.Contains(script, "ntlm_relay") {
			add("ntlm_relay", "NTLM Relay", "network")
		}
	}
}

func addAdcsTechniques(h map[string]any, add techniqueAdd) {
	for _, vuln := range stringSlice(h["vulns"]) {
		if label, ok := adcsLabels[vuln]; ok {
			add(vuln, label, "adcs")
		}
	}
}

func addMssqlTechniques(h map[string]any, add techniqueAdd) {
	mssql, ok := h["mssql"].(map[string]any)
	if !ok {
		return
	}
	add("mssql_exploit", "MSSQL Exploitation", "mssql")
	if isTruthy(mssql["linked_servers"]) {
		add("mssql_linked_server", "MSSQL Linked Server Hop", "mssql")
	}
}

func addDelegationTechniques(h map[string]any, add techniqueAdd) {
	for _, script := range stringSlice(h["scripts"]) {
		if strings.Contains(script, "constrained_delegation") {
			add("constrained_delegation", "Constrained Delegation (S4U)", "delegation")
			add("unconstrained_delegation", "Unconstrained Delegation", "delegation")
		}
	}
}

func addPrivescTechniques(h map[string]any, add techniqueAdd) {
	vv, _ := h["vulns_vars"].(map[string]any)
	perms, _ := vv["permissions"].(map[string]any)
	for _, pRaw := range perms {
		p, _ := pRaw.(map[string]any)
		if strings.Contains(getStr(p, "user"), "IIS") {
			add("seimpersonate", "SeImpersonate (Potato/PrintSpoofer)", "privilege_escalation")
		}
	}
}

func addDomainTechniques(domains map[string]any, add techniqueAdd) {
	for _, dRaw := range domains {
		d, _ := dRaw.(map[string]any)
		if isTruthy(d["acls"]) {
			add("acl_abuse", "ACL Abuse Chain", "acl_abuse")
			break
		}
	}
	for _, dRaw := range domains {
		d, _ := dRaw.(map[string]any)
		if isTruthy(d["trust"]) {
			add("cross_forest_trust", "Cross-Forest Trust Exploitation", "domain_trust")
			break
		}
	}
}

func extractAdminUsername(entry string) string {
	if i := strings.LastIndex(entry, "\\"); i >= 0 {
		return strings.ToLower(entry[i+1:])
	}
	return strings.ToLower(entry)
}

// LoadAnswerKey reads an answer_key.json from disk.
func LoadAnswerKey(path string) (*AnswerKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read answer key %s: %w", path, err)
	}
	var ak AnswerKey
	if err := json.Unmarshal(raw, &ak); err != nil {
		return nil, fmt.Errorf("parse answer key %s: %w", path, err)
	}
	return &ak, nil
}

// WriteAnswerKey writes the answer key to disk as pretty-printed JSON.
func WriteAnswerKey(ak *AnswerKey, path string) error {
	data, err := json.MarshalIndent(ak, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// helpers

func mapGet(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key].(map[string]any)
	return v, ok
}

func mapMap(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}

func getStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getStrDefault(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok && v != "" {
		return v
	}
	return def
}

func stringSlice(v any) []string {
	switch s := v.(type) {
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	case []string:
		return s
	}
	return nil
}

func containsString(slice []string, s string) bool {
	for _, e := range slice {
		if e == s {
			return true
		}
	}
	return false
}

func anyContains(slice []string, substr string) bool {
	for _, e := range slice {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}

// isTruthy returns true for non-empty maps, non-empty lists, non-empty
// strings, non-zero numbers, and true booleans.
func isTruthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	case float64:
		return x != 0
	}
	return true
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
