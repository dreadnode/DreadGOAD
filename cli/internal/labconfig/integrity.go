package labconfig

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Finding is one referential-integrity problem in a lab config.
type Finding struct {
	// Path locates the offending value, e.g.
	// `domains["essos.local"].groups.universal.greatmaster`.
	Path string
	// Ref is the reference that failed to resolve, when the finding is about
	// a dangling reference rather than a missing pairing.
	Ref string
	// Msg explains the invariant that was violated.
	Msg string
}

func (f Finding) String() string {
	if f.Ref == "" {
		return fmt.Sprintf("%s: %s", f.Path, f.Msg)
	}
	return fmt.Sprintf("%s: %s (%q)", f.Path, f.Msg, f.Ref)
}

// Options configures the cross-file invariants CheckIntegrity enforces.
type Options struct {
	// VulnsRequiringVars names vulns whose Ansible role dereferences
	// vulns_vars, meaning a host that lists the vuln must also carry a
	// matching vulns_vars entry.
	//
	// This is worth linting precisely because Ansible will not complain:
	// vulnerabilities.yml passes `vulns_vars[vuln] | default({})`, so a
	// missing entry leaves the role iterating an empty dict and provisioning
	// nothing, with the vuln still listed as if it had been applied.
	//
	// Callers derive this from the role sources rather than hardcoding it
	// here, so the set cannot drift from the roles it describes. A nil map
	// disables the pairing check.
	VulnsRequiringVars map[string]bool

	// VulnsVarsGroupRefs maps a vuln name to the vulns_vars leaf keys that
	// hold a group name, e.g. adcs_esc13 -> ["adcs_esc13_group"]. These are
	// the references that a per-env overlay can silently orphan by deleting
	// the group while leaving the vuln in place.
	VulnsVarsGroupRefs map[string][]string

	// VulnsVarsPrincipalRefs is the same idea for leaf keys holding a
	// DOMAIN\user reference, e.g. adcs_esc7 -> ["ca_manager"].
	VulnsVarsPrincipalRefs map[string][]string

	// KnownVulnRoles is the set of vuln names that have a corresponding
	// ansible/roles/vulns_<name> role. vulnerabilities.yml interpolates the
	// name straight into include_role, so a typo here is a provision-time
	// "role not found" rather than anything the data itself reveals. Upstream
	// GOAD-Light shipped `enable_nbt-ns` against a `vulns_enable_nbt_ns` role
	// for exactly this reason. A nil map disables the check.
	KnownVulnRoles map[string]bool
}

// DefaultOptions returns the reference map for the vulns currently shipped in
// ad/*/data. VulnsRequiringVars is deliberately left nil; see Options.
func DefaultOptions() Options {
	return Options{
		VulnsVarsGroupRefs: map[string][]string{
			"adcs_esc13": {"adcs_esc13_group"},
		},
		VulnsVarsPrincipalRefs: map[string][]string{
			"adcs_esc7": {"ca_manager"},
		},
	}
}

// CheckIntegrity validates that every entity a merged lab config references
// still exists in that config.
//
// It exists because the per-env `{env}-overlay.json` files are RFC 7386 merge
// patches, where a null deletes a key and an array replaces its base wholesale.
// A one-line overlay can therefore remove a group, user, or vulns_vars entry
// that something else in the config still points at, and nothing else in the
// pipeline notices: Ansible only fails later, on the host, when a role
// dereferences the missing object.
//
// Pass the already-merged document, not the base config. Validating the base
// alone is what lets overlay-introduced breakage through.
func CheckIntegrity(merged []byte, opts Options) ([]Finding, error) {
	var doc struct {
		Lab struct {
			Domains map[string]domain `json:"domains"`
			Hosts   map[string]host   `json:"hosts"`
		} `json:"lab"`
	}
	if err := json.Unmarshal(merged, &doc); err != nil {
		return nil, fmt.Errorf("parse merged lab config: %w", err)
	}

	idx := newIndex(doc.Lab.Domains, doc.Lab.Hosts)
	var out []Finding

	for name, d := range doc.Lab.Domains {
		out = append(out, idx.checkDomain(name, d)...)
	}
	for id, h := range doc.Lab.Hosts {
		out = append(out, idx.checkHost(id, h, opts)...)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Ref < out[j].Ref
	})
	return out, nil
}

// overlayDropKeys names the per-host arrays whose loss silently removes lab
// capability. Each is provisioned by looping over the array, so an entry the
// overlay drops is never applied to the host.
var overlayDropKeys = []struct {
	name string
	get  func(host) []string
}{
	{"vulns", func(h host) []string { return h.Vulns }},
	{"scripts", func(h host) []string { return h.Scripts }},
	{"vulns_adcs_templates", func(h host) []string { return h.VulnsADCSTemplates }},
}

// CheckOverlayDrops reports capability the base config grants a host that the
// merged, post-overlay config does not.
//
// CheckIntegrity cannot see this class, by construction. Under RFC 7386 an
// array in an overlay replaces its base counterpart wholesale rather than
// merging into it, so an overlay that redeclares `vulns` without one of the
// base's entries yields a document that is internally consistent and silently
// smaller. Nothing dangles, so a referential check reports nothing, and
// vulnerabilities.yml simply never includes the absent role: the play recap
// reads ok=N failed=0 and the lab is quietly wrong in that environment alone.
//
// This shipped. Adding adcs_esc10_case1 to dc03 in config.json left the dev,
// staging and test overlays redeclaring dc03.vulns without it, so ESC6 and
// ESC9 stayed unexploitable in all three, while prod, which carries no dc03
// override and therefore inherited the base, was correct.
//
// Pass the same base and merged pair the provisioner resolves. A host the
// overlay deletes outright with an explicit null is not reported, since that
// removal is already legible in the overlay.
func CheckOverlayDrops(base, merged []byte) ([]Finding, error) {
	baseHosts, err := hostsOf(base)
	if err != nil {
		return nil, fmt.Errorf("parse base lab config: %w", err)
	}
	mergedHosts, err := hostsOf(merged)
	if err != nil {
		return nil, fmt.Errorf("parse merged lab config: %w", err)
	}

	var out []Finding
	for id, bh := range baseHosts {
		mh, ok := mergedHosts[id]
		if !ok {
			continue
		}
		for _, key := range overlayDropKeys {
			kept := map[string]bool{}
			for _, v := range key.get(mh) {
				kept[v] = true
			}
			for _, v := range key.get(bh) {
				if !kept[v] {
					out = append(out, Finding{
						Path: fmt.Sprintf("hosts[%q].%s", id, key.name),
						Ref:  v,
						Msg:  "overlay drops an entry the base config declares, so it is never provisioned in this environment",
					})
				}
			}
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Ref < out[j].Ref
	})
	return out, nil
}

// hostsOf decodes just the host table from a lab config document.
func hostsOf(doc []byte) (map[string]host, error) {
	var parsed struct {
		Lab struct {
			Hosts map[string]host `json:"hosts"`
		} `json:"lab"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return nil, err
	}
	return parsed.Lab.Hosts, nil
}

type domain struct {
	DC          string `json:"dc"`
	NetbiosName string `json:"netbios_name"`
	// Trust is a single partner FQDN, empty when the domain trusts nobody.
	// Every lab in ad/ uses this scalar form rather than a list.
	Trust                   string                      `json:"trust"`
	Users                   map[string]labUser          `json:"users"`
	Groups                  map[string]map[string]group `json:"groups"`
	ACLs                    map[string]acl              `json:"acls"`
	OUs                     map[string]any              `json:"organisation_units"`
	MultiDomainGroupsMember map[string][]string         `json:"multi_domain_groups_member"`
	LAPSReaders             []string                    `json:"laps_readers"`
	GMSA                    map[string]gmsa             `json:"gmsa"`
}

type labUser struct {
	Groups []string `json:"groups"`
}

type group struct {
	ManagedBy string   `json:"managed_by"`
	Members   []string `json:"members"`
}

type acl struct {
	For string `json:"for"`
	To  string `json:"to"`
}

type gmsa struct {
	Name      string   `json:"gMSA_Name"`
	HostNames []string `json:"gMSA_HostNames"`
}

type host struct {
	Hostname           string              `json:"hostname"`
	Domain             string              `json:"domain"`
	LocalGroups        map[string][]string `json:"local_groups"`
	Vulns              []string            `json:"vulns"`
	Scripts            []string            `json:"scripts"`
	VulnsADCSTemplates []string            `json:"vulns_adcs_templates"`
	VulnsVars          map[string]any      `json:"vulns_vars"`
	MSSQL              *struct {
		Sysadmins      []string          `json:"sysadmins"`
		ExecuteAsLogin map[string]string `json:"executeaslogin"`
		ExecuteAsUser  map[string]struct {
			User string `json:"user"`
		} `json:"executeasuser"`
	} `json:"mssql"`
}

// index answers "does this principal exist?" across the whole lab.
type index struct {
	// byDomain maps a domain FQDN to its principals.
	byDomain map[string]*domainPrincipals
	// domainKey resolves a netbios name or FQDN (both lowercased) to the
	// domain's FQDN, since references use either form interchangeably.
	domainKey map[string]string
	// computers and gmsaAccounts are lab-wide: a "name$" reference is not
	// scoped to a domain in the config's own notation.
	computers    map[string]bool
	gmsaAccounts map[string]bool
	hostnames    map[string]bool
	hostIDs      map[string]bool
}

type domainPrincipals struct {
	users  map[string]bool
	groups map[string]bool
}

func newIndex(domains map[string]domain, hosts map[string]host) *index {
	idx := &index{
		byDomain:     map[string]*domainPrincipals{},
		domainKey:    map[string]string{},
		computers:    map[string]bool{},
		gmsaAccounts: map[string]bool{},
		hostnames:    map[string]bool{},
		hostIDs:      map[string]bool{},
	}

	for fqdn, d := range domains {
		p := &domainPrincipals{users: map[string]bool{}, groups: map[string]bool{}}
		for u := range d.Users {
			p.users[strings.ToLower(u)] = true
		}
		for _, scope := range d.Groups {
			for g := range scope {
				p.groups[strings.ToLower(g)] = true
			}
		}
		idx.byDomain[fqdn] = p

		idx.domainKey[strings.ToLower(fqdn)] = fqdn
		if d.NetbiosName != "" {
			idx.domainKey[strings.ToLower(d.NetbiosName)] = fqdn
		}
		// A domain's leftmost label is also used as a prefix in local_groups
		// (e.g. "north\\eddard.stark" for north.sevenkingdoms.local).
		if label := strings.SplitN(strings.ToLower(fqdn), ".", 2)[0]; label != "" {
			if _, taken := idx.domainKey[label]; !taken {
				idx.domainKey[label] = fqdn
			}
		}

		for _, g := range d.GMSA {
			if g.Name != "" {
				idx.gmsaAccounts[strings.ToLower(g.Name)] = true
			}
		}
	}

	for id, h := range hosts {
		idx.hostIDs[strings.ToLower(id)] = true
		if h.Hostname != "" {
			idx.hostnames[strings.ToLower(h.Hostname)] = true
			idx.computers[strings.ToLower(h.Hostname)] = true
		}
	}
	return idx
}

// builtinUsers and builtinGroups are principals AD creates itself, so the lab
// config references them without declaring them.
var builtinUsers = map[string]bool{
	"administrator": true, "guest": true, "krbtgt": true, "defaultaccount": true,
	"ansible": true, "ssm-user": true, "vagrant": true,
}

var builtinGroups = map[string]bool{
	"domain admins": true, "domain users": true, "domain guests": true,
	"domain computers": true, "domain controllers": true,
	"enterprise admins": true, "schema admins": true,
	"group policy creator owners": true, "protected users": true,
	"cert publishers": true, "read-only domain controllers": true,
	"enterprise read-only domain controllers": true,
	"dnsadmins": true, "dnsupdateproxy": true, "ras and ias servers": true,
	"allowed rodc password replication group": true,
	"denied rodc password replication group":  true,
	"cloneable domain controllers":            true, "key admins": true,
	"enterprise key admins": true, "account operators": true,
	"administrators": true, "backup operators": true, "server operators": true,
	"print operators": true, "remote desktop users": true, "users": true,
	"guests": true, "iis_iusrs": true, "performance log users": true,
	"performance monitor users": true, "distributed com users": true,
	"event log readers": true, "certificate service dcom access": true,
	"cryptographic operators": true, "network configuration operators": true,
	"incoming forest trust builders":     true,
	"pre-windows 2000 compatible access": true,
	"windows authorization access group": true,
	"terminal server license servers":    true, "remote management users": true,
	"access control assistance operators": true,
	"system managed accounts group":       true, "storage replica administrators": true,
	"hyper-v administrators": true,
}

// wellKnownPrefixes mark references to SIDs AD resolves on its own.
var wellKnownPrefixes = []string{
	"nt authority\\", "builtin\\", "nt service\\", "everyone",
	"authenticated users", "creator owner",
}

// resolve reports whether ref names something that exists. domainCtx is the
// FQDN a bare (unprefixed) reference is interpreted against.
//
// It accepts the four notations the configs actually use: a bare name, a
// DOMAIN\name pair where DOMAIN is a netbios name or an FQDN, a "name$"
// machine or gMSA account, and a distinguished name.
func (idx *index) resolve(ref, domainCtx string) bool {
	r := strings.TrimSpace(ref)
	if r == "" {
		return true // nothing referenced; emptiness is checked by the callers that care
	}
	lower := strings.ToLower(r)

	for _, p := range wellKnownPrefixes {
		if strings.HasPrefix(lower, p) || lower == strings.TrimSuffix(p, "\\") {
			return true
		}
	}

	// A distinguished name is structural rather than a principal. Validate the
	// domain it claims to live in and leave the leaf alone: CN/OU leaves are
	// created by roles this config does not enumerate.
	if strings.Contains(r, "=") {
		return idx.resolveDN(lower)
	}

	if i := strings.Index(r, "\\"); i >= 0 {
		dom, leaf := lower[:i], r[i+1:]
		fqdn, ok := idx.domainKey[dom]
		if !ok {
			return false
		}
		return idx.resolveLeaf(leaf, fqdn)
	}
	return idx.resolveLeaf(r, domainCtx)
}

func (idx *index) resolveDN(lowerDN string) bool {
	var labels []string
	for _, part := range strings.Split(lowerDN, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "dc=") {
			labels = append(labels, strings.TrimPrefix(part, "dc="))
		}
	}
	if len(labels) == 0 {
		return true // a relative DN carries no domain claim to check
	}
	_, ok := idx.domainKey[strings.Join(labels, ".")]
	return ok
}

func (idx *index) resolveLeaf(leaf, domainCtx string) bool {
	l := strings.ToLower(strings.TrimSpace(leaf))
	if l == "" {
		return true
	}
	if base, isAccount := strings.CutSuffix(l, "$"); isAccount {
		// Machine and gMSA accounts. Trust accounts are the partner domain's
		// netbios name, which the domainKey index already covers.
		if idx.computers[base] || idx.gmsaAccounts[base] {
			return true
		}
		_, isTrust := idx.domainKey[base]
		return isTrust
	}
	if builtinUsers[l] || builtinGroups[l] {
		return true
	}
	if p, ok := idx.byDomain[domainCtx]; ok && (p.users[l] || p.groups[l]) {
		return true
	}
	// Cross-domain references are common in this lab (forest trusts), so a
	// bare name that resolves in any domain is accepted rather than reported.
	for _, p := range idx.byDomain {
		if p.users[l] || p.groups[l] {
			return true
		}
	}
	return false
}

func (idx *index) resolveGroup(ref, domainCtx string) bool {
	l := strings.ToLower(strings.TrimSpace(ref))
	if l == "" {
		return true
	}
	if builtinGroups[l] {
		return true
	}
	if p, ok := idx.byDomain[domainCtx]; ok && p.groups[l] {
		return true
	}
	for _, p := range idx.byDomain {
		if p.groups[l] {
			return true
		}
	}
	return false
}

// refChecker accumulates findings for a single domain or host. It carries the
// path prefix and the domain context that bare references resolve against, so
// the per-section helpers below stay small enough to read at a glance.
type refChecker struct {
	idx    *index
	prefix string // e.g. `domains["essos.local"].`
	ctx    string // domain FQDN that unprefixed references resolve against
	out    []Finding
}

func (c *refChecker) at(format string, a ...any) string {
	return c.prefix + fmt.Sprintf(format, a...)
}

func (c *refChecker) add(path, ref, msg string) {
	c.out = append(c.out, Finding{Path: path, Ref: ref, Msg: msg})
}

// ref reports ref unless it resolves to any principal.
func (c *refChecker) ref(path, ref, what string) {
	if ref != "" && !c.idx.resolve(ref, c.ctx) {
		c.add(path, ref, "unresolved "+what)
	}
}

// group reports ref unless it resolves to a group specifically.
func (c *refChecker) group(path, ref string) {
	if ref != "" && !c.idx.resolveGroup(ref, c.ctx) {
		c.add(path, ref, "unresolved group")
	}
}

func (idx *index) checkDomain(fqdn string, d domain) []Finding {
	c := &refChecker{idx: idx, prefix: fmt.Sprintf("domains[%q].", fqdn), ctx: fqdn}
	c.domainTopology(d)
	c.domainGroups(d)
	c.domainUsers(d)
	c.domainACLs(d)
	c.domainCrossRefs(d)
	return c.out
}

func (c *refChecker) domainTopology(d domain) {
	if d.DC != "" && !c.idx.hostIDs[strings.ToLower(d.DC)] {
		c.add(c.at("dc"), d.DC, "domain controller is not a declared host")
	}
	if d.Trust == "" {
		return
	}
	if _, ok := c.idx.domainKey[strings.ToLower(d.Trust)]; !ok {
		c.add(c.at("trust"), d.Trust, "trust partner is not a declared domain")
	}
}

func (c *refChecker) domainGroups(d domain) {
	for scope, groups := range d.Groups {
		for name, g := range groups {
			c.ref(c.at("groups.%s.%s.managed_by", scope, name), g.ManagedBy, "managed_by principal")
			for _, m := range g.Members {
				c.ref(c.at("groups.%s.%s.members", scope, name), m, "group member")
			}
		}
	}
}

func (c *refChecker) domainUsers(d domain) {
	for name, u := range d.Users {
		for _, g := range u.Groups {
			c.group(c.at("users.%s.groups", name), g)
		}
	}
}

func (c *refChecker) domainACLs(d domain) {
	for key, a := range d.ACLs {
		c.ref(c.at("acls.%s.for", key), a.For, "ACL principal")
		c.ref(c.at("acls.%s.to", key), a.To, "ACL target")
	}
}

func (c *refChecker) domainCrossRefs(d domain) {
	for g, members := range d.MultiDomainGroupsMember {
		c.group(c.at("multi_domain_groups_member"), g)
		for _, m := range members {
			c.ref(c.at("multi_domain_groups_member.%s", g), m, "cross-domain member")
		}
	}
	for _, r := range d.LAPSReaders {
		c.ref(c.at("laps_readers"), r, "LAPS reader")
	}
	for key, g := range d.GMSA {
		for _, hn := range g.HostNames {
			if !c.idx.hostnames[strings.ToLower(hn)] {
				c.add(c.at("gmsa.%s.gMSA_HostNames", key), hn, "gMSA host is not a declared host")
			}
		}
	}
}

func (idx *index) checkHost(id string, h host, opts Options) []Finding {
	c := &refChecker{idx: idx, prefix: fmt.Sprintf("hosts[%q].", id), ctx: h.Domain}
	c.hostDomain(h)
	c.hostLocalGroups(h)
	c.hostMSSQL(h)
	c.hostVulns(h, opts)
	return c.out
}

func (c *refChecker) hostDomain(h host) {
	if h.Domain == "" {
		return
	}
	if _, ok := c.idx.domainKey[strings.ToLower(h.Domain)]; !ok {
		c.add(c.at("domain"), h.Domain, "host joins an undeclared domain")
	}
}

func (c *refChecker) hostLocalGroups(h host) {
	for lg, members := range h.LocalGroups {
		for _, m := range members {
			c.ref(c.at("local_groups.%s", lg), m, "local group member")
		}
	}
}

func (c *refChecker) hostMSSQL(h host) {
	if h.MSSQL == nil {
		return
	}
	for _, s := range h.MSSQL.Sysadmins {
		c.ref(c.at("mssql.sysadmins"), s, "MSSQL sysadmin")
	}
	for login := range h.MSSQL.ExecuteAsLogin {
		// Values are SQL logins (e.g. "sa"), which are not AD principals;
		// only the granted-to key is an AD identity.
		c.ref(c.at("mssql.executeaslogin"), login, "MSSQL login")
	}
	for key, e := range h.MSSQL.ExecuteAsUser {
		c.ref(c.at("mssql.executeasuser.%s.user", key), e.User, "MSSQL user")
	}
}

// hostVulns covers the pairing that broke ESC13: a vuln stays in the list while
// an overlay deletes the vulns_vars entry, or the group that entry points at.
func (c *refChecker) hostVulns(h host, opts Options) {
	for _, v := range h.Vulns {
		c.vulnHasRole(v, opts)
		c.vulnHasVars(h, v, opts)
		c.vulnVarsRefs(h, v, opts)
	}
}

func (c *refChecker) vulnHasRole(v string, opts Options) {
	if opts.KnownVulnRoles != nil && !opts.KnownVulnRoles[v] {
		c.add(c.at("vulns"), v, "no ansible/roles/vulns_<name> role exists for this vuln")
	}
}

func (c *refChecker) vulnHasVars(h host, v string, opts Options) {
	if !opts.VulnsRequiringVars[v] {
		return
	}
	if _, ok := h.VulnsVars[v]; !ok {
		c.add(c.at("vulns"), v, "vuln has no vulns_vars entry, so its role provisions nothing")
	}
}

func (c *refChecker) vulnVarsRefs(h host, v string, opts Options) {
	for _, leaf := range opts.VulnsVarsGroupRefs[v] {
		for _, ref := range vulnsVarsLeaves(h.VulnsVars, v, leaf) {
			c.group(c.at("vulns_vars.%s.*.%s", v, leaf), ref)
		}
	}
	for _, leaf := range opts.VulnsVarsPrincipalRefs[v] {
		for _, ref := range vulnsVarsLeaves(h.VulnsVars, v, leaf) {
			c.ref(c.at("vulns_vars.%s.*.%s", v, leaf), ref, "principal")
		}
	}
}

// vulnsVarsLeaves pulls every value of leaf out of vulns_vars[vuln], which is
// shaped as a map of arbitrary case names to option objects.
func vulnsVarsLeaves(vars map[string]any, vuln, leaf string) []string {
	entry, ok := vars[vuln].(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	for _, caseRaw := range entry {
		c, ok := caseRaw.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := c[leaf].(string); ok && v != "" {
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}
