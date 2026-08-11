package cmd

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/provider"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestUpProvisionCommandRegistersEveryProvisionFlag(t *testing.T) {
	synth, err := newUpProvisionCommand(context.Background(), upProvisionOptions{})
	if err != nil {
		t.Fatalf("newUpProvisionCommand() error: %v", err)
	}

	provisionCmd.Flags().VisitAll(func(flag *pflag.Flag) {
		if synth.Flags().Lookup(flag.Name) == nil {
			t.Errorf("up does not register provision flag --%s", flag.Name)
		}
	})
}

func TestUpProvisionCommandForwardsValues(t *testing.T) {
	maxRetries, retryDelay := 4, 45
	synth, err := newUpProvisionCommand(context.Background(), upProvisionOptions{
		fromPlaybook: "ad-data.yml",
		limit:        "dc01",
		retry: retryOverrides{
			maxRetries: &maxRetries,
			retryDelay: &retryDelay,
		},
	})
	if err != nil {
		t.Fatalf("newUpProvisionCommand() error: %v", err)
	}

	assertStringFlag(t, synth, "plays", "")
	assertStringFlag(t, synth, "from", "ad-data.yml")
	assertStringFlag(t, synth, "limit", "dc01")
	assertIntFlag(t, synth, "max-retries", 4)
	assertIntFlag(t, synth, "retry-delay", 45)

	extraVars, err := synth.Flags().GetStringArray("extra-vars")
	if err != nil {
		t.Fatalf("get --extra-vars: %v", err)
	}
	if len(extraVars) != 0 {
		t.Errorf("--extra-vars = %v, want empty", extraVars)
	}
}

func TestUpProvisionCommandForwardsExplicitZeroRetryFlags(t *testing.T) {
	zero := 0
	synth, err := newUpProvisionCommand(context.Background(), upProvisionOptions{
		retry: retryOverrides{maxRetries: &zero, retryDelay: &zero},
	})
	if err != nil {
		t.Fatalf("newUpProvisionCommand() error: %v", err)
	}

	for _, name := range []string{"max-retries", "retry-delay"} {
		assertIntFlag(t, synth, name, 0)
		if !synth.Flags().Changed(name) {
			t.Errorf("--%s explicit zero was not marked as set", name)
		}
	}
}

func TestUpProvisionResumeResolvesPlaybookSuffix(t *testing.T) {
	synth, err := newUpProvisionCommand(context.Background(), upProvisionOptions{
		fromPlaybook: "ad-data.yml",
	})
	if err != nil {
		t.Fatalf("newUpProvisionCommand() error: %v", err)
	}

	from, err := synth.Flags().GetString("from")
	if err != nil {
		t.Fatalf("get --from: %v", err)
	}
	cfg := &config.Config{
		ProjectRoot: t.TempDir(),
		Playbooks:   []string{"build.yml", "ad-servers.yml", "ad-data.yml", "vulnerabilities.yml"},
	}
	got, err := resolvePlaybooks(cfg, "", from)
	if err != nil {
		t.Fatalf("resolvePlaybooks() error: %v", err)
	}
	want := []string{"ad-data.yml", "vulnerabilities.yml"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("resolved playbooks = %v, want %v", got, want)
	}
}

func TestValidateUpProvisionResume(t *testing.T) {
	withProvision := []upStep{{id: "provision"}, {id: "health-check"}}
	withoutProvision := []upStep{{id: "health-check"}}

	if err := validateUpProvisionResume(withProvision, "", "ad-data.yml"); err != nil {
		t.Errorf("valid resume rejected: %v", err)
	}
	if err := validateUpProvisionResume(withoutProvision, "", ""); err != nil {
		t.Errorf("empty --from-playbook should be ignored: %v", err)
	}

	tests := []struct {
		name  string
		steps []upStep
		plays string
		want  string
	}{
		{
			name:  "plays conflict",
			steps: withProvision,
			plays: "ad-data.yml",
			want:  "cannot be combined with --plays",
		},
		{
			name:  "provision skipped",
			steps: withoutProvision,
			want:  "provision step is skipped",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateUpProvisionResume(tc.steps, tc.plays, "ad-data.yml")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want text %q", err, tc.want)
			}
		})
	}
}

func TestUpResumeCommandNamesFailedPlaybook(t *testing.T) {
	cause := errors.New("ansible failed")
	failure := &provisionFailure{
		Playbook: "ad-data.yml",
		LogFile:  "/tmp/provision.log",
		Err:      cause,
	}

	got := upResumeCommand("provision", fmt.Errorf("wrapped: %w", failure), upResumeOptions{})
	want := "dreadgoad up --from provision --from-playbook 'ad-data.yml'"
	if got != want {
		t.Errorf("resume command = %q, want %q", got, want)
	}

	if got := upResumeCommand("provision", cause, upResumeOptions{}); got != "dreadgoad up --from provision" {
		t.Errorf("generic provision resume command = %q", got)
	}
	if got := upResumeCommand("infra", failure, upResumeOptions{}); got != "dreadgoad up --from infra" {
		t.Errorf("infra resume command = %q", got)
	}
}

func TestUpResumeCommandPreservesRemainingCustomPlaybooks(t *testing.T) {
	failure := &provisionFailure{
		Playbook: "custom-data.yml",
		LogFile:  "/tmp/provision.log",
		Err:      errors.New("ansible failed"),
	}

	got := upResumeCommand(
		"provision",
		failure,
		upResumeOptions{plays: "bootstrap.yml,custom-data.yml,custom vulnerabilities.yml"},
	)
	want := "dreadgoad up --from provision --plays 'custom-data.yml,custom vulnerabilities.yml'"
	if got != want {
		t.Errorf("resume command = %q, want %q", got, want)
	}
}

func TestUpResumeCommandPreservesExecutionOverrides(t *testing.T) {
	zero, delay := 0, 12
	failure := &provisionFailure{
		Playbook: "ad-data.yml",
		LogFile:  "/tmp/provision.log",
		Err:      errors.New("ansible failed"),
	}
	opts := upResumeOptions{
		limit: "dc01,DC 02",
		retry: retryOverrides{
			maxRetries: &zero,
			retryDelay: &delay,
		},
	}

	got := upResumeCommand("provision", failure, opts)
	want := "dreadgoad up --from provision --from-playbook 'ad-data.yml' --limit 'dc01,DC 02' --max-retries 0 --retry-delay 12"
	if got != want {
		t.Errorf("resume command = %q, want %q", got, want)
	}
}

func TestUpResumeCommandPreservesOverridesBeforeProvisioning(t *testing.T) {
	zero := 0
	opts := upResumeOptions{
		plays:        "build.yml,ad-data.yml",
		limit:        "dc01",
		infraModule:  "network",
		infraExclude: "bastion,monitoring",
		retry:        retryOverrides{maxRetries: &zero},
	}

	got := upResumeCommand("infra", errors.New("terraform failed"), opts)
	want := "dreadgoad up --from infra --module 'network' --exclude 'bastion,monitoring' --plays 'build.yml,ad-data.yml' --limit 'dc01' --max-retries 0"
	if got != want {
		t.Errorf("resume command = %q, want %q", got, want)
	}

	got = upResumeCommand("health-check", errors.New("check failed"), opts)
	if want := "dreadgoad up --from health-check"; got != want {
		t.Errorf("health-check resume command = %q, want %q", got, want)
	}
}

func TestShellQuoteResumeArg(t *testing.T) {
	got := shellQuoteResumeArg("first.yml,operator's.yml")
	want := `'first.yml,operator'"'"'s.yml'`
	if got != want {
		t.Errorf("shellQuoteResumeArg() = %q, want %q", got, want)
	}
}

func TestUpDoctorFailureDoesNotRecommendBypass(t *testing.T) {
	err := upDoctorFailure(2)
	message := err.Error()
	for _, want := range []string{"2 pre-flight check(s) failed", "dreadgoad doctor", "retry 'dreadgoad up'"} {
		if !strings.Contains(message, want) {
			t.Errorf("error %q does not contain %q", message, want)
		}
	}
	if strings.Contains(message, "skip-doctor") || strings.Contains(message, "bypass") {
		t.Errorf("doctor failure recommends bypassing checks: %q", message)
	}
}

func assertStringFlag(t *testing.T, cmd *cobra.Command, name, want string) {
	t.Helper()
	got, err := cmd.Flags().GetString(name)
	if err != nil {
		t.Fatalf("get --%s: %v", name, err)
	}
	if got != want {
		t.Errorf("--%s = %q, want %q", name, got, want)
	}
}

func assertIntFlag(t *testing.T, cmd *cobra.Command, name string, want int) {
	t.Helper()
	got, err := cmd.Flags().GetInt(name)
	if err != nil {
		t.Fatalf("get --%s: %v", name, err)
	}
	if got != want {
		t.Errorf("--%s = %d, want %d", name, got, want)
	}
}

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
