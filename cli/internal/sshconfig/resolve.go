// Package sshconfig resolves SSH connection targets — ssh_config aliases or
// raw hostnames — into the concrete fields needed for non-ssh consumers
// (e.g. a TCP reachability probe). It does this by shelling out to
// `ssh -G <target>`, which is the same path the ssh client itself uses, so
// the result reflects whatever Host stanzas, ProxyJump, etc. the user has.
package sshconfig

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Resolved is the subset of `ssh -G` output the rest of the CLI cares about.
type Resolved struct {
	Hostname     string
	Port         int
	User         string
	IdentityFile string
}

// Resolve runs `ssh -G <target>` and returns the resolved connection
// parameters. `target` may be an ssh_config Host alias or a raw hostname;
// either way ssh applies its config rules and emits the effective values.
//
// A 5-second timeout caps the call so a misconfigured ProxyCommand or DNS
// lookup can't hang the CLI.
func Resolve(target string) (Resolved, error) {
	return resolveWithConfig(target, "")
}

// resolveWithConfig is the testable form: when configFile is non-empty it is
// passed via `-F`, which is needed for tests because ssh -G doesn't honor
// $HOME on macOS — it reads /etc/ssh/ssh_config_d/* and the invoking user's
// real home directory.
func resolveWithConfig(target, configFile string) (Resolved, error) {
	if target == "" {
		return Resolved{}, fmt.Errorf("ssh target is empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []string{}
	if configFile != "" {
		args = append(args, "-F", configFile)
	}
	args = append(args, "-G", target)

	out, err := exec.CommandContext(ctx, "ssh", args...).Output()
	if err != nil {
		return Resolved{}, fmt.Errorf("ssh -G %s: %w", target, err)
	}

	r := Resolved{Hostname: target, Port: 22, User: "root"}
	for _, line := range strings.Split(string(out), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "hostname":
			r.Hostname = value
		case "port":
			if p, err := strconv.Atoi(value); err == nil {
				r.Port = p
			}
		case "user":
			r.User = value
		case "identityfile":
			// ssh -G emits multiple identityfile lines; first explicit one wins.
			if r.IdentityFile == "" {
				r.IdentityFile = value
			}
		}
	}
	return r, nil
}
