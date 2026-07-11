package variant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestSourceFull creates a test source with extensionless files, user files,
// and compound group name scripts to exercise all known edge cases.
func setupTestSourceFull(t *testing.T) (sourceDir, targetDir string) {
	t.Helper()
	sourceDir, targetDir = setupTestSource(t)

	// Extensionless inventory file (Bug 1)
	inventoryContent := `[all:vars]
; sevenkingdoms.local
ansible_user=administrator@sevenkingdoms.local
`
	if err := os.WriteFile(filepath.Join(sourceDir, "data", "inventory_disable_vagrant"), []byte(inventoryContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// File named after a user (Bug 2)
	if err := os.MkdirAll(filepath.Join(sourceDir, "files", "srv02", "all"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(sourceDir, "files", "srv02", "all", "arya.txt"),
		[]byte("Hey arya, here is your sword."),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	// Script with compound group name (Bug 3)
	gpoScript := `$gpo = "StarkWallpaper"
Set-GPO -Name "StarkWallpaper" -Target "DC=north,DC=sevenkingdoms,DC=local"
`
	if err := os.WriteFile(filepath.Join(sourceDir, "scripts", "gpo_abuse.ps1"), []byte(gpoScript), 0o644); err != nil {
		t.Fatal(err)
	}

	return sourceDir, targetDir
}

func setupTestSource(t *testing.T) (sourceDir, targetDir string) {
	t.Helper()
	tmpDir := t.TempDir()
	sourceDir = filepath.Join(tmpDir, "source")
	targetDir = filepath.Join(tmpDir, "target")

	if err := os.MkdirAll(filepath.Join(sourceDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}

	config := testConfig()
	configData, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(filepath.Join(sourceDir, "data", "config.json"), configData, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(sourceDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(sourceDir, "scripts", "test.ps1"),
		[]byte("# Connect to kingslanding.sevenkingdoms.local\n$dc = 'SEVENKINGDOMS\\arya.stark'\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	return sourceDir, targetDir
}

func testConfig() *LabConfig {
	config := &LabConfig{}
	config.Lab.Hosts = map[string]*HostConfig{
		"dc01": {
			Hostname:           "kingslanding",
			Type:               "dc",
			Domain:             "sevenkingdoms.local",
			LocalAdminPassword: "TestPass123!",
		},
		"dc03": {
			Hostname:           "meereen",
			Type:               "dc",
			Domain:             "essos.local",
			LocalAdminPassword: "TestPass456!",
		},
	}
	config.Lab.Domains = map[string]*DomainConfig{
		"sevenkingdoms.local": {
			DomainPassword: "DomainPass1!",
			Users: map[string]*UserConfig{
				"arya.stark": {
					Firstname: "arya",
					Surname:   "stark",
					Password:  "NeedleIsMySword!",
					City:      "Winterfell",
				},
				"samwell.tarly": {
					Firstname:   "samwell",
					Surname:     "tarly",
					Password:    "Heartsbane",
					Description: "Samwell Tarly (Password : Heartsbane)",
				},
				"sql_svc": {
					Firstname: "sql",
					Surname:   "-",
					Password:  "SqlSvcPass1!",
				},
			},
			Groups: GroupsConfig{
				Global: map[string]GroupConfig{
					"Stark":         {},
					"Domain Admins": {},
				},
			},
			OrganisationUnits: map[string]OUConfig{
				"Vale": {},
			},
			ACLs: map[string]ACLConfig{
				"GenericAll_arya_stark": {
					For:   "arya.stark",
					To:    "CN=SomeObject",
					Right: "GenericAll",
				},
			},
			GMSA: map[string]GMSAConfig{
				"gmsa1": {
					Name: "gmsaDragon",
				},
			},
		},
		"essos.local": {
			DomainPassword: "EssosPass1!",
			Users:          map[string]*UserConfig{},
		},
	}
	return config
}

func TestGeneratorEndToEnd(t *testing.T) {
	sourceDir, targetDir := setupTestSource(t)

	gen := NewGenerator(sourceDir, targetDir, "test-variant")
	if err := gen.Run(); err != nil {
		t.Fatalf("generator failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(targetDir, "data", "config.json")); err != nil {
		t.Fatal("config.json not created in target")
	}
	if _, err := os.Stat(filepath.Join(targetDir, "mapping.json")); err != nil {
		t.Fatal("mapping.json not created")
	}

	transformedData, err := os.ReadFile(filepath.Join(targetDir, "data", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(transformedData)

	for _, name := range []string{"sevenkingdoms", "essos", "kingslanding", "meereen", "arya", "stark"} {
		if strings.Contains(strings.ToLower(content), name) {
			t.Errorf("original name %q still found in transformed config", name)
		}
	}
	if !strings.Contains(content, "sql_svc") {
		t.Error("sql_svc should be preserved")
	}

	scriptData, err := os.ReadFile(filepath.Join(targetDir, "scripts", "test.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	scriptContent := string(scriptData)
	if strings.Contains(strings.ToLower(scriptContent), "kingslanding") {
		t.Error("original hostname found in transformed script")
	}
	if strings.Contains(strings.ToLower(scriptContent), "sevenkingdoms") {
		t.Error("original domain found in transformed script")
	}

	if _, err := os.Stat(filepath.Join(targetDir, "README.md")); err != nil {
		t.Fatal("README.md not created")
	}
}

func TestPasswordInDescriptionPreserved(t *testing.T) {
	sourceDir, targetDir := setupTestSource(t)

	gen := NewGenerator(sourceDir, targetDir, "test-pwd-desc")
	if err := gen.Run(); err != nil {
		t.Fatalf("generator failed: %v", err)
	}

	transformedData, err := os.ReadFile(filepath.Join(targetDir, "data", "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	var config LabConfig
	if err := json.Unmarshal(transformedData, &config); err != nil {
		t.Fatal(err)
	}

	// Find the transformed user that was samwell.tarly
	newUsername := gen.mappings.Users["samwell.tarly"]
	if newUsername == "" {
		t.Fatal("samwell.tarly not found in user mappings")
	}

	for _, domain := range config.Lab.Domains {
		if user, ok := domain.Users[newUsername]; ok {
			if !strings.Contains(user.Description, "(Password :") {
				t.Errorf("password-in-description pattern lost for %s: got %q", newUsername, user.Description)
			}
			if !strings.Contains(user.Description, user.Password) {
				t.Errorf("description should contain the new password for %s: desc=%q password=%q", newUsername, user.Description, user.Password)
			}
			return
		}
	}
	t.Errorf("transformed user %s not found in any domain", newUsername)
}

func TestApplyReplacements(t *testing.T) {
	gen := NewGenerator("", "", "test")
	gen.mappings.Misc["robert"] = "james"
	gen.replacements = []replacement{
		{Old: "sevenkingdoms.local", New: "deltasystems.local"},
		{Old: "robert", New: "james", WordBoundary: true},
	}

	content := "domain: sevenkingdoms.local, user: robert"
	result := gen.applyReplacements(content)

	if strings.Contains(result, "sevenkingdoms") {
		t.Error("sevenkingdoms not replaced")
	}
	if !strings.Contains(result, "deltasystems.local") {
		t.Error("deltasystems.local not present")
	}
}

func TestApplyReplacementsUnderscoreDelimited(t *testing.T) {
	gen := NewGenerator("", "", "test")
	gen.replacements = []replacement{
		{Old: "missandei", New: "donna", WordBoundary: true},
		{Old: "viserys", New: "alexander", WordBoundary: true},
	}

	content := `"GenericWrite_missandei_viserys": null`
	result := gen.applyReplacements(content)

	if strings.Contains(result, "missandei") {
		t.Errorf("missandei not replaced in underscore-delimited key: %s", result)
	}
	if strings.Contains(result, "viserys") {
		t.Errorf("viserys not replaced in underscore-delimited key: %s", result)
	}
	expected := `"GenericWrite_donna_alexander": null`
	if result != expected {
		t.Errorf("unexpected result:\n  got:  %s\n  want: %s", result, expected)
	}
}

func TestIsNameComponent(t *testing.T) {
	gen := NewGenerator("", "", "test")
	gen.mappings.Misc["robert"] = "james"
	gen.nameComponents["robert"] = true
	gen.mappings.Misc["meereen$"] = "beacon$"
	gen.mappings.Misc["winterfell.domain"] = "cascade.domain"
	// Group name that happens to also be a surname — should NOT be a name component
	// unless explicitly registered via mapUserNameComponents.
	gen.mappings.Misc["Stark"] = "OperationsGroup"

	tests := []struct {
		name string
		want bool
	}{
		{"robert", true},
		{"meereen$", false},
		{"winterfell.domain", false},
		{"notinmisc", false},
		{"Stark", false}, // group name, not registered as name component
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := gen.isNameComponent(tt.name)
			if got != tt.want {
				t.Errorf("isNameComponent(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestCapitalize(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "Hello"},
		{"HELLO", "Hello"},
		{"", ""},
		{"a", "A"},
	}

	for _, tt := range tests {
		if got := capitalize(tt.input); got != tt.want {
			t.Errorf("capitalize(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSimplifyEntity(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`DOMAIN\user`, "user"},
		{`CN=SomeObject,OU=Test`, "someobject"},
		{`admin`, "admin"},
	}

	for _, tt := range tests {
		got := simplifyEntity(tt.input)
		if got != tt.want {
			t.Errorf("simplifyEntity(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTransformEdgeCases(t *testing.T) {
	sourceDir, targetDir := setupTestSourceFull(t)

	gen := NewGenerator(sourceDir, targetDir, "test-edges")
	if err := gen.Run(); err != nil {
		t.Fatalf("generator failed: %v", err)
	}

	// Bug 1: extensionless files should be detected as text and transformed.
	t.Run("extensionless_file", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(targetDir, "data", "inventory_disable_vagrant"))
		if err != nil {
			t.Fatal("inventory_disable_vagrant not created in target")
		}
		if strings.Contains(strings.ToLower(string(data)), "sevenkingdoms") {
			t.Error("original domain 'sevenkingdoms' still found in extensionless inventory file")
		}
	})

	// Bug 2: files named after entities should be renamed on disk.
	t.Run("file_renamed", func(t *testing.T) {
		oldPath := filepath.Join(targetDir, "files", "srv02", "all", "arya.txt")
		if _, err := os.Stat(oldPath); err == nil {
			t.Error("arya.txt still exists at original path — file was not renamed")
		}
		newFirstname := gen.mappings.Misc["arya"]
		if newFirstname == "" {
			t.Fatal("no mapping found for 'arya' in Misc")
		}
		newPath := filepath.Join(targetDir, "files", "srv02", "all", newFirstname+".txt")
		if _, err := os.Stat(newPath); err != nil {
			t.Errorf("renamed file %s.txt not found at expected path", newFirstname)
		}
	})

	// Bug 3: group names in compound strings (e.g., "StarkWallpaper") should be replaced.
	t.Run("compound_group_name", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(targetDir, "scripts", "gpo_abuse.ps1"))
		if err != nil {
			t.Fatal("gpo_abuse.ps1 not created in target")
		}
		if strings.Contains(string(data), "Stark") {
			t.Errorf("original group name 'Stark' still found in gpo_abuse.ps1: %s", string(data))
		}
	})
}

func TestGroupSurnameMappingConsistency(t *testing.T) {
	sourceDir, targetDir := setupTestSourceFull(t)

	gen := NewGenerator(sourceDir, targetDir, "test-group-surname")
	if err := gen.Run(); err != nil {
		t.Fatalf("generator failed: %v", err)
	}

	// "Stark" is both a group name and a capitalized surname (from arya.stark).
	// The group mapping should win — Misc should not have a conflicting entry.
	groupNew := gen.mappings.Groups["Stark"]
	if groupNew == "" {
		t.Fatal("group 'Stark' not found in mappings")
	}
	if miscNew, exists := gen.mappings.Misc["Stark"]; exists {
		t.Errorf("Misc still has 'Stark' -> %q, should have been removed in favor of group mapping %q", miscNew, groupNew)
	}

	// Verify the config.json actually uses the group name, not the surname.
	varData, err := os.ReadFile(filepath.Join(targetDir, "data", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(varData), groupNew) {
		t.Errorf("group name %q not found in variant config.json", groupNew)
	}
}

func TestFirstnameCollisionNoOverwrite(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")
	targetDir := filepath.Join(tmpDir, "target")

	if err := os.MkdirAll(filepath.Join(sourceDir, "data"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Two users with the same firstname in the same domain.
	config := &LabConfig{}
	config.Lab.Hosts = map[string]*HostConfig{
		"dc01": {
			Hostname:           "kingslanding",
			Type:               "dc",
			Domain:             "sevenkingdoms.local",
			LocalAdminPassword: "TestPass123!",
		},
	}
	config.Lab.Domains = map[string]*DomainConfig{
		"sevenkingdoms.local": {
			DomainPassword: "DomainPass1!",
			Users: map[string]*UserConfig{
				"brandon.stark": {
					Firstname: "brandon",
					Surname:   "stark",
					Password:  "BranPass1!",
				},
				"brandon.lannister": {
					Firstname: "brandon",
					Surname:   "lannister",
					Password:  "BranPass2!",
				},
			},
			Groups: GroupsConfig{},
		},
	}
	configData, _ := json.MarshalIndent(config, "", "  ")
	if err := os.WriteFile(filepath.Join(sourceDir, "data", "config.json"), configData, 0o644); err != nil {
		t.Fatal(err)
	}

	gen := NewGenerator(sourceDir, targetDir, "test-collision")
	if err := gen.Run(); err != nil {
		t.Fatalf("generator failed: %v", err)
	}

	// Both users should have distinct mappings.
	user1 := gen.mappings.Users["brandon.stark"]
	user2 := gen.mappings.Users["brandon.lannister"]
	if user1 == "" || user2 == "" {
		t.Fatal("one or both users not mapped")
	}
	if user1 == user2 {
		t.Errorf("both users mapped to same username: %s", user1)
	}

	// The Misc entry for "brandon" should exist and not be empty.
	miscBrandon := gen.mappings.Misc["brandon"]
	if miscBrandon == "" {
		t.Error("Misc mapping for 'brandon' is empty")
	}

	// Verify both users exist in the output config with correct firstname/surname.
	varData, err := os.ReadFile(filepath.Join(targetDir, "data", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var varConfig LabConfig
	if err := json.Unmarshal(varData, &varConfig); err != nil {
		t.Fatal(err)
	}
	for domainName, domain := range varConfig.Lab.Domains {
		for username, user := range domain.Users {
			if strings.Contains(username, ".") {
				parts := strings.SplitN(username, ".", 2)
				if user.Firstname != parts[0] {
					t.Errorf("domain %s user %s: firstname=%q doesn't match username prefix %q",
						domainName, username, user.Firstname, parts[0])
				}
			}
		}
	}
}
