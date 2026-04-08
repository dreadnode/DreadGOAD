package inventory

import (
	"os"
	"path/filepath"
	"testing"
)

const testInventory = `; GOAD inventory - auto-generated
[default]
DC01 ansible_host=i-0e428dfc02f5007dd dict_key=dc01 dns_domain=sevenkingdoms.local ansible_user=vagrant
DC02 ansible_host=i-0abc123def456789a dict_key=dc02 dns_domain=north.sevenkingdoms.local ansible_user=vagrant
SRV01 ansible_host=i-0fff999888777666a dict_key=srv01 dns_domain=sevenkingdoms.local ansible_user=vagrant

[all:vars]
ansible_aws_ssm_region=us-east-1
ansible_connection=aws_ssm

[dc]
DC01
DC02

[server]
SRV01

[north]
DC02
`

func writeTestInventory(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test-inventory")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func parseTestInventory(t *testing.T) *Inventory {
	t.Helper()
	path := writeTestInventory(t, testInventory)
	inv, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return inv
}

func TestParse_HostCount(t *testing.T) {
	inv := parseTestInventory(t)
	if len(inv.Hosts) != 3 {
		t.Errorf("got %d hosts, want 3", len(inv.Hosts))
	}
}

func TestParse_HostAttributes(t *testing.T) {
	inv := parseTestInventory(t)
	dc01 := inv.Hosts["DC01"]
	if dc01 == nil {
		t.Fatal("DC01 not found")
	}
	if dc01.InstanceID != "i-0e428dfc02f5007dd" {
		t.Errorf("InstanceID = %q, want %q", dc01.InstanceID, "i-0e428dfc02f5007dd")
	}
	if dc01.DictKey != "dc01" {
		t.Errorf("DictKey = %q, want %q", dc01.DictKey, "dc01")
	}
	if dc01.DNSDomain != "sevenkingdoms.local" {
		t.Errorf("DNSDomain = %q, want %q", dc01.DNSDomain, "sevenkingdoms.local")
	}
	if dc01.User != "vagrant" {
		t.Errorf("User = %q, want %q", dc01.User, "vagrant")
	}
}

func TestParse_Vars(t *testing.T) {
	inv := parseTestInventory(t)
	if inv.Vars["ansible_aws_ssm_region"] != "us-east-1" {
		t.Errorf("region = %q, want %q", inv.Vars["ansible_aws_ssm_region"], "us-east-1")
	}
	if inv.Vars["ansible_connection"] != "aws_ssm" {
		t.Errorf("connection = %q, want %q", inv.Vars["ansible_connection"], "aws_ssm")
	}
}

func TestParse_Groups(t *testing.T) {
	inv := parseTestInventory(t)
	if len(inv.Groups["dc"]) != 2 {
		t.Errorf("dc group has %d members, want 2", len(inv.Groups["dc"]))
	}
	if len(inv.Groups["server"]) != 1 {
		t.Errorf("server group has %d members, want 1", len(inv.Groups["server"]))
	}
}

func TestParse_GroupMembership(t *testing.T) {
	inv := parseTestInventory(t)
	dc02 := inv.Hosts["DC02"]
	if dc02 == nil {
		t.Fatal("DC02 not found")
	}
	wantGroups := map[string]bool{"dc": false, "north": false}
	for _, g := range dc02.Groups {
		if _, ok := wantGroups[g]; ok {
			wantGroups[g] = true
		}
	}
	for g, found := range wantGroups {
		if !found {
			t.Errorf("DC02 missing group %q", g)
		}
	}
}

func TestParse_FileNotFound(t *testing.T) {
	_, err := Parse("/nonexistent/inventory")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestParse_EmptyFile(t *testing.T) {
	path := writeTestInventory(t, "")
	inv, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(inv.Hosts) != 0 {
		t.Errorf("got %d hosts, want 0", len(inv.Hosts))
	}
}

func TestParse_CommentsOnly(t *testing.T) {
	path := writeTestInventory(t, "# comment\n; another comment\n")
	inv, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(inv.Hosts) != 0 {
		t.Errorf("got %d hosts, want 0", len(inv.Hosts))
	}
}

func TestInstanceIDs(t *testing.T) {
	path := writeTestInventory(t, testInventory)
	inv, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	ids := inv.InstanceIDs()
	if len(ids) != 3 {
		t.Errorf("InstanceIDs() returned %d, want 3", len(ids))
	}

	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	for _, want := range []string{"i-0e428dfc02f5007dd", "i-0abc123def456789a", "i-0fff999888777666a"} {
		if !idSet[want] {
			t.Errorf("InstanceIDs() missing %q", want)
		}
	}
}

func TestRegion(t *testing.T) {
	t.Run("from vars", func(t *testing.T) {
		path := writeTestInventory(t, testInventory)
		inv, _ := Parse(path)
		if got := inv.Region(); got != "us-east-1" {
			t.Errorf("Region() = %q, want %q", got, "us-east-1")
		}
	})

	t.Run("missing returns empty", func(t *testing.T) {
		path := writeTestInventory(t, "[default]\n")
		inv, _ := Parse(path)
		if got := inv.Region(); got != "" {
			t.Errorf("Region() = %q, want empty string (no silent fallback)", got)
		}
	})
}

func TestHostByName(t *testing.T) {
	path := writeTestInventory(t, testInventory)
	inv, _ := Parse(path)

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{"exact match", "DC01", "DC01"},
		{"case insensitive", "dc01", "DC01"},
		{"mixed case", "Dc02", "DC02"},
		{"not found", "NONEXISTENT", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inv.HostByName(tt.query)
			if tt.want == "" {
				if got != nil {
					t.Errorf("HostByName(%q) = %v, want nil", tt.query, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("HostByName(%q) = nil, want %q", tt.query, tt.want)
			}
			if got.Name != tt.want {
				t.Errorf("HostByName(%q).Name = %q, want %q", tt.query, got.Name, tt.want)
			}
		})
	}
}

func TestHostByInstanceID(t *testing.T) {
	path := writeTestInventory(t, testInventory)
	inv, _ := Parse(path)

	t.Run("found", func(t *testing.T) {
		got := inv.HostByInstanceID("i-0e428dfc02f5007dd")
		if got == nil || got.Name != "DC01" {
			t.Errorf("HostByInstanceID() = %v, want DC01", got)
		}
	})

	t.Run("not found", func(t *testing.T) {
		got := inv.HostByInstanceID("i-nonexistent")
		if got != nil {
			t.Errorf("HostByInstanceID() = %v, want nil", got)
		}
	})
}
