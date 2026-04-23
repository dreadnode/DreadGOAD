package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
)

func TestIsSSMInventory(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			"aws ssm inventory",
			"[default]\ndc01 ansible_host=i-0abc dict_key=dc01 dns_domain=dc01\n\n[all:vars]\nansible_connection=amazon.aws.aws_ssm\nansible_aws_ssm_region=us-east-1\n",
			true,
		},
		{
			"ludus inventory",
			"[default]\ndc01 ansible_host=10.0.10.10 dns_domain=dc01 dict_key=dc01\n\n[all:vars]\nansible_user=localuser\nansible_password=password\n",
			false,
		},
		{
			"proxmox winrm inventory",
			"[default]\ndc01 ansible_host=192.168.1.10 dict_key=dc01\n\n[all:vars]\nansible_connection=winrm\n",
			false,
		},
		{
			"ssh inventory",
			"[default]\nlx01 ansible_host=10.0.0.12 dict_key=lx01\n\n[all:vars]\nansible_connection=ssh\n",
			false,
		},
		{
			"no inventory file",
			"",
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := &config.Config{
				Env:         "test",
				ProjectRoot: dir,
			}

			if tt.content != "" {
				invPath := filepath.Join(dir, "test-inventory")
				if err := os.WriteFile(invPath, []byte(tt.content), 0o644); err != nil {
					t.Fatalf("write inventory: %v", err)
				}
			}

			got := isSSMInventory(cfg)
			if got != tt.want {
				t.Errorf("isSSMInventory() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureInventorySynced_SkipsNonSSM(t *testing.T) {
	dir := t.TempDir()
	invPath := filepath.Join(dir, "test-inventory")

	// Write a Ludus-style inventory (no SSM connection)
	content := "[default]\ndc01 ansible_host=10.0.10.10 dns_domain=dc01 dict_key=dc01\n\n[all:vars]\nansible_user=localuser\n"
	if err := os.WriteFile(invPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write inventory: %v", err)
	}

	// Construct cfg directly to avoid the global config singleton
	// picking up the real project root.
	cfg := &config.Config{
		Env:         "test",
		ProjectRoot: dir,
	}

	// Should return nil (no-op) without attempting AWS calls.
	// If it tried AWS, it would error on missing credentials/region.
	if err := ensureInventorySynced(context.Background(), cfg); err != nil {
		t.Errorf("ensureInventorySynced() should be no-op for non-SSM, got error: %v", err)
	}
}

func TestBootstrapInventory(t *testing.T) {
	t.Run("copies example when inventory missing", func(t *testing.T) {
		dir := t.TempDir()
		invPath := filepath.Join(dir, "dev-inventory")
		examplePath := invPath + ".example"

		exampleContent := []byte("[all:vars]\nenv=dev\nregion=us-west-2\n")
		if err := os.WriteFile(examplePath, exampleContent, 0o644); err != nil {
			t.Fatalf("write example: %v", err)
		}

		if err := bootstrapInventory(invPath); err != nil {
			t.Fatalf("bootstrapInventory() error: %v", err)
		}

		got, err := os.ReadFile(invPath)
		if err != nil {
			t.Fatalf("read bootstrapped inventory: %v", err)
		}
		if string(got) != string(exampleContent) {
			t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, exampleContent)
		}
	})

	t.Run("no-op when inventory exists", func(t *testing.T) {
		dir := t.TempDir()
		invPath := filepath.Join(dir, "dev-inventory")

		existing := []byte("[all:vars]\nenv=dev\ninstance=i-abc123\n")
		if err := os.WriteFile(invPath, existing, 0o644); err != nil {
			t.Fatalf("write existing: %v", err)
		}

		if err := bootstrapInventory(invPath); err != nil {
			t.Fatalf("bootstrapInventory() error: %v", err)
		}

		got, err := os.ReadFile(invPath)
		if err != nil {
			t.Fatalf("read inventory: %v", err)
		}
		if string(got) != string(existing) {
			t.Errorf("existing inventory was overwritten")
		}
	})

	t.Run("errors when neither file exists", func(t *testing.T) {
		dir := t.TempDir()
		invPath := filepath.Join(dir, "dev-inventory")

		err := bootstrapInventory(invPath)
		if err == nil {
			t.Fatal("expected error when no inventory or example exists")
		}
	})
}
