package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/inventory"
)

func TestResolveRegion(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		want    string
		wantErr string // substring the error must contain; empty means success expected
	}{
		{
			name: "returns configured region",
			cfg:  &Config{Region: "eu-west-1"},
			want: "eu-west-1",
		},
		{
			name:    "errors when region is empty",
			cfg:     &Config{Region: ""},
			wantErr: "region",
		},
		{
			name: "prefers the active environment's region over the global one",
			cfg: &Config{
				Env:    "staging",
				Region: "us-east-1",
				Environments: map[string]EnvironmentConfig{
					"staging": {Region: "us-west-1"},
					"prod":    {Region: "us-east-1"},
				},
			},
			want: "us-west-1",
		},
		{
			name: "falls back to the global region when the environment declares none",
			cfg: &Config{
				Env:          "dev",
				Region:       "us-east-1",
				Environments: map[string]EnvironmentConfig{"dev": {VpcCidr: "10.0.0.0/16"}},
			},
			want: "us-east-1",
		},
		{
			name: "falls back to the global region for an undefined environment",
			cfg:  &Config{Env: "nope", Region: "us-east-1"},
			want: "us-east-1",
		},
		{
			name: "an explicit override outranks the environment region",
			cfg: &Config{
				Env:            "staging",
				Region:         "us-east-1",
				regionOverride: "eu-west-1",
				Environments:   map[string]EnvironmentConfig{"staging": {Region: "us-west-1"}},
			},
			want: "eu-west-1",
		},
		{
			name: "the environment region satisfies an empty global region",
			cfg: &Config{
				Env:          "staging",
				Environments: map[string]EnvironmentConfig{"staging": {Region: "us-west-1"}},
			},
			want: "us-west-1",
		},
		{
			name:    "error names the environment key to set",
			cfg:     &Config{Env: "staging"},
			wantErr: "environments.staging.region",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ResolveRegion()
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected an error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ResolveRegion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolvedLabPrecedence(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{name: "default", cfg: &Config{}, want: "GOAD"},
		{name: "top level", cfg: &Config{Lab: "GOAD-Light"}, want: "GOAD-Light"},
		{
			name: "environment override",
			cfg: &Config{
				Env: "scope-dev",
				Lab: "GOAD",
				Environments: map[string]EnvironmentConfig{
					"scope-dev": {Lab: "SCOPE-RANGE"},
				},
			},
			want: "SCOPE-RANGE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolvedLab(); got != tt.want {
				t.Fatalf("ResolvedLab() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateLabName(t *testing.T) {
	for _, valid := range []string{"GOAD", "GOAD-Light", "SCOPE_RANGE", "lab123"} {
		if err := ValidateLabName(valid); err != nil {
			t.Errorf("ValidateLabName(%q) unexpected error: %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "../GOAD", "GOAD/Light", ".", "scope range", "GOAD.Light"} {
		if err := ValidateLabName(invalid); err == nil {
			t.Errorf("ValidateLabName(%q) = nil, want error", invalid)
		}
	}
}

func TestEnvironmentOverridesProviderAndDeployment(t *testing.T) {
	cfg := &Config{
		Env:      "scope-dev",
		Provider: "aws",
		Infra:    InfraConfig{Deployment: "goad-deployment"},
		Environments: map[string]EnvironmentConfig{
			"scope-dev": {
				Provider:   "azure",
				Deployment: "scope-range-deployment",
			},
		},
	}

	if got := cfg.ResolvedProvider(); got != "azure" {
		t.Errorf("ResolvedProvider() = %q, want azure", got)
	}
	if got := cfg.ResolvedDeployment(); got != "scope-range-deployment" {
		t.Errorf("ResolvedDeployment() = %q, want scope-range-deployment", got)
	}
	want := filepath.Join(cfg.ProjectRoot, "infra", "azure", "scope-range-deployment")
	if got := cfg.InfraBasePathForProvider("azure"); got != want {
		t.Errorf("InfraBasePathForProvider(azure) = %q, want %q", got, want)
	}
}

func TestLabConfigDataDirUsesActiveLab(t *testing.T) {
	cfg := &Config{ProjectRoot: "/repo", Lab: "SCOPE-RANGE"}
	want := filepath.Join("/repo", "ad", "SCOPE-RANGE", "data")
	if got := cfg.labConfigDataDir(); got != want {
		t.Errorf("labConfigDataDir() = %q, want %q", got, want)
	}
}

func TestGetRegionOverridePrecedence(t *testing.T) {
	t.Run("an explicit --region beats DREADGOAD_REGION", func(t *testing.T) {
		Reset()
		defer Reset()
		t.Setenv("DREADGOAD_REGION", "ap-southeast-1")
		SetRegionOverride("eu-west-1")

		c, err := Get()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.regionOverride != "eu-west-1" {
			t.Errorf("regionOverride = %q, want the flag value %q", c.regionOverride, "eu-west-1")
		}
	})

	t.Run("DREADGOAD_REGION applies when no flag is given", func(t *testing.T) {
		Reset()
		defer Reset()
		t.Setenv("DREADGOAD_REGION", "ap-southeast-1")

		c, err := Get()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.regionOverride != "ap-southeast-1" {
			t.Errorf("regionOverride = %q, want the env value %q", c.regionOverride, "ap-southeast-1")
		}
	})
}

func TestResolveRegionWithInventory(t *testing.T) {
	t.Run("prefers inventory region", func(t *testing.T) {
		c := &Config{Region: "us-west-1"}
		inv := &inventory.Inventory{
			Vars: map[string]string{"ansible_aws_ssm_region": "ap-southeast-1"},
		}
		got, err := c.ResolveRegionWithInventory(inv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "ap-southeast-1" {
			t.Errorf("ResolveRegionWithInventory() = %q, want %q", got, "ap-southeast-1")
		}
	})

	t.Run("falls back to config when inventory has no region", func(t *testing.T) {
		c := &Config{Region: "us-east-2"}
		inv := &inventory.Inventory{Vars: map[string]string{}}
		got, err := c.ResolveRegionWithInventory(inv)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "us-east-2" {
			t.Errorf("ResolveRegionWithInventory() = %q, want %q", got, "us-east-2")
		}
	})

	t.Run("falls back to config when inventory is nil", func(t *testing.T) {
		c := &Config{Region: "eu-central-1"}
		got, err := c.ResolveRegionWithInventory(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "eu-central-1" {
			t.Errorf("ResolveRegionWithInventory() = %q, want %q", got, "eu-central-1")
		}
	})

	t.Run("errors when both inventory and config have no region", func(t *testing.T) {
		c := &Config{Region: ""}
		inv := &inventory.Inventory{Vars: map[string]string{}}
		_, err := c.ResolveRegionWithInventory(inv)
		if err == nil {
			t.Fatal("expected error when no region available, got nil")
		}
	})

	t.Run("errors when nil inventory and config has no region", func(t *testing.T) {
		c := &Config{Region: ""}
		_, err := c.ResolveRegionWithInventory(nil)
		if err == nil {
			t.Fatal("expected error when no region available, got nil")
		}
	})
}

func TestConfigInventoryPath(t *testing.T) {
	c := &Config{ProjectRoot: "/opt/goad", Env: "dev"}
	got := c.InventoryPath()
	want := filepath.Join("/opt/goad", "dev-inventory")
	if got != want {
		t.Errorf("InventoryPath() = %q, want %q", got, want)
	}
}

func TestConfigAnsibleCfgPath(t *testing.T) {
	c := &Config{ProjectRoot: "/opt/goad"}
	got := c.AnsibleCfgPath()
	want := filepath.Join("/opt/goad", "ansible", "ansible.cfg")
	if got != want {
		t.Errorf("AnsibleCfgPath() = %q, want %q", got, want)
	}
}

func TestConfigAnsibleEnv(t *testing.T) {
	c := &Config{ProjectRoot: "/opt/goad", Env: "staging"}

	env, err := c.AnsibleEnv()
	if err != nil {
		t.Fatalf("AnsibleEnv() returned unexpected error: %v", err)
	}

	if env["ANSIBLE_CONFIG"] != c.AnsibleCfgPath() {
		t.Errorf("ANSIBLE_CONFIG = %q, want %q", env["ANSIBLE_CONFIG"], c.AnsibleCfgPath())
	}
	if env["ANSIBLE_HOST_KEY_CHECKING"] != "False" {
		t.Errorf("ANSIBLE_HOST_KEY_CHECKING = %q, want %q", env["ANSIBLE_HOST_KEY_CHECKING"], "False")
	}
	if env["ANSIBLE_RETRY_FILES_ENABLED"] != "True" {
		t.Errorf("ANSIBLE_RETRY_FILES_ENABLED = %q, want %q", env["ANSIBLE_RETRY_FILES_ENABLED"], "True")
	}
	if env["ANSIBLE_GATHER_TIMEOUT"] != "60" {
		t.Errorf("ANSIBLE_GATHER_TIMEOUT = %q, want %q", env["ANSIBLE_GATHER_TIMEOUT"], "60")
	}

	cacheConn := env["ANSIBLE_CACHE_PLUGIN_CONNECTION"]
	if !strings.Contains(cacheConn, "staging_dreadgoad_facts") {
		t.Errorf("ANSIBLE_CACHE_PLUGIN_CONNECTION = %q, want to contain %q", cacheConn, "staging_dreadgoad_facts")
	}
}

func TestLabConfigPath(t *testing.T) {
	t.Run("non-variant returns base GOAD path", func(t *testing.T) {
		c := &Config{ProjectRoot: "/opt/goad", Env: "staging"}
		got := c.LabConfigPath()
		want := filepath.Join("/opt/goad", "ad", "GOAD", "data", "staging-config.json")
		if got != want {
			t.Errorf("LabConfigPath() = %q, want %q", got, want)
		}
	})

	t.Run("variant returns variant path when config exists", func(t *testing.T) {
		dir := resolveSymlinks(t, t.TempDir())
		variantData := filepath.Join(dir, "ad", "GOAD-variant-1", "data")
		if err := os.MkdirAll(variantData, 0o755); err != nil {
			t.Fatal(err)
		}
		configFile := filepath.Join(variantData, "dev-config.json")
		if err := os.WriteFile(configFile, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}

		c := &Config{
			ProjectRoot: dir,
			Env:         "dev",
			Environments: map[string]EnvironmentConfig{
				"dev": {Variant: true, VariantTarget: "ad/GOAD-variant-1"},
			},
		}
		got := c.LabConfigPath()
		if got != configFile {
			t.Errorf("LabConfigPath() = %q, want %q", got, configFile)
		}
	})

	t.Run("variant falls back to base when variant config missing", func(t *testing.T) {
		dir := resolveSymlinks(t, t.TempDir())
		c := &Config{
			ProjectRoot: dir,
			Env:         "dev",
			Environments: map[string]EnvironmentConfig{
				"dev": {Variant: true, VariantTarget: "ad/GOAD-variant-1"},
			},
		}
		got := c.LabConfigPath()
		want := filepath.Join(dir, "ad", "GOAD", "data", "dev-config.json")
		if got != want {
			t.Errorf("LabConfigPath() = %q, want %q", got, want)
		}
	})
}

func TestResolvedLabConfigPathMarksMissingConfig(t *testing.T) {
	c := &Config{ProjectRoot: resolveSymlinks(t, t.TempDir()), Env: "dev"}

	_, err := c.ResolvedLabConfigPath()
	if !errors.Is(err, ErrLabConfigNotFound) {
		t.Fatalf("ResolvedLabConfigPath() error = %v, want ErrLabConfigNotFound", err)
	}
}

func TestConfigInstanceProfile(t *testing.T) {
	t.Run("field accessible on struct", func(t *testing.T) {
		c := &Config{InstanceProfile: "WarpgateImageBuilderInstanceProfile"}
		if c.InstanceProfile != "WarpgateImageBuilderInstanceProfile" {
			t.Errorf("InstanceProfile = %q, want %q", c.InstanceProfile, "WarpgateImageBuilderInstanceProfile")
		}
	})

	t.Run("zero value is empty string", func(t *testing.T) {
		c := &Config{}
		if c.InstanceProfile != "" {
			t.Errorf("InstanceProfile = %q, want empty", c.InstanceProfile)
		}
	})

	t.Run("mapstructure tag is instance_profile", func(t *testing.T) {
		// Verify the struct tag so viper unmarshals the YAML key correctly.
		var c Config
		typ := reflect.TypeOf(c)
		f, ok := typ.FieldByName("InstanceProfile")
		if !ok {
			t.Fatal("Config struct missing InstanceProfile field")
		}
		tag := f.Tag.Get("mapstructure")
		if tag != "instance_profile" {
			t.Errorf("mapstructure tag = %q, want %q", tag, "instance_profile")
		}
	})
}

func TestConfigInventoryPathDifferentEnvs(t *testing.T) {
	tests := []struct {
		env      string
		wantSufx string
	}{
		{"dev", "dev-inventory"},
		{"staging", "staging-inventory"},
		{"prod", "prod-inventory"},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			c := &Config{ProjectRoot: "/opt/goad", Env: tt.env}
			got := c.InventoryPath()
			if !strings.HasSuffix(got, tt.wantSufx) {
				t.Errorf("InventoryPath() = %q, want suffix %q", got, tt.wantSufx)
			}
		})
	}
}

func TestDefaultPlaybooks(t *testing.T) {
	if len(DefaultPlaybooks) == 0 {
		t.Fatal("DefaultPlaybooks is empty")
	}

	if DefaultPlaybooks[0] != "network_setup.yml" {
		t.Errorf("first playbook = %q, want %q", DefaultPlaybooks[0], "network_setup.yml")
	}

	last := DefaultPlaybooks[len(DefaultPlaybooks)-1]
	if last != "vulnerabilities.yml" {
		t.Errorf("last playbook = %q, want %q", last, "vulnerabilities.yml")
	}

	for _, p := range DefaultPlaybooks {
		if !strings.HasSuffix(p, ".yml") {
			t.Errorf("playbook %q does not end in .yml", p)
		}
	}
}

func TestRebootPlaybooks(t *testing.T) {
	if len(RebootPlaybooks) == 0 {
		t.Fatal("RebootPlaybooks is empty")
	}

	defaultSet := make(map[string]bool)
	for _, p := range DefaultPlaybooks {
		defaultSet[p] = true
	}
	for _, p := range RebootPlaybooks {
		if !defaultSet[p] {
			t.Errorf("RebootPlaybook %q not in DefaultPlaybooks", p)
		}
	}
}

// resolveSymlinks resolves symlinks so paths are comparable on macOS
// where TempDir returns /var/... but os.Getwd returns /private/var/...
func resolveSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestFindProjectRoot(t *testing.T) {
	t.Run("finds ansible directory", func(t *testing.T) {
		dir := resolveSymlinks(t, t.TempDir())
		ansibleDir := filepath.Join(dir, "ansible")
		if err := os.Mkdir(ansibleDir, 0o755); err != nil {
			t.Fatal(err)
		}

		subDir := filepath.Join(dir, "sub", "deep")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}

		origDir, _ := os.Getwd()
		if err := os.Chdir(subDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		got, err := findProjectRoot()
		if err != nil {
			t.Fatalf("findProjectRoot() returned unexpected error: %v", err)
		}
		if got != dir {
			t.Errorf("findProjectRoot() = %q, want %q", got, dir)
		}
	})

	t.Run("falls back to cwd when no ansible dir", func(t *testing.T) {
		dir := resolveSymlinks(t, t.TempDir())
		origDir, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		got, err := findProjectRoot()
		if err != nil {
			t.Fatalf("findProjectRoot() returned unexpected error: %v", err)
		}
		if got != dir {
			t.Errorf("findProjectRoot() = %q, want %q", got, dir)
		}
	})
}
