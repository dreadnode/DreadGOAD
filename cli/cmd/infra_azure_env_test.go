package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

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
