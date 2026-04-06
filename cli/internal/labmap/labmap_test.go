package labmap

import (
	"path/filepath"
	"runtime"
	"testing"
)

func projectRoot() string {
	_, f, _, _ := runtime.Caller(0)
	// cli/internal/labmap/labmap_test.go -> project root is 4 levels up
	return filepath.Join(filepath.Dir(f), "..", "..", "..")
}

func loadLab(t *testing.T, name string) *LabMap {
	t.Helper()
	root := projectRoot()
	lab, err := LoadFromSource(filepath.Join(root, "ad", name))
	if err != nil {
		t.Fatalf("LoadFromSource %s: %v", name, err)
	}
	return lab
}

func TestGOADTopology(t *testing.T) {
	lab := loadLab(t, "GOAD")

	assertLen(t, "domains", len(lab.DomainConfigs), 3)
	assertLen(t, "hosts", len(lab.HostConfigs), 5)
	assertLen(t, "DCs", len(lab.DCs()), 3)
	assertLen(t, "WindowsServers", len(lab.WindowsServers()), 2)
	assertLen(t, "trusts", len(lab.DomainTrusts()), 1)
}

func TestGOADVulns(t *testing.T) {
	lab := loadLab(t, "GOAD")

	assertNonEmpty(t, "UsersWithSPNs", len(lab.UsersWithSPNs()))
	assertNonEmpty(t, "UsersWithPasswordInDescription", len(lab.UsersWithPasswordInDescription()))
	assertLen(t, "MSSQL hosts", len(lab.HostsWithMSSQL()), 2)
	assertNonEmpty(t, "ADCS hosts", len(lab.ADCSHosts()))
	assertNonEmpty(t, "asrep_roasting hosts", len(lab.HostsWithScript("asrep_roasting")))
	assertNonEmpty(t, "constrained_delegation hosts", len(lab.HostsWithScript("constrained_delegation")))
	assertNonEmpty(t, "ACLs", len(lab.AllACLs()))
}

func TestGOADPasswordInDescription(t *testing.T) {
	lab := loadLab(t, "GOAD")

	pwdUsers := lab.UsersWithPasswordInDescription()
	found := false
	for _, u := range pwdUsers {
		if u.Username == "samwell.tarly" {
			found = true
		}
	}
	if !found {
		t.Error("expected samwell.tarly in password-in-description users")
	}
}

func TestGOADDomains(t *testing.T) {
	lab := loadLab(t, "GOAD")

	assertNotEmpty(t, "RootDomain", lab.RootDomain)
	assertNotEmpty(t, "ChildDomain", lab.ChildDomain)
	assertNotEmpty(t, "ForestDomain", lab.ForestDomain)
}

func TestDRACARYSTopology(t *testing.T) {
	lab := loadLab(t, "DRACARYS")

	assertLen(t, "domains", len(lab.DomainConfigs), 1)
	assertLen(t, "hosts", len(lab.HostConfigs), 3)
	assertLen(t, "DCs", len(lab.DCs()), 1)
	assertLen(t, "WindowsServers", len(lab.WindowsServers()), 1)
	assertLen(t, "trusts", len(lab.DomainTrusts()), 0)
	assertLen(t, "MSSQL hosts", len(lab.HostsWithMSSQL()), 0)
	assertLen(t, "SPN users", len(lab.UsersWithSPNs()), 0)
}

func TestDRACARYSDomains(t *testing.T) {
	lab := loadLab(t, "DRACARYS")

	assertNotEmpty(t, "RootDomain", lab.RootDomain)
	if lab.ChildDomain != "" {
		t.Errorf("ChildDomain should be empty, got %s", lab.ChildDomain)
	}
	if lab.ForestDomain != "" {
		t.Errorf("ForestDomain should be empty, got %s", lab.ForestDomain)
	}
}

func TestNHATopology(t *testing.T) {
	lab := loadLab(t, "NHA")

	assertLen(t, "domains", len(lab.DomainConfigs), 2)
	assertLen(t, "hosts", len(lab.HostConfigs), 5)
	assertLen(t, "DCs", len(lab.DCs()), 2)
	assertLen(t, "WindowsServers", len(lab.WindowsServers()), 3)
	assertLen(t, "trusts", len(lab.DomainTrusts()), 1)
	assertLen(t, "SPN users", len(lab.UsersWithSPNs()), 2)
	assertLen(t, "MSSQL hosts", len(lab.HostsWithMSSQL()), 1)
}

func TestNHADomains(t *testing.T) {
	lab := loadLab(t, "NHA")

	assertNotEmpty(t, "RootDomain", lab.RootDomain)
	assertNotEmpty(t, "ChildDomain", lab.ChildDomain)
}

func TestHostRolesNeverEmpty(t *testing.T) {
	labs := []string{"GOAD", "DRACARYS", "NHA", "GOAD-Light", "GOAD-Mini"}

	for _, name := range labs {
		t.Run(name, func(t *testing.T) {
			lab := loadLab(t, name)
			assertNonEmpty(t, "HostRoles", len(lab.HostRoles()))
			assertNonEmpty(t, "DCs", len(lab.DCs()))
			assertNonEmpty(t, "Domains", len(lab.Domains()))
		})
	}
}

func assertLen(t *testing.T, what string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s: expected %d, got %d", what, want, got)
	}
}

func assertNonEmpty(t *testing.T, what string, got int) {
	t.Helper()
	if got == 0 {
		t.Errorf("%s: expected non-empty", what)
	}
}

func assertNotEmpty(t *testing.T, what, val string) {
	t.Helper()
	if val == "" {
		t.Errorf("%s should not be empty", what)
	}
}
