package ludus

import (
	"context"
	"fmt"
	"net"

	socks5 "github.com/armon/go-socks5"
)

// SOCKSTunnel exposes a SOCKS5 proxy whose dials route through a pure-Go
// SSH connection to the Ludus host. Replaces the previous `ssh -D port -N`
// subprocess wrapper. Public surface (Port, ProxyURL, Close) is unchanged
// so provision.go callers don't need to be updated.
type SOCKSTunnel struct {
	Port int

	cli      *sshClient
	listener net.Listener
	doneCh   chan struct{}
}

// StartSOCKSTunnel opens an SSH connection to the Ludus host (honoring the
// user's ssh_config via `ssh -G`, IdentityAgent, ProxyJump, etc.) and runs
// a SOCKS5 listener on a free local port. Close() shuts down both.
func StartSOCKSTunnel(sshCfg SSHConfig) (*SOCKSTunnel, error) {
	cli, err := dialSSH(context.Background(), sshCfg)
	if err != nil {
		return nil, fmt.Errorf("dial ssh for SOCKS tunnel: %w", err)
	}
	t, err := startSOCKS5(func(_ context.Context, network, addr string) (net.Conn, error) {
		return cli.Dial(network, addr)
	})
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("start SOCKS5 listener: %w", err)
	}
	t.cli = cli
	return t, nil
}

// startSOCKS5 stands up a local SOCKS5 listener whose dials are routed
// through the given dial func. Extracted so tests can exercise the listener
// without needing a real *ssh.Client.
func startSOCKS5(dial func(context.Context, string, string) (net.Conn, error)) (*SOCKSTunnel, error) {
	srv, err := socks5.New(&socks5.Config{Dial: dial})
	if err != nil {
		return nil, fmt.Errorf("init socks5 server: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("bind socks5 listener: %w", err)
	}

	done := make(chan struct{})
	go func() {
		_ = srv.Serve(ln)
		close(done)
	}()

	return &SOCKSTunnel{
		Port:     ln.Addr().(*net.TCPAddr).Port,
		listener: ln,
		doneCh:   done,
	}, nil
}

// ProxyURL returns the SOCKS5 proxy URL for use with ansible_psrp_proxy.
func (t *SOCKSTunnel) ProxyURL() string {
	return fmt.Sprintf("socks5h://localhost:%d", t.Port)
}

// Close terminates the SOCKS5 listener and the underlying SSH connection.
func (t *SOCKSTunnel) Close() {
	if t.listener != nil {
		_ = t.listener.Close()
		<-t.doneCh
	}
	if t.cli != nil {
		_ = t.cli.Close()
	}
}
