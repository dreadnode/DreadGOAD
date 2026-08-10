package cmd

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// `up` drives `infra apply` through a synthetic command rather than the real
// one, so its flag set is maintained by hand. When the two drift, the missing
// flag reads back as its zero value and the lookup error is discarded — the
// inner action cannot tell "not registered" from "left at default".
//
// That is exactly how --with-bastion/--with-controller went missing: they were
// added to `infra apply` (#161) three days after up.go was written (#141), and
// `dreadgoad up` silently deployed Azure ranges with no Bastion and no
// controller, then failed at step 3 because provisioning had no route to any
// Windows host. This test fails on any future flag added to `infra apply` and
// not forwarded here.
func TestUpInfraCommandForwardsEveryFlag(t *testing.T) {
	synth := newUpInfraCommand(context.Background(), provider.NameAzure)

	check := func(label string, set *pflag.FlagSet) {
		set.VisitAll(func(f *pflag.Flag) {
			if synth.Flags().Lookup(f.Name) == nil {
				t.Errorf("up does not forward %s --%s to `infra apply`", label, f.Name)
			}
		})
	}
	check("local flag", infraApplyCmd.Flags())
	// resolveDeployment reads --deployment, which is persistent on the parent.
	check("persistent flag", infraCmd.PersistentFlags())
}

// Bastion and the in-VNet controller are prerequisites of the `up` pipeline on
// Azure, not options: provisioning reaches the Windows hosts only through them.
// They must therefore default ON for Azure and stay OFF everywhere else, where
// the modules do not exist.
func TestUpInfraCommandEnablesAzureTunnelModules(t *testing.T) {
	for _, tc := range []struct {
		providerName string
		want         bool
	}{
		{provider.NameAzure, true},
		{provider.NameAWS, false},
		{"proxmox", false},
		{"ludus", false},
	} {
		t.Run(tc.providerName, func(t *testing.T) {
			c := newUpInfraCommand(context.Background(), tc.providerName)
			for _, name := range []string{"with-bastion", "with-controller"} {
				got, err := c.Flags().GetBool(name)
				if err != nil {
					t.Fatalf("--%s not registered: %v", name, err)
				}
				if got != tc.want {
					t.Errorf("--%s = %v, want %v for provider %q",
						name, got, tc.want, tc.providerName)
				}
			}
		})
	}
}

// The flags only matter insofar as they reach terragrunt. This drives up's
// command through the real translation step (azureModuleEnv) and asserts on
// the env vars the exclude{} blocks actually read, closing the chain:
//
//	up → newUpInfraCommand → azureModuleEnv → DREADGOAD_ENABLE_AZURE_* →
//	terragrunt exclude{} → bastion + controller deployed → step 3 can reach
//	the Windows hosts.
func TestUpDeploysTheAzureModulesProvisioningNeeds(t *testing.T) {
	// An empty layout, so the destroy-time fallback has nothing to find and
	// we observe the forwarded flags alone.
	emptyRoot := t.TempDir()

	got := azureModuleEnv(newUpInfraCommand(context.Background(), provider.NameAzure), "apply", emptyRoot)

	for _, want := range []string{
		"DREADGOAD_ENABLE_AZURE_BASTION=true",
		"DREADGOAD_ENABLE_AZURE_CONTROLLER=true",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("`up` on Azure does not set %s; provisioning will have no route "+
				"to the Windows hosts and step 3 fails. got=%v", want, got)
		}
	}
	if slices.Contains(got, "DREADGOAD_ENABLE_AZURE_KALI=true") {
		t.Errorf("`up` deployed the Kali box without --with-kali: %v", got)
	}
}

// Non-Azure providers have no such modules, so up must not set the vars.
func TestUpSetsNoAzureModuleEnvOnOtherProviders(t *testing.T) {
	emptyRoot := t.TempDir()

	for _, name := range []string{provider.NameAWS, "proxmox", "ludus"} {
		t.Run(name, func(t *testing.T) {
			got := azureModuleEnv(newUpInfraCommand(context.Background(), name), "apply", emptyRoot)
			if len(got) != 0 {
				t.Errorf("provider %q set Azure module env: %v", name, got)
			}
		})
	}
}

// Round-trip invariant: every module `up` deploys on Azure must also be torn
// down by a BARE `infra destroy` — which is exactly what the console's
// /destroy runs (commands.py maps it to ("infra", "destroy") with no flags).
// A module in the up set but not the destroy set is a resource left standing
// and still billing, and Bastion is the expensive one.
func TestUpDestroyRoundTripLeavesNothingBehind(t *testing.T) {
	t.Cleanup(func() { upWithKali = false })

	root := moduleRootWith(t, "bastion", "controller", "kali", "goad", "network")

	for _, kali := range []bool{false, true} {
		upWithKali = kali

		created := azureModuleEnv(
			newUpInfraCommand(context.Background(), provider.NameAzure), "apply", root)

		// A bare destroy, carrying the real infraDestroyCmd flag set at defaults.
		bare := &cobra.Command{}
		bare.Flags().AddFlagSet(infraDestroyCmd.Flags())
		destroyed := azureModuleEnv(bare, "destroy", root)

		for _, mod := range created {
			if !slices.Contains(destroyed, mod) {
				t.Errorf("--with-kali=%v: `up` deploys %s but a bare `infra destroy` "+
					"does not tear it down — orphaned resource.\n  up=%v\n  destroy=%v",
					kali, strings.TrimSuffix(mod, "=true"), created, destroyed)
			}
		}
	}
}

// Kali is a real attack box the operator pays for, so unlike the tunnel modules
// it stays opt-in on every provider.
func TestUpInfraCommandKaliIsOptIn(t *testing.T) {
	t.Cleanup(func() { upWithKali = false })

	upWithKali = false
	if got, _ := newUpInfraCommand(context.Background(), provider.NameAzure).Flags().GetBool("with-kali"); got {
		t.Error("--with-kali defaulted to true; it must stay opt-in")
	}

	upWithKali = true
	if got, _ := newUpInfraCommand(context.Background(), provider.NameAzure).Flags().GetBool("with-kali"); !got {
		t.Error("up --with-kali was not forwarded to `infra apply`")
	}
}
