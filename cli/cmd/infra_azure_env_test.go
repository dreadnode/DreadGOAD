package cmd

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/spf13/cobra"
)

// withFlags builds a command carrying the --with-* opt-ins, standing in for
// whichever real or synthetic command reaches azureModuleEnv.
func withFlags(bastion, controller, kali bool) *cobra.Command {
	c := &cobra.Command{}
	c.Flags().Bool("with-bastion", bastion, "")
	c.Flags().Bool("with-controller", controller, "")
	c.Flags().Bool("with-kali", kali, "")
	return c
}

// moduleRootWith builds a layout containing the named module directories.
func moduleRootWith(t *testing.T, dirs ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// `infra destroy` carries no --with-* flags (the console's /destroy runs it
// bare), and every exclude{} block uses actions = ["all"], so a module left
// out of the destroy keeps its resources standing. Since `up` now deploys
// bastion and controller by default on Azure, missing either one orphans a
// billed, always-on Bastion after every range teardown.
func TestAzureModuleEnvDestroyIncludesEveryPresentModule(t *testing.T) {
	root := moduleRootWith(t, "bastion", "controller", "kali")

	got := azureModuleEnv(withFlags(false, false, false), "destroy", root)

	for _, want := range []string{
		"DREADGOAD_ENABLE_AZURE_BASTION=true",
		"DREADGOAD_ENABLE_AZURE_CONTROLLER=true",
		"DREADGOAD_ENABLE_AZURE_KALI=true",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("destroy skipped a deployed module (%s); its resources survive "+
				"teardown and keep billing. got=%v", want, got)
		}
	}
}

// The fallback is gated on the module being present in the layout, so a
// deployment tree without one does not enable it.
func TestAzureModuleEnvDestroySkipsAbsentModules(t *testing.T) {
	root := moduleRootWith(t, "bastion") // no controller, no kali

	got := azureModuleEnv(withFlags(false, false, false), "destroy", root)

	if !slices.Contains(got, "DREADGOAD_ENABLE_AZURE_BASTION=true") {
		t.Errorf("present module not enabled on destroy: %v", got)
	}
	for _, unwanted := range []string{
		"DREADGOAD_ENABLE_AZURE_CONTROLLER=true",
		"DREADGOAD_ENABLE_AZURE_KALI=true",
	} {
		if slices.Contains(got, unwanted) {
			t.Errorf("absent module enabled on destroy (%s): %v", unwanted, got)
		}
	}
}

// The fallback is destroy-only: an apply must never deploy a module the
// operator did not ask for, however the layout looks.
func TestAzureModuleEnvApplyNeverFallsBack(t *testing.T) {
	root := moduleRootWith(t, "bastion", "controller", "kali")

	for _, action := range []string{"apply", "plan", "init"} {
		t.Run(action, func(t *testing.T) {
			if got := azureModuleEnv(withFlags(false, false, false), action, root); len(got) != 0 {
				t.Errorf("%s enabled modules without a flag: %v", action, got)
			}
		})
	}
}

// Explicit flags are honoured on apply.
func TestAzureModuleEnvApplyHonoursFlags(t *testing.T) {
	root := moduleRootWith(t) // empty: only the flags can turn anything on

	got := azureModuleEnv(withFlags(true, true, false), "apply", root)

	want := []string{
		"DREADGOAD_ENABLE_AZURE_BASTION=true",
		"DREADGOAD_ENABLE_AZURE_CONTROLLER=true",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// An unregistered flag reads back as false with the error discarded — the
// failure mode that broke `up` on Azure. Pinned so the silence is at least
// deliberate: callers are kept honest by TestUpInfraCommandForwardsEveryFlag.
func TestAzureModuleEnvTreatsMissingFlagsAsOff(t *testing.T) {
	if got := azureModuleEnv(&cobra.Command{}, "apply", moduleRootWith(t)); len(got) != 0 {
		t.Errorf("expected no env from a command with no flags, got %v", got)
	}
}

// Azure's Bastion, controller, and Kali units each create a subnet in one
// shared VNet. Exercise the real Azure caller so a future refactor cannot drop
// the unit-level serialization while leaving only runner-level tests green.
func TestAzureRunAllSerializesSharedVNetMutations(t *testing.T) {
	root := t.TempDir()
	workDir := filepath.Join(root, "infra", "azure", "goad-deployment", "dev", "centralus")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}

	argsLog := filepath.Join(root, "terragrunt-args.log")
	fakeTerragrunt := filepath.Join(root, "terragrunt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DREADGOAD_TEST_TG_ARGS\"\n"
	if err := os.WriteFile(fakeTerragrunt, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DREADGOAD_TEST_TG_ARGS", argsLog)

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cfg := &config.Config{
		Env:         "dev",
		Provider:    "azure",
		Region:      "centralus",
		ProjectRoot: root,
		LogDir:      filepath.Join(root, "logs"),
		Infra: config.InfraConfig{
			Deployment:       "goad-deployment",
			TerragruntBinary: fakeTerragrunt,
		},
	}

	if err := runInfraActionAzure(cmd, cfg, "apply"); err != nil {
		t.Fatalf("runInfraActionAzure: %v", err)
	}
	data, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected pre-init and apply invocations, got %q", lines)
	}
	for _, line := range lines {
		if !strings.Contains(line, "--parallelism 1 --") {
			t.Fatalf("Azure invocation was not serialized: %q", line)
		}
	}
}
