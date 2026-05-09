package labconfig

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lab.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write lab config: %v", err)
	}
	return path
}

func sortedEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	if len(g) != len(w) {
		t.Errorf("%s: len=%d want %d (got=%v want=%v)", label, len(g), len(w), g, w)
		return
	}
	for i := range g {
		if g[i] != w[i] {
			t.Errorf("%s: got=%v want=%v", label, g, w)
			return
		}
	}
}

func TestLoad_AggregatesAcrossDomains(t *testing.T) {
	path := writeConfig(t, `{
      "lab": {
        "domains": {
          "north": {
            "netbios_name": "north",
            "users":  {"Alice": {}, "BOB": {}},
            "groups": {"global": {"Engineers": {}}, "local": {"HelpDesk": {}}}
          },
          "south": {
            "netbios_name": "South",
            "users":  {"carol": {}},
            "groups": {"global": {"Engineers": {}, "Auditors": {}}}
          }
        },
        "hosts": {
          "dc01": {"hostname": "dc01"},
          "ws01": {"hostname": "WS01"},
          "ws02": {"hostname": "ws02"}
        }
      }
    }`)

	a, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	sortedEqual(t, "users", a.Users, []string{"alice", "bob", "carol"})
	sortedEqual(t, "computers", a.Computers, []string{"DC01", "WS01", "WS02"})
	sortedEqual(t, "groups", a.Groups, []string{"engineers", "helpdesk", "auditors"})
	sortedEqual(t, "trusts", a.Trusts, []string{"NORTH$", "SOUTH$"})
}

func TestLoad_OmitsEmptyNetbiosFromTrusts(t *testing.T) {
	path := writeConfig(t, `{
      "lab": {
        "domains": {
          "north": {"netbios_name": "", "users": {"alice": {}}}
        },
        "hosts": {}
      }
    }`)

	a, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(a.Trusts) != 0 {
		t.Errorf("trusts: got %v, want empty", a.Trusts)
	}
}

func TestLoad_SkipsHostsWithoutHostname(t *testing.T) {
	path := writeConfig(t, `{
      "lab": {
        "domains": {},
        "hosts": {
          "good": {"hostname": "good01"},
          "bad":  {"role": "dc"}
        }
      }
    }`)

	a, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sortedEqual(t, "computers", a.Computers, []string{"GOOD01"})
}

func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/path/lab.json"); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	path := writeConfig(t, `{ "lab":`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestLoad_EmptyConfig(t *testing.T) {
	path := writeConfig(t, `{}`)
	a, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(a.Users)+len(a.Computers)+len(a.Groups)+len(a.Trusts) != 0 {
		t.Errorf("expected empty allowlist, got %+v", a)
	}
}

// Same user/group/host appearing in multiple domains should collapse to a
// single allowlist entry — the underlying map dedups by case-folded key,
// and the test guards that behavior so future map→slice rewrites can't
// silently double-count.
func TestLoad_DedupsAcrossDomains(t *testing.T) {
	path := writeConfig(t, `{
      "lab": {
        "domains": {
          "north": {
            "netbios_name": "shared",
            "users":  {"alice": {}, "ALICE": {}},
            "groups": {"global": {"Engineers": {}}}
          },
          "south": {
            "netbios_name": "SHARED",
            "users":  {"Alice": {}},
            "groups": {"global": {"engineers": {}}}
          }
        },
        "hosts": {
          "h1": {"hostname": "DC01"},
          "h2": {"hostname": "dc01"}
        }
      }
    }`)

	a, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	sortedEqual(t, "users", a.Users, []string{"alice"})
	sortedEqual(t, "groups", a.Groups, []string{"engineers"})
	sortedEqual(t, "computers", a.Computers, []string{"DC01"})
	sortedEqual(t, "trusts", a.Trusts, []string{"SHARED$"})
}
