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

func testConfig() map[string]any {
	return map[string]any{
		"lab": map[string]any{
			"hosts": map[string]any{
				"dc01": map[string]any{
					"hostname":             "kingslanding",
					"type":                 "dc",
					"domain":               "sevenkingdoms.local",
					"local_admin_password": "TestPass123!",
				},
				"dc03": map[string]any{
					"hostname":             "meereen",
					"type":                 "dc",
					"domain":               "essos.local",
					"local_admin_password": "TestPass456!",
				},
			},
			"domains": map[string]any{
				"sevenkingdoms.local": map[string]any{
					"domain_password": "DomainPass1!",
					"users": map[string]any{
						"arya.stark": map[string]any{
							"firstname": "arya",
							"surname":   "stark",
							"password":  "NeedleIsMySword!",
							"city":      "Winterfell",
						},
						"sql_svc": map[string]any{
							"firstname": "sql",
							"surname":   "-",
							"password":  "SqlSvcPass1!",
						},
					},
					"groups": map[string]any{
						"global": map[string]any{
							"Stark":         map[string]any{},
							"Domain Admins": map[string]any{},
						},
					},
					"organisation_units": map[string]any{
						"Vale": map[string]any{},
					},
					"acls": map[string]any{
						"GenericAll_arya_stark": map[string]any{
							"for":   "arya.stark",
							"to":    "CN=SomeObject",
							"right": "GenericAll",
						},
					},
					"gmsa": map[string]any{
						"gmsa1": map[string]any{
							"gMSA_Name": "gmsaDragon",
						},
					},
				},
				"essos.local": map[string]any{
					"domain_password": "EssosPass1!",
					"users":           map[string]any{},
					"groups":          map[string]any{},
				},
			},
		},
	}
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
