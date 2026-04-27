//go:build integration

package ludus

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/net/proxy"
)

// TestResolveSSH_RealAlias exercises the `ssh -G` integration path against
// an alias that exists in the developer's ssh_config. Set DG_SSH_ALIAS to
// override (default: proxmox).
func TestResolveSSH_RealAlias(t *testing.T) {
	alias := os.Getenv("DG_SSH_ALIAS")
	if alias == "" {
		alias = "proxmox"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rc, err := resolveSSH(ctx, SSHConfig{Host: alias})
	if err != nil {
		t.Fatalf("resolveSSH(%q): %v", alias, err)
	}

	t.Logf("Hostname: %s", rc.Hostname)
	t.Logf("User: %s", rc.User)
	t.Logf("Port: %d", rc.Port)
	t.Logf("IdentityAgent: %s", rc.IdentityAgent)
	t.Logf("IdentityFiles: %v", rc.IdentityFiles)
	t.Logf("ProxyJump: %s", rc.ProxyJump)
	t.Logf("StrictHostKey: %s", rc.StrictHostKey)
	t.Logf("UserKnownHostsFiles: %v", rc.UserKnownHostsFiles)

	if rc.Hostname == "" {
		t.Errorf("Hostname empty; ssh -G should always resolve a hostname")
	}
	if rc.Port == 0 {
		t.Errorf("Port = 0; default of 22 should have been parsed")
	}
}

// TestIdentityAgent_Reachable verifies that whatever IdentityAgent the
// resolved config points to (typically the 1Password agent socket on macOS)
// is reachable and exposes at least one key. This is the highest-risk
// component of the prototype because it exercises the unix-socket agent
// protocol directly without going through OpenSSH.
func TestIdentityAgent_Reachable(t *testing.T) {
	alias := os.Getenv("DG_SSH_ALIAS")
	if alias == "" {
		alias = "proxmox"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rc, err := resolveSSH(ctx, SSHConfig{Host: alias})
	if err != nil {
		t.Fatalf("resolveSSH: %v", err)
	}

	sock := rc.IdentityAgent
	if sock == "" {
		sock = os.Getenv("SSH_AUTH_SOCK")
	}
	sock = expandPath(sock)
	if sock == "" || strings.EqualFold(sock, "none") {
		t.Skip("no IdentityAgent or SSH_AUTH_SOCK configured")
	}
	t.Logf("Connecting to agent socket: %s", sock)

	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial agent socket %q: %v", sock, err)
	}
	defer conn.Close()

	keys, err := agent.NewClient(conn).List()
	if err != nil {
		t.Fatalf("agent.List: %v", err)
	}
	t.Logf("Agent exposes %d key(s):", len(keys))
	for _, k := range keys {
		t.Logf("  %s %s", k.Type(), k.Comment)
	}
	if len(keys) == 0 {
		t.Errorf("agent reachable but exposes no keys; auth would fail")
	}
}

// TestSOCKS5Server_Standalone spins up the SOCKS5 listener with a stub Dial
// and confirms a client can connect through it. We don't require a real SSH
// session for this — only the listener wiring.
func TestSOCKS5Server_Standalone(t *testing.T) {
	// Stand up a tiny TCP echo server we'll proxy to.
	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("echo listen: %v", err)
	}
	defer echoLn.Close()
	go func() {
		c, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 64)
		n, _ := c.Read(buf)
		_, _ = c.Write(buf[:n])
	}()

	// Use the dial-agnostic SOCKS5 core: route SOCKS5 dials to our echo server.
	target := echoLn.Addr().String()
	socks, srvErr := startSOCKS5(func(_ context.Context, network, _ string) (net.Conn, error) {
		return net.Dial(network, target)
	})
	if srvErr != nil {
		t.Fatalf("startSOCKS5: %v", srvErr)
	}
	defer socks.Close()
	t.Logf("SOCKS5 server listening on port %d", socks.Port())

	// Run a real SOCKS5 handshake through it: connect → ask the proxy to
	// reach an arbitrary host (our stub Dial ignores the target and routes
	// to the echo server) → write/read to verify bytes flow end-to-end.
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", itoa(socks.Port())), nil, proxy.Direct)
	if err != nil {
		t.Fatalf("build socks5 dialer: %v", err)
	}
	// The destination address is parsed by the SOCKS5 server but our stub
	// Dial ignores it and routes to the echo server. Use 127.0.0.1:1 so
	// the server's name resolution step succeeds without DNS.
	conn, err := dialer.Dial("tcp", "127.0.0.1:1")
	if err != nil {
		t.Fatalf("dial through socks5: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	want := []byte("ping")
	if _, err := conn.Write(want); err != nil {
		t.Fatalf("write through proxy: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatalf("read through proxy: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("echo mismatch: got %q want %q", got, want)
	}
}

// TestNativeDial_LiveHandshake performs an actual SSH handshake against the
// alias in DG_SSH_ALIAS using the prototype, then runs `whoami` over the
// session. Skipped unless DG_SSH_LIVE=1 because it triggers the 1Password
// biometric prompt.
func TestNativeDial_LiveHandshake(t *testing.T) {
	if os.Getenv("DG_SSH_LIVE") != "1" {
		t.Skip("set DG_SSH_LIVE=1 to run a real handshake (will trigger 1Password prompt)")
	}
	alias := os.Getenv("DG_SSH_ALIAS")
	if alias == "" {
		alias = "proxmox"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cli, err := dialNative(ctx, SSHConfig{Host: alias})
	if err != nil {
		t.Fatalf("dialNative(%q): %v", alias, err)
	}
	defer cli.Close()

	stdout, stderr, err := cli.Run(ctx, "whoami")
	if err != nil {
		t.Fatalf("Run whoami: %v (stderr=%q)", err, stderr)
	}
	t.Logf("Remote whoami: %q (stderr: %q)", strings.TrimSpace(stdout), stderr)
	if strings.TrimSpace(stdout) == "" {
		t.Errorf("whoami returned empty stdout")
	}
}

// TestNativeSOCKS5_ThroughLiveSSH dials the alias for real, exposes a SOCKS5
// proxy backed by that SSH client, then connects to an external host through
// the proxy. This is the smoke test for the provision flow's tunnel: traffic
// originates locally, exits the network at the SSH server. Skipped unless
// DG_SSH_LIVE=1.
func TestNativeSOCKS5_ThroughLiveSSH(t *testing.T) {
	if os.Getenv("DG_SSH_LIVE") != "1" {
		t.Skip("set DG_SSH_LIVE=1 to run a real handshake")
	}
	alias := os.Getenv("DG_SSH_ALIAS")
	if alias == "" {
		alias = "proxmox"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cli, err := dialNative(ctx, SSHConfig{Host: alias})
	if err != nil {
		t.Fatalf("dialNative: %v", err)
	}
	defer cli.Close()

	socks, err := cli.StartSOCKS5()
	if err != nil {
		t.Fatalf("StartSOCKS5: %v", err)
	}
	defer socks.Close()
	t.Logf("SOCKS5 over live SSH: %s", socks.ProxyURL())

	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", itoa(socks.Port())), nil, proxy.Direct)
	if err != nil {
		t.Fatalf("build socks5 dialer: %v", err)
	}
	conn, err := dialer.Dial("tcp", "1.1.1.1:80")
	if err != nil {
		t.Fatalf("dial 1.1.1.1:80 through proxy: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send a minimal HTTP request and verify we get back something.
	if _, err := conn.Write([]byte("GET / HTTP/1.0\r\nHost: 1.1.1.1\r\n\r\n")); err != nil {
		t.Fatalf("write http: %v", err)
	}
	buf := make([]byte, 32)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read http: %v", err)
	}
	t.Logf("Response prefix: %q", buf[:n])
	if !strings.HasPrefix(string(buf[:n]), "HTTP/") {
		t.Errorf("expected HTTP response, got %q", buf[:n])
	}
}

func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	return string(buf[i:])
}
