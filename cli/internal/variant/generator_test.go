package variant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestFindCrackablePasswords(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "source")

	// Create scripts dir with an AS-REP roasting script targeting arya.stark
	scriptsDir := filepath.Join(sourceDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Target arya.stark AND sql_svc — sql_svc should still be skipped (preserved)
	if err := os.WriteFile(
		filepath.Join(scriptsDir, "asrep_roasting.ps1"),
		[]byte("Get-ADUser -Identity \"arya.stark\" | Set-ADAccountControl -DoesNotRequirePreAuth:$true\nGet-ADUser -Identity \"sql_svc\" | Set-ADAccountControl -DoesNotRequirePreAuth:$true"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	config := testConfig()

	// Give samwell.tarly SPNs — Kerberoastable
	config.Lab.Domains["sevenkingdoms.local"].Users["samwell.tarly"].SPNs = []string{
		"HTTP/eyrie.sevenkingdoms.local",
	}

	// Give sql_svc SPNs — should NOT be crackable (preserved)
	config.Lab.Domains["sevenkingdoms.local"].Users["sql_svc"].SPNs = []string{
		"MSSQLSvc/kingslanding.sevenkingdoms.local:1433",
	}

	gen := NewGenerator(sourceDir, "", "test-crackable")

	crackable := gen.findCrackablePasswords(config)

	// (1) samwell.tarly has SPNs → password must be crackable
	if !crackable["Heartsbane"] {
		t.Error("expected Heartsbane (samwell.tarly SPN user) to be crackable")
	}

	// (2) arya.stark is in asrep script → password must be crackable
	if !crackable["NeedleIsMySword!"] {
		t.Error("expected NeedleIsMySword! (arya.stark AS-REP user) to be crackable")
	}

	// (3) sql_svc is preserved → password must NOT be crackable (even via SPN or AS-REP)
	if crackable["SqlSvcPass1!"] {
		t.Error("sql_svc password should not be crackable (preserved user)")
	}

	// Domain password should not be crackable
	if crackable["DomainPass1!"] {
		t.Error("domain password should not be crackable")
	}
}

func TestApplyReplacements(t *testing.T) {
	gen := NewGenerator("", "", "test")
	gen.mappings.Misc["robert"] = "james"
	gen.replacements = []replacement{
		{"sevenkingdoms.local", "deltasystems.local"},
		{"robert", "james"},
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

func TestIsNameComponent(t *testing.T) {
	gen := NewGenerator("", "", "test")
	gen.mappings.Misc["robert"] = "james"
	gen.mappings.Misc["meereen$"] = "beacon$"
	gen.mappings.Misc["winterfell.domain"] = "cascade.domain"

	tests := []struct {
		name string
		want bool
	}{
		{"robert", true},
		{"meereen$", false},
		{"winterfell.domain", false},
		{"notinmisc", false},
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
