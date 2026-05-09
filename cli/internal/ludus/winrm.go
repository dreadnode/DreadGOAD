// Package ludus — winrm.go provides a direct-from-Go WinRM client whose
// network dials are routed through the long-lived SSH connection to the
// Ludus host. This replaces the previous "spawn `ansible win_shell` over
// ssh" path used by the validator: one TCP+HTTP round-trip per PS call
// instead of fork(ansible) + python + winrm + ssh teardown for every
// check, which routinely blew past the validator's 15s per-call budget.
package ludus

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/masterzen/winrm"
)

// winrmCreds holds the credentials used for every WinRM call. Ludus
// templates default to localuser/password; if a deployment ever uses
// different lab creds these need to flow in through SSHConfig or a
// dedicated config block.
type winrmCreds struct {
	User     string
	Password string
}

// defaultWinRMCreds matches the Ludus stock localuser account that the
// previous ansible path was hardcoded against.
var defaultWinRMCreds = winrmCreds{User: "localuser", Password: "password"}

// winrmRunner runs PowerShell over WinRM (HTTP/5985, NTLM) with all TCP
// dials routed through a shared SSH connection. Clients are cached per
// host IP because *winrm.Client is safe for concurrent use and the cache
// avoids re-parsing the endpoint on every call.
type winrmRunner struct {
	dial    func(network, addr string) (net.Conn, error)
	creds   winrmCreds
	clients sync.Map // ip -> *winrm.Client
}

// newWinRMRunner wires the runner to an SSH dialer (typically
// (*sshClient).Dial). The dialer is called once per WinRM HTTP request
// to open a forwarded TCP channel to ip:5985.
func newWinRMRunner(dial func(network, addr string) (net.Conn, error)) *winrmRunner {
	return &winrmRunner{dial: dial, creds: defaultWinRMCreds}
}

// clientFor returns a cached or freshly-built *winrm.Client for ip:5985.
// We construct with NewClientWithParameters so we can inject the custom
// Dial; the standard NewClient builds its own net.Dialer and would not
// route through SSH.
func (r *winrmRunner) clientFor(ip string) (*winrm.Client, error) {
	if v, ok := r.clients.Load(ip); ok {
		return v.(*winrm.Client), nil
	}

	// HTTPS/5986 with insecure=true (self-signed Ludus certs). We
	// deliberately use Basic auth (the default in masterzen/winrm) rather
	// than NTLM: Azure/go-ntlmssp's NTLM negotiation requires three
	// HTTP round-trips on the same TCP connection, which Go's
	// http.Transport breaks under concurrent requests through one
	// *winrm.Client (the second goroutine grabs the keep-alive socket
	// mid-handshake → 401). Basic over TLS is stateless, so concurrent
	// fan-out works. AllowUnencrypted=false on the server is satisfied
	// because TLS provides the transport-level encryption it requires.
	endpoint := winrm.NewEndpoint(ip, 5986, true, true, nil, nil, nil, 0)
	params := winrm.DefaultParameters
	params.Dial = r.dial
	// PT3M matches the ansible path's prior winrm operation/read timeouts
	// (400/500s); keeping a generous server-side window here lets the
	// per-call context.WithTimeout govern the actual deadline.
	params.Timeout = "PT3M"

	c, err := winrm.NewClientWithParameters(endpoint, r.creds.User, r.creds.Password, params)
	if err != nil {
		return nil, fmt.Errorf("winrm client for %s: %w", ip, err)
	}
	actual, _ := r.clients.LoadOrStore(ip, c)
	return actual.(*winrm.Client), nil
}

// RunPS executes a PowerShell command against ip and returns
// (stdout, stderr, error). A non-zero WinRM exit code is surfaced as an
// error so callers (which only inspect err and result.Stdout) treat it
// as a failure the same way the ansible path did.
func (r *winrmRunner) RunPS(ctx context.Context, ip, command string, timeout time.Duration) (string, string, error) {
	cli, err := r.clientFor(ip)
	if err != nil {
		return "", "", err
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stdout, stderr, code, err := cli.RunPSWithContextWithString(cmdCtx, command, "")
	if err != nil {
		return stdout, stderr, fmt.Errorf("winrm %s: %w", ip, err)
	}
	if code != 0 {
		return stdout, stderr, fmt.Errorf("winrm %s exit %d: %s", ip, code, failureDetail(stderr, stdout, nil))
	}
	return stdout, stderr, nil
}
