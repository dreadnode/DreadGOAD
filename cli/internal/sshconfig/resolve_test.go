package sshconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// writeSSHConfig writes a temp ssh_config file and returns its path. Tests
// pass this path to resolveWithConfig because ssh -G ignores $HOME on macOS
// (it reads the real invoking user's home directory) and only the explicit
// -F flag overrides the lookup.
func writeSSHConfig(t *testing.T, content string) string {
	t.Helper()
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh not available")
	}
	path := filepath.Join(t.TempDir(), "ssh_config")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveAlias(t *testing.T) {
	cfg := writeSSHConfig(t, `Host proxmox
    HostName proxmox.example.com
    User admin
    Port 2222
    IdentityFile ~/.ssh/proxmox_key
`)

	got, err := resolveWithConfig("proxmox", cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Hostname != "proxmox.example.com" {
		t.Errorf("Hostname = %q, want proxmox.example.com", got.Hostname)
	}
	if got.Port != 2222 {
		t.Errorf("Port = %d, want 2222", got.Port)
	}
	if got.User != "admin" {
		t.Errorf("User = %q, want admin", got.User)
	}
	if got.IdentityFile == "" {
		t.Errorf("IdentityFile is empty, want a path")
	}
}

func TestResolveNoAlias(t *testing.T) {
	// Without a matching Host stanza, ssh -G echoes the input as the
	// hostname and applies the global default port.
	cfg := writeSSHConfig(t, "")

	got, err := resolveWithConfig("plain.example.com", cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Hostname != "plain.example.com" {
		t.Errorf("Hostname = %q, want plain.example.com", got.Hostname)
	}
	if got.Port != 22 {
		t.Errorf("Port = %d, want 22", got.Port)
	}
}

func TestResolveEmpty(t *testing.T) {
	if _, err := Resolve(""); err == nil {
		t.Fatal("expected error for empty target")
	}
}
