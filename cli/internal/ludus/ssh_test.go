package ludus

import (
	"bytes"
	"testing"
)

func TestParseSSHGOutput(t *testing.T) {
	sample := `host ludus-prod
hostname 198.51.100.42
user root
port 2222
identityagent "~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"
identityfile ~/.ssh/id_ed25519
identityfile ~/.ssh/id_rsa
proxyjump bastion.internal
stricthostkeychecking accept-new
userknownhostsfile ~/.ssh/known_hosts ~/.ssh/known_hosts2
preferredauthentications publickey,password
`

	rc, err := parseSSHGOutput(bytes.NewBufferString(sample))
	if err != nil {
		t.Fatalf("parseSSHGOutput: %v", err)
	}

	if rc.Hostname != "198.51.100.42" {
		t.Errorf("Hostname = %q, want 198.51.100.42", rc.Hostname)
	}
	if rc.User != "root" {
		t.Errorf("User = %q, want root", rc.User)
	}
	if rc.Port != 2222 {
		t.Errorf("Port = %d, want 2222", rc.Port)
	}
	if rc.IdentityAgent != "~/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock" {
		t.Errorf("IdentityAgent = %q (quotes should be stripped)", rc.IdentityAgent)
	}
	if len(rc.IdentityFiles) != 2 {
		t.Errorf("IdentityFiles = %v, want 2 entries", rc.IdentityFiles)
	}
	if rc.ProxyJump != "bastion.internal" {
		t.Errorf("ProxyJump = %q, want bastion.internal", rc.ProxyJump)
	}
	if rc.StrictHostKey != "accept-new" {
		t.Errorf("StrictHostKey = %q, want accept-new", rc.StrictHostKey)
	}
	if len(rc.UserKnownHostsFiles) != 2 {
		t.Errorf("UserKnownHostsFiles = %v, want 2 entries (space-split)", rc.UserKnownHostsFiles)
	}
}

func TestParseSSHGOutput_DefaultsPort22(t *testing.T) {
	rc, err := parseSSHGOutput(bytes.NewBufferString("hostname example.com\nuser alice\n"))
	if err != nil {
		t.Fatalf("parseSSHGOutput: %v", err)
	}
	if rc.Port != 22 {
		t.Errorf("Port = %d, want 22 default", rc.Port)
	}
}
