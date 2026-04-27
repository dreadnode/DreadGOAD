package ludus

import (
	"context"
	"fmt"
)

// SOCKSTunnel exposes a SOCKS5 proxy whose dials route through a pure-Go
// SSH connection to the Ludus host. Replaces the previous `ssh -D port -N`
// subprocess wrapper. Public surface (Port, ProxyURL, Close) is unchanged
// so provision.go callers don't need to be updated.
type SOCKSTunnel struct {
	Port int

	cli   *nativeClient
	socks *nativeSOCKS
}

// StartSOCKSTunnel opens an SSH connection to the Ludus host (honoring the
// user's ssh_config via `ssh -G`, IdentityAgent, ProxyJump, etc.) and runs
// a SOCKS5 listener on a free local port. Close() shuts down both.
func StartSOCKSTunnel(sshCfg SSHConfig) (*SOCKSTunnel, error) {
	cli, err := dialNative(context.Background(), sshCfg)
	if err != nil {
		return nil, fmt.Errorf("dial ssh for SOCKS tunnel: %w", err)
	}
	socks, err := cli.StartSOCKS5()
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("start SOCKS5 listener: %w", err)
	}
	return &SOCKSTunnel{Port: socks.Port(), cli: cli, socks: socks}, nil
}

// ProxyURL returns the SOCKS5 proxy URL for use with ansible_psrp_proxy.
func (t *SOCKSTunnel) ProxyURL() string {
	return fmt.Sprintf("socks5h://localhost:%d", t.Port)
}

// Close terminates the SOCKS5 listener and the underlying SSH connection.
func (t *SOCKSTunnel) Close() {
	if t.socks != nil {
		t.socks.Close()
	}
	if t.cli != nil {
		_ = t.cli.Close()
	}
}
