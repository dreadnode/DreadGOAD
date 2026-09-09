package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

// credFixture writes an inventory and the materialized lab config Terraform
// would have read, then returns a config pointing at both.
func credFixture(t *testing.T, invBody string, hostPasswords map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
	// Azure explicitly: the credential check is scoped to it, so a fixture
	// without a provider would make every assertion here pass vacuously.
	cfg := &config.Config{ProjectRoot: root, Env: "e1", Provider: "azure"}

	if err := os.WriteFile(cfg.InventoryPath(), []byte(invBody), 0o644); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(root, "ad", "GOAD", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(`{"lab":{"hosts":{`)
	first := true
	for host, pw := range hostPasswords {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(`"` + host + `":{"local_admin_password":"` + pw + `"}`)
	}
	b.WriteString(`}}}`)
	if err := os.WriteFile(filepath.Join(dataDir, "e1-config.json"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The 3.1 shape: the inventory was scaffolded from a provider template whose
// stock passwords were never reconciled with the generated config, so no host
// can authenticate. Measured on the real range: 0/5 matched.
func TestValidateInventoryCredentialsBlocksTotalMismatch(t *testing.T) {
	cfg := credFixture(t,
		"[default]\n"+
			"dc01 ansible_host=10.1.1.5 ansible_user=ansible ansible_password=from-template-a\n"+
			"dc02 ansible_host=10.1.1.6 ansible_user=ansible ansible_password=from-template-b\n",
		map[string]string{"dc01": "built-with-a", "dc02": "built-with-b"})

	err := validateInventoryCredentials(cfg)
	if err == nil {
		t.Fatal("every host had the wrong password and provisioning was allowed to start")
	}
	for _, want := range []string{"dc01", "dc02", "WinRM"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q: %v", want, err)
		}
	}
	// The whole point of fingerprinting during diagnosis: secrets must not be
	// echoed into a terminal or a CI log.
	for _, secret := range []string{"from-template-a", "built-with-a", "from-template-b", "built-with-b"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaked a password: %v", err)
		}
	}
}

// The dreadindex shape: the range that provisioned successfully matched on
// every host (measured 5/5).
func TestValidateInventoryCredentialsAcceptsFullMatch(t *testing.T) {
	cfg := credFixture(t,
		"[default]\n"+
			"dc01 ansible_host=10.1.1.5 ansible_user=ansible ansible_password=shared-a\n"+
			"srv02 ansible_host=10.1.1.8 ansible_user=ansible ansible_password=shared-b\n",
		map[string]string{"dc01": "shared-a", "srv02": "shared-b"})

	if err := validateInventoryCredentials(cfg); err != nil {
		t.Fatalf("a correctly-provisioned range was blocked: %v", err)
	}
}

// One host drifting is not the scaffolding bug and must not block a range that
// is otherwise fine — a DC whose account moved into the domain, say.
func TestValidateInventoryCredentialsAllowsPartialDrift(t *testing.T) {
	cfg := credFixture(t,
		"[default]\n"+
			"dc01 ansible_host=10.1.1.5 ansible_password=shared-a\n"+
			"srv02 ansible_host=10.1.1.8 ansible_password=drifted\n",
		map[string]string{"dc01": "shared-a", "srv02": "shared-b"})

	if err := validateInventoryCredentials(cfg); err != nil {
		t.Fatalf("a single drifted host blocked the run: %v", err)
	}
}

// Absence of the materialized config is not evidence of a mismatch. Blocking
// here would break every layout that does not materialize one.
func TestValidateInventoryCredentialsSkipsWithoutConfig(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{ProjectRoot: root, Env: "e1", Provider: "azure"}
	if err := os.WriteFile(cfg.InventoryPath(),
		[]byte("[default]\ndc01 ansible_host=10.1.1.5 ansible_password=x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateInventoryCredentials(cfg); err != nil {
		t.Fatalf("missing lab config was treated as a mismatch: %v", err)
	}
}

// Hosts the config says nothing about, and hosts with no password in either
// place, are not comparable — counting them would manufacture a mismatch.
func TestValidateInventoryCredentialsIgnoresIncomparableHosts(t *testing.T) {
	cfg := credFixture(t,
		"[default]\n"+
			"dc01 ansible_host=10.1.1.5 ansible_password=shared-a\n"+
			"kali ansible_host=10.1.3.9\n"+
			"ghost ansible_host=10.1.1.9 ansible_password=whatever\n",
		map[string]string{"dc01": "shared-a"})

	if err := validateInventoryCredentials(cfg); err != nil {
		t.Fatalf("incomparable hosts produced a mismatch: %v", err)
	}
}

// Corrupt JSON is a different problem with a different fix; it must not be
// reported as a credential mismatch.
func TestValidateInventoryCredentialsSurvivesCorruptConfig(t *testing.T) {
	cfg := credFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password=x\n",
		map[string]string{"dc01": "y"})
	bad := filepath.Join(cfg.ProjectRoot, "ad", "GOAD", "data", "e1-config.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := validateInventoryCredentials(cfg); err != nil {
		t.Fatalf("corrupt config was reported as a credential mismatch: %v", err)
	}
}

// The inventory parser strips quotes from ansible_password. A quoted config
// value must therefore still compare equal, or every generated range with a
// shell-unsafe password would be falsely blocked.
func TestValidateInventoryCredentialsHandlesQuotedPasswords(t *testing.T) {
	cfg := credFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password='F](O:4<O,&(V'\n",
		map[string]string{"dc01": `F](O:4<O,&(V`})
	if err := validateInventoryCredentials(cfg); err != nil {
		t.Fatalf("a quoted password was treated as a mismatch: %v", err)
	}
}
