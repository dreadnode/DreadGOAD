package ludus

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// resolvedSSHConfig is the subset of `ssh -G <host>` output we act on. We
// shell out to OpenSSH once to expand ssh_config (Match exec, Include globs,
// ProxyJump chaining, IdentityAgent — all things a Go ssh_config parser would
// approximate) and then drive the actual connection with golang.org/x/crypto/ssh.
type resolvedSSHConfig struct {
	Hostname            string
	User                string
	Port                int
	IdentityAgent       string
	IdentityFiles       []string
	ProxyJump           string
	StrictHostKey       string
	UserKnownHostsFiles []string
}

// resolveSSH runs `ssh -G <host>` and parses the output. The host argument
// may be an ssh_config alias or a raw hostname; OpenSSH treats unrecognized
// names as direct hostnames and returns sensible defaults either way.
//
// SSHConfig overrides (User, Port, KeyPath) are forwarded as `-l`, `-p`,
// and `-i` flags so they participate in resolution the same way they would
// during a real `ssh` invocation. Password auth is handled separately in
// dial since `ssh -G` knows nothing about it.
func resolveSSH(ctx context.Context, cfg SSHConfig) (*resolvedSSHConfig, error) {
	args := []string{"-G"}
	if cfg.User != "" {
		args = append(args, "-l", cfg.User)
	}
	if cfg.Port != 0 && cfg.Port != 22 {
		args = append(args, "-p", strconv.Itoa(cfg.Port))
	}
	if cfg.KeyPath != "" {
		args = append(args, "-i", cfg.KeyPath)
	}
	args = append(args, cfg.Host)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ssh -G %s: %w (%s)", cfg.Host, err, strings.TrimSpace(stderr.String()))
	}
	return parseSSHGOutput(&stdout)
}

func parseSSHGOutput(r *bytes.Buffer) (*resolvedSSHConfig, error) {
	rc := &resolvedSSHConfig{Port: 22}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), " ")
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "hostname":
			rc.Hostname = value
		case "user":
			rc.User = value
		case "port":
			if n, err := strconv.Atoi(value); err == nil {
				rc.Port = n
			}
		case "identityagent":
			rc.IdentityAgent = strings.Trim(value, `"`)
		case "identityfile":
			rc.IdentityFiles = append(rc.IdentityFiles, value)
		case "proxyjump":
			rc.ProxyJump = value
		case "stricthostkeychecking":
			rc.StrictHostKey = value
		case "userknownhostsfile":
			rc.UserKnownHostsFiles = append(rc.UserKnownHostsFiles, strings.Fields(value)...)
		}
	}
	return rc, scanner.Err()
}

// expandPath resolves leading `~` and any $VAR references against the
// current environment. ssh -G typically pre-resolves these, but IdentityAgent
// values like `~/Library/...` (1Password) sometimes come through raw.
func expandPath(p string) string {
	p = os.ExpandEnv(p)
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	return p
}

// sshClient is a long-lived *ssh.Client. Reuse across calls to avoid
// re-handshaking — equivalent to ControlMaster but without the socket file.
type sshClient struct {
	cli  *ssh.Client
	host string
}

// dialSSH resolves the SSHConfig through `ssh -G`, then establishes a
// real SSH connection in pure Go. Password auth (when set on cfg) replaces
// the legacy sshpass dependency.
func dialSSH(ctx context.Context, cfg SSHConfig) (*sshClient, error) {
	cli, err := dialResolved(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &sshClient{cli: cli, host: cfg.Host}, nil
}

func dialResolved(ctx context.Context, cfg SSHConfig) (*ssh.Client, error) {
	rc, err := resolveSSH(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if rc.Hostname == "" {
		rc.Hostname = cfg.Host
	}
	if rc.User == "" {
		if u, err := user.Current(); err == nil {
			rc.User = u.Username
		}
	}

	hostKey, err := buildHostKeyCallback(rc)
	if err != nil {
		return nil, fmt.Errorf("host key callback: %w", err)
	}
	authMethods, err := buildAuthMethods(rc, cfg.Password)
	if err != nil {
		return nil, fmt.Errorf("auth methods: %w", err)
	}

	clientCfg := &ssh.ClientConfig{
		User:            rc.User,
		Auth:            authMethods,
		HostKeyCallback: hostKey,
		Timeout:         15 * time.Second,
	}
	target := net.JoinHostPort(rc.Hostname, strconv.Itoa(rc.Port))

	if rc.ProxyJump != "" && !strings.EqualFold(rc.ProxyJump, "none") {
		// Single-hop ProxyJump for now. Multi-hop chains
		// (`ProxyJump host1,host2`) would recurse on Split(",") tail.
		jump := strings.Split(rc.ProxyJump, ",")[0]
		jumpClient, err := dialResolved(ctx, SSHConfig{Host: jump})
		if err != nil {
			return nil, fmt.Errorf("proxyjump %q: %w", jump, err)
		}
		conn, err := jumpClient.Dial("tcp", target)
		if err != nil {
			_ = jumpClient.Close()
			return nil, fmt.Errorf("dial through jump: %w", err)
		}
		c, ch, reqs, err := ssh.NewClientConn(conn, target, clientCfg)
		if err != nil {
			_ = jumpClient.Close()
			return nil, fmt.Errorf("ssh handshake through jump: %w", err)
		}
		return ssh.NewClient(c, ch, reqs), nil
	}

	d := net.Dialer{Timeout: clientCfg.Timeout}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", target, err)
	}
	c, ch, reqs, err := ssh.NewClientConn(conn, target, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	return ssh.NewClient(c, ch, reqs), nil
}

func buildHostKeyCallback(rc *resolvedSSHConfig) (ssh.HostKeyCallback, error) {
	if strings.EqualFold(rc.StrictHostKey, "no") {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	var files []string
	for _, f := range rc.UserKnownHostsFiles {
		expanded := expandPath(f)
		if _, err := os.Stat(expanded); err == nil {
			files = append(files, expanded)
		}
	}
	if len(files) == 0 {
		if home, err := os.UserHomeDir(); err == nil {
			f := filepath.Join(home, ".ssh", "known_hosts")
			if _, err := os.Stat(f); err == nil {
				files = []string{f}
			}
		}
	}
	if len(files) == 0 {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	return knownhosts.New(files...)
}

func buildAuthMethods(rc *resolvedSSHConfig, password string) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	agentSock := rc.IdentityAgent
	if agentSock == "" {
		agentSock = os.Getenv("SSH_AUTH_SOCK")
	}
	if agentSock != "" && !strings.EqualFold(agentSock, "none") {
		agentSock = expandPath(agentSock)
		if conn, err := net.Dial("unix", agentSock); err == nil {
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}

	var signers []ssh.Signer
	for _, f := range rc.IdentityFiles {
		data, err := os.ReadFile(expandPath(f))
		if err != nil {
			continue
		}
		s, err := ssh.ParsePrivateKey(data)
		if err != nil {
			// Encrypted keys would need an interactive passphrase prompt;
			// out of scope for the prototype. Agent-loaded copies will
			// already be available via the agent path above.
			continue
		}
		signers = append(signers, s)
	}
	if len(signers) > 0 {
		methods = append(methods, ssh.PublicKeys(signers...))
	}

	if password != "" {
		methods = append(methods, ssh.Password(password))
	}

	if len(methods) == 0 {
		return nil, errors.New("no auth methods available (no agent socket, no readable identity files, no password)")
	}
	return methods, nil
}

// Run executes a command on the remote host and returns stdout, stderr, and
// any execution error. Mirrors the (string, string, error) shape returned by
// the existing exec.Command wrapper so callers swap cleanly.
func (c *sshClient) Run(ctx context.Context, cmdLine string) (sout, serr string, runErr error) {
	sess, err := c.cli.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("new session: %w", err)
	}
	defer func() {
		// Surface a session-close error only if the command itself
		// succeeded; otherwise the run error is more useful.
		if cerr := sess.Close(); cerr != nil && runErr == nil && cerr != io.EOF {
			runErr = fmt.Errorf("close ssh session: %w", cerr)
		}
	}()

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- sess.Run(cmdLine) }()

	select {
	case err := <-done:
		return stdout.String(), stderr.String(), err
	case <-ctx.Done():
		if sigErr := sess.Signal(ssh.SIGKILL); sigErr != nil {
			return stdout.String(), stderr.String(), fmt.Errorf("%w (signal: %v)", ctx.Err(), sigErr)
		}
		return stdout.String(), stderr.String(), ctx.Err()
	}
}

// Dial opens a TCP connection from the remote SSH host. Used by socks.go to
// route SOCKS5 dials through the SSH transport.
func (c *sshClient) Dial(network, addr string) (net.Conn, error) {
	return c.cli.Dial(network, addr)
}

// Close releases the underlying SSH connection.
func (c *sshClient) Close() error {
	if c.cli != nil {
		return c.cli.Close()
	}
	return nil
}
