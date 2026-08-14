package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	inv "github.com/dreadnode/dreadgoad/internal/inventory"
)

// pwFixture writes an inventory plus the materialized lab config Terraform
// consumed, and returns a config pointing at both.
func pwFixture(t *testing.T, invBody string, hostPasswords map[string]string) *config.Config {
	t.Helper()
	root := t.TempDir()
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
		esc := strings.ReplaceAll(pw, `\`, `\\`)
		esc = strings.ReplaceAll(esc, `"`, `\"`)
		b.WriteString(`"` + host + `":{"local_admin_password":"` + esc + `"}`)
	}
	b.WriteString(`}}}`)
	if err := os.WriteFile(filepath.Join(dataDir, "e1-config.json"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// readBack parses the inventory the way the rest of the CLI does, so the test
// asserts what a consumer actually sees rather than raw file text.
func readBack(t *testing.T, cfg *config.Config) *inv.Inventory {
	t.Helper()
	parsed, err := inv.Parse(cfg.InventoryPath())
	if err != nil {
		t.Fatalf("inventory no longer parses: %v", err)
	}
	return parsed
}

// The 3.1 shape: template passwords that match no config. After the sync every
// host presents the password its machine was actually built with.
func TestSyncAzurePasswordsReconcilesTemplateValues(t *testing.T) {
	cfg := pwFixture(t,
		"[default]\n"+
			// Placeholder stand-ins for the provider template's stock values;
			// no reason to give those a second home in the test suite.
			"dc01 ansible_host=10.100.1.5 dns_domain=dc01 dict_key=dc01 ansible_user=ansible ansible_password=from-template-dc01\n"+
			"srv02 ansible_host=10.100.1.6 dns_domain=dc02 dict_key=srv02 ansible_user=ansible ansible_password=from-template-srv02\n",
		map[string]string{"dc01": "built-with-dc01", "srv02": "built-with-srv02"})

	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatalf("sync: %v", err)
	}
	parsed := readBack(t, cfg)
	for host, want := range map[string]string{"dc01": "built-with-dc01", "srv02": "built-with-srv02"} {
		if got := parsed.HostByName(host).Password; got != want {
			t.Errorf("%s password = %q, want the value from the lab config", host, got)
		}
	}
	// Everything else on the line has to survive untouched.
	if h := parsed.HostByName("dc01"); h.InstanceID != "10.100.1.5" || h.User != "ansible" || h.DictKey != "dc01" {
		t.Errorf("sync damaged other fields on the host line: %+v", h)
	}
}

// After the sync, the gate that blocks provisioning must be satisfied. These
// two have to agree or the fix does not actually unblock anything.
func TestSyncThenCredentialGatePasses(t *testing.T) {
	cfg := pwFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password=stock-a\ndc02 ansible_host=10.1.1.6 ansible_password=stock-b\n",
		map[string]string{"dc01": "real-a", "dc02": "real-b"})

	if err := validateInventoryCredentials(cfg); err == nil {
		t.Fatal("gate should have blocked before the sync ran")
	}
	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if err := validateInventoryCredentials(cfg); err != nil {
		t.Fatalf("gate still blocks after a successful sync: %v", err)
	}
}

// Passwords carry shell metacharacters. "$" in particular would be read as a
// capture-group reference by a naive regexp replacement, silently corrupting
// the value.
func TestSyncAzurePasswordsHandlesMetacharacters(t *testing.T) {
	tricky := `a$1b&c|d;e<f>g?h*i(j)k=l+m!n@o#p%q^r-s_t.u,v:w`
	cfg := pwFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password=old\n",
		map[string]string{"dc01": tricky})

	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := readBack(t, cfg).HostByName("dc01").Password; got != tricky {
		t.Errorf("metacharacters were mangled:\n got  %q\n want %q", got, tricky)
	}
}

// A password containing a single quote must round-trip via double quotes.
func TestSyncAzurePasswordsQuotesCorrectly(t *testing.T) {
	withSingle := `has'single`
	cfg := pwFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password=old\n",
		map[string]string{"dc01": withSingle})
	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := readBack(t, cfg).HostByName("dc01").Password; got != withSingle {
		t.Errorf("password with a single quote = %q, want %q", got, withSingle)
	}
}

// Both quote characters cannot be represented by inventory.stripQuotes. Writing
// something that parses back differently is worse than leaving it alone.
func TestSyncAzurePasswordsSkipsUnrepresentableValues(t *testing.T) {
	cfg := pwFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password=untouched\n",
		map[string]string{"dc01": `both'and"quotes`})
	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := readBack(t, cfg).HostByName("dc01").Password; got != "untouched" {
		t.Errorf("wrote an unrepresentable value: %q", got)
	}
}

// An already-quoted value in the inventory must be replaced whole, not nested
// inside the old quotes.
func TestSyncAzurePasswordsReplacesQuotedValues(t *testing.T) {
	cfg := pwFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password='old value' dict_key=dc01\n",
		map[string]string{"dc01": "new-secret"})
	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatalf("sync: %v", err)
	}
	parsed := readBack(t, cfg)
	if got := parsed.HostByName("dc01").Password; got != "new-secret" {
		t.Errorf("password = %q, want new-secret", got)
	}
	if parsed.HostByName("dc01").DictKey != "dc01" {
		t.Error("trailing fields were consumed by the replacement")
	}
}

// Hosts the config says nothing about keep whatever they had.
func TestSyncAzurePasswordsLeavesUnknownHostsAlone(t *testing.T) {
	cfg := pwFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password=keep-me\nkali ansible_host=10.1.3.9 ansible_password=kali-pw\n",
		map[string]string{"dc01": "keep-me"})
	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := readBack(t, cfg).HostByName("kali").Password; got != "kali-pw" {
		t.Errorf("kali password changed to %q", got)
	}
}

// No materialized config means nothing to reconcile against, and must not be
// treated as an error or wipe the inventory.
func TestSyncAzurePasswordsNoOpsWithoutConfig(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{ProjectRoot: root, Env: "e1", Provider: "azure"}
	body := "[default]\ndc01 ansible_host=10.1.1.5 ansible_password=x\n"
	if err := os.WriteFile(cfg.InventoryPath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatalf("missing config produced an error: %v", err)
	}
	got, err := os.ReadFile(cfg.InventoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Errorf("inventory was rewritten with no config present:\n%s", got)
	}
}

// Running twice must produce the same file — preflight runs on every provision.
func TestSyncAzurePasswordsIsIdempotent(t *testing.T) {
	cfg := pwFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password=stock\n",
		map[string]string{"dc01": "real"})
	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(cfg.InventoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(cfg.InventoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("second run changed the file:\n%s\n---\n%s", first, second)
	}
}

// The credential gate tells the operator to run `inventory sync`. If that
// command does not reconcile passwords, the message is a dead end: the sync
// reports success and the gate keeps blocking with no way forward.
func TestCredentialGateErrorNamesAWorkingCommand(t *testing.T) {
	cfg := pwFixture(t,
		"[default]\ndc01 ansible_host=10.1.1.5 ansible_password=stock\n",
		map[string]string{"dc01": "real"})

	err := validateInventoryCredentials(cfg)
	if err == nil {
		t.Fatal("expected the gate to block")
	}
	if !strings.Contains(err.Error(), "inventory sync") {
		t.Fatalf("error does not name a command to run: %v", err)
	}
	// Now do what the message says, via the same function the command calls.
	if err := syncAzureInventoryPasswords(cfg); err != nil {
		t.Fatalf("the remedy the error names failed: %v", err)
	}
	if err := validateInventoryCredentials(cfg); err != nil {
		t.Fatalf("following the error's own instruction did not clear it: %v", err)
	}
}

// AWS ships an inventory whose passwords match no config and are never sent —
// it authenticates over SSM. Comparing there would block a working provider.
func TestCredentialGateIgnoresNonAzureProviders(t *testing.T) {
	for _, prov := range []string{"aws", "ludus", "proxmox", ""} {
		cfg := pwFixture(t,
			"[default]\ndc01 ansible_host=i-0abc ansible_password=template-value\n",
			map[string]string{"dc01": "totally-different"})
		cfg.Provider = prov
		if err := validateInventoryCredentials(cfg); err != nil {
			t.Errorf("provider %q was blocked by the Azure credential check: %v", prov, err)
		}
	}
}
