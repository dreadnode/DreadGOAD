package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dreadnode/dreadgoad/internal/config"
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

	got := upResumeCommand("provision", fmt.Errorf("wrapped: %w", failure))
	want := "dreadgoad up --from provision --from-playbook ad-data.yml"
	if got != want {
		t.Errorf("resume command = %q, want %q", got, want)
	}

	if got := upResumeCommand("provision", cause); got != "dreadgoad up --from provision" {
		t.Errorf("generic provision resume command = %q", got)
	}
	if got := upResumeCommand("infra", failure); got != "dreadgoad up --from infra" {
		t.Errorf("infra resume command = %q", got)
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
