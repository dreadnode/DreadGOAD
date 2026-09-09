package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

// gateConfig builds a config whose InventoryPath() points at a temp inventory
// holding body.
func gateConfig(t *testing.T, body string) *config.Config {
	t.Helper()
	root := t.TempDir()
	cfg := &config.Config{ProjectRoot: root, Env: "3.1"}
	if err := os.WriteFile(cfg.InventoryPath(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// The exact state that reached Ansible and produced "unreachable" on all five
// Windows hosts. Preflight must stop here instead.
func TestValidateInventoryResolvedBlocksPendingHosts(t *testing.T) {
	cfg := gateConfig(t, "[default]\n"+
		"dc01 ansible_host=PENDING dns_domain=dc01 dict_key=dc01\n"+
		"srv02 ansible_host=PENDING dns_domain=dc02 dict_key=srv02\n")

	err := validateInventoryResolved(cfg, "")
	if err == nil {
		t.Fatal("preflight accepted an inventory with no addresses")
	}
	for _, want := range []string{"dc01", "srv02", "inventory sync"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q, so it is not actionable: %v", want, err)
		}
	}
}

// Ludus and Proxmox reach preflight through bootstrap, which is a no-op when
// the inventory already exists. An unrendered template therefore survives, and
// fails Ansible exactly the way PENDING does.
func TestValidateInventoryResolvedBlocksUnrenderedTemplate(t *testing.T) {
	cfg := gateConfig(t, "[default]\ndc01 ansible_host={{ip_range}}.10 dict_key=dc01\n")
	if err := validateInventoryResolved(cfg, ""); err == nil {
		t.Fatal("preflight accepted an unrendered {{ip_range}} template")
	}
}

func TestValidateInventoryResolvedAcceptsRealAddresses(t *testing.T) {
	// Both address shapes the codebase supports: an Azure/Ludus private IP and
	// an AWS SSM instance ID.
	cfg := gateConfig(t, "[default]\n"+
		"dc01 ansible_host=10.100.1.5 dict_key=dc01\n"+
		"dc02 ansible_host=i-0e428dfc02f5007dd dict_key=dc02\n")
	if err := validateInventoryResolved(cfg, ""); err != nil {
		t.Fatalf("a fully resolved inventory was rejected: %v", err)
	}
}

// A deliberate partial run must not be blocked by a host it never targets.
// The operator still gets told, because the limit may well select it.
func TestValidateInventoryResolvedDowngradesUnderLimit(t *testing.T) {
	cfg := gateConfig(t, "[default]\n"+
		"dc01 ansible_host=10.100.1.5 dict_key=dc01\n"+
		"srv03 ansible_host=PENDING dict_key=srv03\n")
	if err := validateInventoryResolved(cfg, "dc01"); err != nil {
		t.Fatalf("--limit run was blocked by an out-of-scope host: %v", err)
	}
}

// A commented-out host is not a host. Reporting it would make the error name a
// machine that is not in the run, and an error that names phantom hosts stops
// being trusted.
func TestValidateInventoryResolvedIgnoresCommentedHosts(t *testing.T) {
	cfg := gateConfig(t, "[default]\n"+
		"; dc09 ansible_host=PENDING dict_key=dc09\n"+
		"# dc10 ansible_host=PENDING dict_key=dc10\n"+
		"dc01 ansible_host=10.100.1.5 dict_key=dc01\n")
	if err := validateInventoryResolved(cfg, ""); err != nil {
		t.Fatalf("commented-out hosts were treated as live: %v", err)
	}
}

// The Azure sync runs before the gate and can hard-fail on its own. If it did
// so unconditionally it would override --limit and block a partial run that
// the gate would have allowed — the two must agree on the policy.
func TestInventorySyncFailureRespectsLimit(t *testing.T) {
	boom := errors.New("srv99 still has placeholder ansible_host")

	if err := inventorySyncFailure(boom, ""); err == nil {
		t.Error("an unlimited run must stop when the sync cannot resolve a host")
	} else if !strings.Contains(err.Error(), "srv99") {
		t.Errorf("wrapped error lost the cause: %v", err)
	}

	if err := inventorySyncFailure(boom, "dc01"); err != nil {
		t.Errorf("--limit run was blocked by a sync failure: %v", err)
	}

	if err := inventorySyncFailure(nil, ""); err != nil {
		t.Errorf("a successful sync produced an error: %v", err)
	}
}

// A missing inventory is a different failure with a different fix, so it must
// not be reported as an address problem.
func TestValidateInventoryResolvedReportsAMissingFile(t *testing.T) {
	cfg := &config.Config{ProjectRoot: t.TempDir(), Env: "nope"}
	err := validateInventoryResolved(cfg, "")
	if err == nil {
		t.Fatal("a missing inventory passed validation")
	}
	if !strings.Contains(err.Error(), "read inventory") {
		t.Errorf("error does not identify the real cause: %v", err)
	}
}

// The live inventory this whole change exists to fix, verbatim from disk, must
// pass now that it has been synced.
func TestValidateInventoryResolvedAcceptsTheRepairedLiveInventory(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "3.1-inventory"))
	if err != nil {
		t.Skip("3.1-inventory not present in this working tree")
	}
	if stale := placeholderHosts(string(data)); len(stale) > 0 {
		t.Errorf("3.1-inventory still has unresolved hosts: %v", stale)
	}
}
