package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

	env := c.AnsibleEnv()

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

	// First playbook should be build.yml
	if DefaultPlaybooks[0] != "build.yml" {
		t.Errorf("first playbook = %q, want %q", DefaultPlaybooks[0], "build.yml")
	}

	// Last playbook should be vulnerabilities.yml
	last := DefaultPlaybooks[len(DefaultPlaybooks)-1]
	if last != "vulnerabilities.yml" {
		t.Errorf("last playbook = %q, want %q", last, "vulnerabilities.yml")
	}

	// All playbooks should end in .yml
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

	// All reboot playbooks should be in DefaultPlaybooks
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

		// Change to a subdirectory
		subDir := filepath.Join(dir, "sub", "deep")
		if err := os.MkdirAll(subDir, 0o755); err != nil {
			t.Fatal(err)
		}

		origDir, _ := os.Getwd()
		if err := os.Chdir(subDir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(origDir) })

		got := findProjectRoot()
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

		got := findProjectRoot()
		if got != dir {
			t.Errorf("findProjectRoot() = %q, want %q", got, dir)
		}
	})
}
