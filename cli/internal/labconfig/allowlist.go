// Package labconfig loads expected AD objects (users, computers, groups,
// trust accounts) from the resolved lab config JSON. Used by lab purge-rogues
// to allowlist legitimate baseline state and flag everything else.
package labconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Allowlist holds the names of expected AD objects, keyed for fast lookup.
// Casing matches AD's own conventions: SAM names lowercased, computer names
// uppercased.
type Allowlist struct {
	Users     []string // lowercased SamAccountNames
	Computers []string // uppercased computer names
	Groups    []string // lowercased group names
	Trusts    []string // uppercased netbios names with trailing $
}

type rawConfig struct {
	Lab struct {
		Domains map[string]rawDomain      `json:"domains"`
		Hosts   map[string]map[string]any `json:"hosts"`
	} `json:"lab"`
}

type rawDomain struct {
	NetbiosName string                    `json:"netbios_name"`
	Users       map[string]any            `json:"users"`
	Groups      map[string]map[string]any `json:"groups"`
}

// Load reads the lab config JSON at path and returns the union of all
// expected users, computers, and groups across every domain. The trust
// list contains every domain's netbios name with a "$" suffix — any of
// these may appear as an InterDomain Trust user account on a partner DC.
func Load(path string) (*Allowlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read lab config %s: %w", path, err)
	}
	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse lab config %s: %w", path, err)
	}

	users := map[string]struct{}{}
	computers := map[string]struct{}{}
	groups := map[string]struct{}{}
	trusts := map[string]struct{}{}

	for _, d := range raw.Lab.Domains {
		for u := range d.Users {
			users[strings.ToLower(u)] = struct{}{}
		}
		for _, scope := range d.Groups {
			for g := range scope {
				groups[strings.ToLower(g)] = struct{}{}
			}
		}
		if d.NetbiosName != "" {
			trusts[strings.ToUpper(d.NetbiosName)+"$"] = struct{}{}
		}
	}
	for _, h := range raw.Lab.Hosts {
		if name, ok := h["hostname"].(string); ok && name != "" {
			computers[strings.ToUpper(name)] = struct{}{}
		}
	}

	a := &Allowlist{
		Users:     keys(users),
		Computers: keys(computers),
		Groups:    keys(groups),
		Trusts:    keys(trusts),
	}
	return a, nil
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
