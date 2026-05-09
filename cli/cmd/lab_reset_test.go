package cmd

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	inv "github.com/dreadnode/dreadgoad/internal/inventory"
	"github.com/dreadnode/dreadgoad/internal/labconfig"
)

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   ", nil},
		{"single", "user", []string{"user"}},
		{"multiple", "user,computer,group", []string{"user", "computer", "group"}},
		{"surrounding spaces", " user , computer ", []string{"user", "computer"}},
		{"trailing comma", "user,computer,", []string{"user", "computer"}},
		{"leading comma", ",user,computer", []string{"user", "computer"}},
		{"empty middle", "user,,computer", []string{"user", "computer"}},
		{"whitespace-only segment", "user, ,computer", []string{"user", "computer"}},
		// only-commas takes the non-early-return path and yields a 0-len
		// slice (may be nil or non-nil — callers use len()).
		{"only commas", ",,,", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCSV(tt.in)
			if len(tt.want) == 0 {
				if len(got) != 0 {
					t.Errorf("splitCSV(%q) = %v, want empty", tt.in, got)
				}
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitCSV(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// newDCInventory builds an *inv.Inventory containing a [dc] group whose
// members map to the given hostname → instance-ID pairs. Use ghostHosts
// for names that should appear in the dc group but be missing from the
// Hosts map (simulates a malformed inventory).
func newDCInventory(hosts map[string]string, ghostHosts ...string) *inv.Inventory {
	out := &inv.Inventory{
		Hosts:  map[string]*inv.Host{},
		Groups: map[string][]string{},
	}
	for name, id := range hosts {
		out.Hosts[name] = &inv.Host{Name: name, InstanceID: id}
		out.Groups["dc"] = append(out.Groups["dc"], name)
	}
	out.Groups["dc"] = append(out.Groups["dc"], ghostHosts...)
	sort.Strings(out.Groups["dc"]) // deterministic order
	return out
}

func targetNames(targets []dcTarget) []string {
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = t.hostname
	}
	sort.Strings(names)
	return names
}

func TestCollectDCTargets(t *testing.T) {
	t.Run("returns hosts with valid instance IDs", func(t *testing.T) {
		parsed := newDCInventory(map[string]string{
			"dc01": "i-aaa",
			"dc02": "i-bbb",
		})
		got := targetNames(collectDCTargets(parsed, nil))
		want := []string{"dc01", "dc02"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got=%v want=%v", got, want)
		}
	})

	t.Run("skips hosts with empty or PENDING instance IDs", func(t *testing.T) {
		parsed := newDCInventory(map[string]string{
			"dc01": "i-aaa",
			"dc02": "",
			"dc03": "PENDING",
		})
		got := targetNames(collectDCTargets(parsed, nil))
		if !reflect.DeepEqual(got, []string{"dc01"}) {
			t.Errorf("got=%v want=[dc01]", got)
		}
	})

	t.Run("skips host listed in dc group but missing from Hosts map", func(t *testing.T) {
		parsed := newDCInventory(map[string]string{"dc01": "i-aaa"}, "phantom")
		got := targetNames(collectDCTargets(parsed, nil))
		if !reflect.DeepEqual(got, []string{"dc01"}) {
			t.Errorf("got=%v want=[dc01]", got)
		}
	})

	t.Run("filter is case-insensitive on both sides", func(t *testing.T) {
		parsed := newDCInventory(map[string]string{
			"DC01": "i-aaa",
			"dc02": "i-bbb",
		})
		got := targetNames(collectDCTargets(parsed, []string{"dc01"}))
		if !reflect.DeepEqual(got, []string{"DC01"}) {
			t.Errorf("got=%v want=[DC01]", got)
		}
	})

	t.Run("nil and empty filter both return all", func(t *testing.T) {
		parsed := newDCInventory(map[string]string{
			"dc01": "i-aaa",
			"dc02": "i-bbb",
		})
		nilGot := targetNames(collectDCTargets(parsed, nil))
		emptyGot := targetNames(collectDCTargets(parsed, []string{}))
		want := []string{"dc01", "dc02"}
		if !reflect.DeepEqual(nilGot, want) || !reflect.DeepEqual(emptyGot, want) {
			t.Errorf("nil=%v empty=%v want=%v", nilGot, emptyGot, want)
		}
	})

	t.Run("filter with no match returns empty", func(t *testing.T) {
		parsed := newDCInventory(map[string]string{"dc01": "i-aaa"})
		got := collectDCTargets(parsed, []string{"dc99"})
		if len(got) != 0 {
			t.Errorf("got=%+v want empty", got)
		}
	})

	t.Run("instance ID preserved on returned target", func(t *testing.T) {
		parsed := newDCInventory(map[string]string{"dc01": "i-aaa"})
		got := collectDCTargets(parsed, nil)
		if len(got) != 1 || got[0].hostname != "dc01" || got[0].instanceID != "i-aaa" {
			t.Errorf("got=%+v want hostname=dc01 instanceID=i-aaa", got)
		}
	})
}

func TestParsePurgeResult(t *testing.T) {
	payload := `{"DC":"DC01","Users":["alice"],"Computers":["WS01"],"Groups":[],` +
		`"Skipped":[{"Class":"user","Name":"svc","Reason":"admin-creator"}],` +
		`"Errors":["boom"],"RemovedUsers":1,"RemovedComputers":0,"RemovedGroups":0}`

	t.Run("valid result with transcript noise above marker", func(t *testing.T) {
		stdout := "noise line one\nnoise line two\n" + purgeResultMarker + "\n" + payload + "\n"
		r, err := parsePurgeResult(stdout)
		if err != nil {
			t.Fatalf("parsePurgeResult: %v", err)
		}
		if r.DC != "DC01" {
			t.Errorf("DC=%q want DC01", r.DC)
		}
		if !reflect.DeepEqual(r.Users, []string{"alice"}) {
			t.Errorf("Users=%v want [alice]", r.Users)
		}
		if !reflect.DeepEqual(r.Computers, []string{"WS01"}) {
			t.Errorf("Computers=%v want [WS01]", r.Computers)
		}
		if r.RemovedUsers != 1 {
			t.Errorf("RemovedUsers=%d want 1", r.RemovedUsers)
		}
		if len(r.Skipped) != 1 || r.Skipped[0].Reason != "admin-creator" {
			t.Errorf("Skipped=%+v want one admin-creator entry", r.Skipped)
		}
		if !reflect.DeepEqual(r.Errors, []string{"boom"}) {
			t.Errorf("Errors=%v want [boom]", r.Errors)
		}
	})

	t.Run("payload immediately after marker with no newline", func(t *testing.T) {
		stdout := purgeResultMarker + payload
		r, err := parsePurgeResult(stdout)
		if err != nil {
			t.Fatalf("parsePurgeResult: %v", err)
		}
		if r.DC != "DC01" {
			t.Errorf("DC=%q want DC01", r.DC)
		}
	})

	t.Run("marker missing", func(t *testing.T) {
		_, err := parsePurgeResult("just some output\nno marker here\n")
		if err == nil {
			t.Fatal("expected error when marker missing")
		}
	})

	t.Run("empty payload after marker", func(t *testing.T) {
		_, err := parsePurgeResult(purgeResultMarker + "\n   \n")
		if err == nil {
			t.Fatal("expected error for empty payload")
		}
	})

	t.Run("malformed JSON after marker", func(t *testing.T) {
		_, err := parsePurgeResult(purgeResultMarker + "\n{not json\n")
		if err == nil {
			t.Fatal("expected error for malformed JSON")
		}
	})

	t.Run("empty input", func(t *testing.T) {
		if _, err := parsePurgeResult(""); err == nil {
			t.Fatal("expected error for empty input")
		}
	})
}

// extractEncodedArgs pulls the base64 blob out of the script template's
// FromBase64String('...') literal.
func extractEncodedArgs(t *testing.T, script string) string {
	t.Helper()
	const prefix = "FromBase64String('"
	start := strings.Index(script, prefix)
	if start < 0 {
		t.Fatal("script missing FromBase64String literal")
	}
	start += len(prefix)
	end := strings.Index(script[start:], "'")
	if end < 0 {
		t.Fatal("script missing closing quote")
	}
	return script[start : start+end]
}

func TestBuildPurgeScript(t *testing.T) {
	t.Run("round-trip preserves args", func(t *testing.T) {
		in := purgeArgs{
			Apply:            true,
			SkipCreatorCheck: false,
			Classes:          []string{"user", "computer"},
			Allowlist: labconfig.Allowlist{
				Users:     []string{"alice", "bob"},
				Computers: []string{"DC01", "WS01"},
				Groups:    []string{"engineers"},
				Trusts:    []string{"NORTH$"},
			},
		}

		script, err := buildPurgeScript(in)
		if err != nil {
			t.Fatalf("buildPurgeScript: %v", err)
		}
		if !strings.Contains(script, purgeResultMarker) {
			t.Error("script does not contain result marker")
		}

		raw, err := base64.StdEncoding.DecodeString(extractEncodedArgs(t, script))
		if err != nil {
			t.Fatalf("decode base64: %v", err)
		}
		var got purgeArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !reflect.DeepEqual(got, in) {
			t.Errorf("round-trip mismatch:\n got=%+v\nwant=%+v", got, in)
		}
	})

	t.Run("encoded blob contains no single quotes (would break PowerShell literal)", func(t *testing.T) {
		// JSON inside base64 → only A–Z, a–z, 0–9, +, /, = appear. Guard
		// against a future change that swaps encoding and breaks the
		// `FromBase64String('...')` literal.
		script, err := buildPurgeScript(purgeArgs{Apply: true})
		if err != nil {
			t.Fatalf("buildPurgeScript: %v", err)
		}
		encoded := extractEncodedArgs(t, script)
		if strings.ContainsAny(encoded, "'\n\r") {
			t.Errorf("encoded blob contains forbidden chars: %q", encoded)
		}
	})

	t.Run("zero-value args round-trip cleanly", func(t *testing.T) {
		script, err := buildPurgeScript(purgeArgs{})
		if err != nil {
			t.Fatalf("buildPurgeScript: %v", err)
		}
		raw, err := base64.StdEncoding.DecodeString(extractEncodedArgs(t, script))
		if err != nil {
			t.Fatalf("decode base64: %v", err)
		}
		var got purgeArgs
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal zero-value: %v", err)
		}
		if got.Apply || got.SkipCreatorCheck || len(got.Classes) != 0 {
			t.Errorf("zero-value mutated by round-trip: %+v", got)
		}
	})
}
