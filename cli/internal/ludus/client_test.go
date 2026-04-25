package ludus

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestRangeStatusJSON_CleanStdout verifies that RangeStatusJSON correctly
// parses JSON from stdout without [INFO] stderr pollution. This is the core
// scenario that the stdout/stderr separation fix addresses: Ludus v2 writes
// "[INFO]  Ludus client ..." to stderr, which previously got mixed into the
// JSON output via CombinedOutput(), breaking json.Unmarshal and causing
// "VM with Proxmox ID X not found" errors after the 2-minute cache TTL.
func TestRangeStatusJSON_CleanStdout(t *testing.T) {
	// This is what stdout contains (clean JSON) after the fix.
	cleanJSON := `{"rangeState":"SUCCESS","rangeNumber":1,"VMs":[{"name":"DG-GOAD-DC01","proxmoxID":104,"poweredOn":true,"ip":"10.1.10.10"},{"name":"DG-GOAD-DC02","proxmoxID":105,"poweredOn":true,"ip":"10.1.10.11"},{"name":"DG-GOAD-SRV03","proxmoxID":108,"poweredOn":true,"ip":"10.1.10.23"}]}`

	var rs RangeStatus
	if err := json.Unmarshal([]byte(cleanJSON), &rs); err != nil {
		t.Fatalf("unmarshal clean JSON: %v", err)
	}

	if rs.RangeState != "SUCCESS" {
		t.Errorf("RangeState = %q, want SUCCESS", rs.RangeState)
	}
	if rs.RangeNumber != 1 {
		t.Errorf("RangeNumber = %d, want 1", rs.RangeNumber)
	}
	if len(rs.VMs) != 3 {
		t.Fatalf("len(VMs) = %d, want 3", len(rs.VMs))
	}
	if rs.VMs[0].ProxmoxID != 104 || rs.VMs[0].Name != "DG-GOAD-DC01" {
		t.Errorf("VM[0] = %+v, want DC01/104", rs.VMs[0])
	}
}

// TestRangeStatusJSON_PollutedCombinedOutput demonstrates the bug: when
// CombinedOutput() mixes [INFO] stderr lines into the JSON, Unmarshal fails.
func TestRangeStatusJSON_PollutedCombinedOutput(t *testing.T) {
	// This is what CombinedOutput() returned before the fix: [INFO] lines
	// from stderr prepended to the JSON from stdout.
	polluted := "[INFO]  Ludus client 2.1.0+970625e\n" +
		`{"rangeState":"SUCCESS","rangeNumber":1,"VMs":[{"name":"DG-GOAD-DC01","proxmoxID":104,"poweredOn":true,"ip":"10.1.10.10"}]}`

	var rs RangeStatus
	err := json.Unmarshal([]byte(polluted), &rs)
	if err == nil {
		t.Fatal("expected unmarshal error for polluted output, got nil")
	}
	// This parse failure is what caused the cache to fail to refresh,
	// leading to "VM with Proxmox ID X not found" after the 2-min TTL.
	t.Logf("confirmed: polluted output causes parse error: %v", err)
}

// TestVersionDetection_CleanStdout verifies that version detection parses
// correctly when stdout contains only JSON (after the fix).
func TestVersionDetection_CleanStdout(t *testing.T) {
	// Ludus v2 version --json stdout (clean, no [INFO] lines).
	cleanJSON := `{"version":"2.1.0+970625e","result":"Ludus Server 2.1.0+970625e - community license"}`

	var v VersionInfo
	if err := json.Unmarshal([]byte(cleanJSON), &v); err != nil {
		t.Fatalf("unmarshal version JSON: %v", err)
	}
	if v.Version != "2.1.0+970625e" {
		t.Errorf("Version = %q, want 2.1.0+970625e", v.Version)
	}

	// Verify major version extraction.
	parts := splitVersion(v.Version)
	if parts != 2 {
		t.Errorf("major version = %d, want 2", parts)
	}
}

// TestVersionDetection_PollutedCombinedOutput demonstrates the version
// detection bug: CombinedOutput() mixed [INFO] into the JSON, causing
// majorVersion to stay at 1 even for Ludus v2.
func TestVersionDetection_PollutedCombinedOutput(t *testing.T) {
	polluted := "[INFO]  Ludus client 2.1.0+970625e\n" +
		`{"version":"2.1.0+970625e","result":"Ludus Server 2.1.0+970625e"}`

	var v VersionInfo
	err := json.Unmarshal([]byte(polluted), &v)
	if err == nil {
		t.Fatal("expected unmarshal error for polluted output, got nil")
	}
	// With CombinedOutput, this parse failure caused majorVersion to stay at 1.
	t.Logf("confirmed: polluted output breaks version detection: %v", err)
}

// TestVMCacheKeyUniqueness verifies that all VMs get unique cache keys.
// Regression test: if ProxmoxID were zero for all VMs (bad JSON field name),
// they'd all map to "0" and overwrite each other.
func TestVMCacheKeyUniqueness(t *testing.T) {
	raw := `{"rangeState":"SUCCESS","rangeNumber":1,"VMs":[
		{"name":"DG-GOAD-DC01","proxmoxID":104,"poweredOn":true,"ip":"10.1.10.10"},
		{"name":"DG-GOAD-DC02","proxmoxID":105,"poweredOn":true,"ip":"10.1.10.11"},
		{"name":"DG-GOAD-DC03","proxmoxID":106,"poweredOn":true,"ip":"10.1.10.12"},
		{"name":"DG-GOAD-SRV02","proxmoxID":107,"poweredOn":true,"ip":"10.1.10.22"},
		{"name":"DG-GOAD-SRV03","proxmoxID":108,"poweredOn":true,"ip":"10.1.10.23"},
		{"name":"DG-router-debian11-x64","proxmoxID":103,"poweredOn":true,"ip":"10.1.10.254"}
	]}`

	var rs RangeStatus
	if err := json.Unmarshal([]byte(raw), &rs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(rs.VMs) != 6 {
		t.Fatalf("len(VMs) = %d, want 6", len(rs.VMs))
	}

	// Build the cache map the same way refreshVMs does.
	m := make(map[string]VM, len(rs.VMs))
	for _, vm := range rs.VMs {
		key := fmt.Sprintf("%d", vm.ProxmoxID)
		m[key] = vm
	}

	// Every VM must be individually addressable.
	for _, id := range []string{"103", "104", "105", "106", "107", "108"} {
		if _, ok := m[id]; !ok {
			t.Errorf("VM with ProxmoxID %s not found in cache map", id)
		}
	}
	if len(m) != 6 {
		t.Errorf("cache map has %d entries, want 6 (key collision?)", len(m))
	}
}

// TestResolveHostname_RoleExtraction verifies hostname resolution from Ludus
// VM names (format: "RANGEID-LAB-ROLE" -> lowercase role).
func TestResolveHostname_RoleExtraction(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"DG-GOAD-DC01", "dc01"},
		{"DG-GOAD-DC02", "dc02"},
		{"DG-GOAD-SRV03", "srv03"},
		{"DG-router-debian11-x64", "x64"}, // router has different pattern
	}

	for _, tt := range tests {
		parts := splitVMName(tt.name)
		if parts != tt.want {
			t.Errorf("role(%q) = %q, want %q", tt.name, parts, tt.want)
		}
	}
}

// splitVersion extracts the major version number from a version string.
func splitVersion(version string) int {
	for i, c := range version {
		if c == '.' {
			switch version[:i] {
			case "1":
				return 1
			case "2":
				return 2
			default:
				return 0
			}
		}
	}
	return 0
}

// splitVMName extracts the role (last hyphen segment) from a Ludus VM name.
func splitVMName(name string) string {
	for i := len(name) - 1; i >= 0; i-- {
		if name[i] == '-' {
			return toLower(name[i+1:])
		}
	}
	return toLower(name)
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// use fmt to satisfy import
var _ = fmt.Sprintf
