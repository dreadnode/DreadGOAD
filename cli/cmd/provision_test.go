package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dreadnode/dreadgoad/internal/ansible"
	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/variant"
	"github.com/spf13/cobra"
)

func TestProvisionFailureCarriesResumeDetails(t *testing.T) {
	cause := errors.New("ansible failed")
	err := &provisionFailure{
		Playbook: "ad-data.yml",
		LogFile:  "/tmp/provision.log",
		Err:      cause,
	}

	want := "provisioning failed at ad-data.yml: ansible failed\n  see full log: /tmp/provision.log"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, cause) {
		t.Error("provisionFailure does not unwrap to its cause")
	}
	var got *provisionFailure
	if !errors.As(fmt.Errorf("outer: %w", err), &got) || got.Playbook != "ad-data.yml" || got.LogFile != "/tmp/provision.log" {
		t.Errorf("errors.As() = %#v, want structured failure details", got)
	}
}

func retryFlagsCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().Int("max-retries", 0, "")
	cmd.Flags().Int("retry-delay", 0, "")
	return cmd
}

func TestRetryOverridesDistinguishOmittedAndExplicitZero(t *testing.T) {
	omitted, err := retryOverridesFromFlags(retryFlagsCommand())
	if err != nil {
		t.Fatalf("omitted flags: %v", err)
	}
	if omitted.maxRetries != nil || omitted.retryDelay != nil {
		t.Fatalf("omitted flags produced overrides: %#v", omitted)
	}

	cmd := retryFlagsCommand()
	for _, name := range []string{"max-retries", "retry-delay"} {
		if err := cmd.Flags().Set(name, "0"); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}
	explicit, err := retryOverridesFromFlags(cmd)
	if err != nil {
		t.Fatalf("explicit zero flags: %v", err)
	}
	if explicit.maxRetries == nil || *explicit.maxRetries != 0 || explicit.retryDelay == nil || *explicit.retryDelay != 0 {
		t.Fatalf("explicit zero flags lost: %#v", explicit)
	}

	var opts ansible.RetryOptions
	explicit.apply(&opts)
	if opts.MaxRetries != 0 || !opts.MaxRetriesSet {
		t.Errorf("MaxRetries = %d, set=%v; want 0,true", opts.MaxRetries, opts.MaxRetriesSet)
	}
	if opts.RetryDelay != 0 || !opts.RetryDelaySet {
		t.Errorf("RetryDelay = %s, set=%v; want 0,true", opts.RetryDelay, opts.RetryDelaySet)
	}
}

func TestRetryOverridesForwardPositiveValues(t *testing.T) {
	cmd := retryFlagsCommand()
	if err := cmd.Flags().Set("max-retries", "5"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("retry-delay", "12"); err != nil {
		t.Fatal(err)
	}
	retry, err := retryOverridesFromFlags(cmd)
	if err != nil {
		t.Fatal(err)
	}

	var opts ansible.RetryOptions
	retry.apply(&opts)
	if opts.MaxRetries != 5 || !opts.MaxRetriesSet || opts.RetryDelay != 12*time.Second || !opts.RetryDelaySet {
		t.Errorf("applied retry options = %#v", opts)
	}
}

func TestRetryOverridesRejectNegativeValues(t *testing.T) {
	for _, name := range []string{"max-retries", "retry-delay"} {
		t.Run(name, func(t *testing.T) {
			cmd := retryFlagsCommand()
			if err := cmd.Flags().Set(name, "-1"); err != nil {
				t.Fatal(err)
			}
			_, err := retryOverridesFromFlags(cmd)
			if err == nil || !strings.Contains(err.Error(), "must be zero or greater") {
				t.Errorf("error = %v, want non-negative validation", err)
			}
		})
	}
}

func variantTestConfig(root, source, target string) *config.Config {
	return &config.Config{
		ProjectRoot: root,
		Env:         "dev",
		Environments: map[string]config.EnvironmentConfig{
			"dev": {
				Variant:       true,
				VariantSource: source,
				VariantTarget: target,
			},
		},
	}
}

func TestEnsureVariantReusesCompleteTarget(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, variant.CompletionMarkerName), []byte("complete\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureVariant(variantTestConfig(t.TempDir(), "unused", target)); err != nil {
		t.Fatalf("ensureVariant() error: %v", err)
	}
}

func TestEnsureVariantRejectsIncompleteTarget(t *testing.T) {
	target := t.TempDir()

	err := ensureVariant(variantTestConfig(t.TempDir(), "unused", target))
	if err == nil || !strings.Contains(err.Error(), "variant directory is incomplete") ||
		!strings.Contains(err.Error(), variant.CompletionMarkerName) {
		t.Fatalf("ensureVariant() error = %v, want incomplete variant error", err)
	}
}

func TestEnsureVariantRejectsNonDirectoryTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "variant")
	if err := os.WriteFile(target, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ensureVariant(variantTestConfig(root, "unused", target))
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("ensureVariant() error = %v, want non-directory error", err)
	}
}

func TestEnsureVariantReturnsTargetInspectionError(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(parent, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "variant")

	err := ensureVariant(variantTestConfig(root, "unused", target))
	if err == nil || !strings.Contains(err.Error(), "inspect variant target") {
		t.Fatalf("ensureVariant() error = %v, want inspection error", err)
	}
}

func TestEnsureVariantGeneratesMissingTargetAndMarksComplete(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(source, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "data", "config.json"), []byte(`{"lab":{"hosts":{},"domains":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ensureVariant(variantTestConfig(root, source, target)); err != nil {
		t.Fatalf("ensureVariant() error: %v", err)
	}
	complete, err := variant.IsComplete(target)
	if err != nil {
		t.Fatalf("check completion marker: %v", err)
	}
	if !complete {
		t.Fatal("generated variant has no completion marker")
	}
}

func extraVarsCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "test", RunE: func(*cobra.Command, []string) error { return nil }}
	c.Flags().StringArrayP("extra-vars", "E", nil, extraVarsUsage)
	c.SetArgs(args)
	c.SetOut(nil)
	if err := c.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return c
}

func TestParseExtraVars(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want map[string]string
	}{
		{
			name: "absent flag yields no vars",
			args: nil,
			want: nil,
		},
		{
			name: "the reconciler dry-run this flag exists for",
			args: []string{"-E", "ad_reconcile_check_only=true"},
			want: map[string]string{"ad_reconcile_check_only": "true"},
		},
		{
			name: "repeated flag accumulates",
			args: []string{"-E", "a=1", "--extra-vars", "b=2"},
			want: map[string]string{"a": "1", "b": "2"},
		},
		{
			// Ansible values legitimately contain '=', so only the first
			// separator may split. Cutting on the last would corrupt them.
			name: "value keeps later equals signs",
			args: []string{"-E", "filter=name=jon"},
			want: map[string]string{"filter": "name=jon"},
		},
		{
			name: "empty value is preserved, not dropped",
			args: []string{"-E", "quiet="},
			want: map[string]string{"quiet": ""},
		},
		{
			name: "last write wins on a repeated key",
			args: []string{"-E", "a=1", "-E", "a=2"},
			want: map[string]string{"a": "2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseExtraVars(extraVarsCmd(t, tc.args...))
			if err != nil {
				t.Fatalf("parseExtraVars: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("key %q: got %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// TestParseExtraVarsRejectsMalformed matters more than it looks: a silently
// ignored var reads as "the dry-run ran and found nothing" when what actually
// happened is a destructive write with the default still in force.
//
// `-E ""` is deliberately absent. pflag drops an empty StringArray value before
// the parser sees it, so there is nothing to reject and nothing at risk.
func TestParseExtraVarsRejectsMalformed(t *testing.T) {
	for _, arg := range []string{"novalue", "=novalue"} {
		t.Run(arg, func(t *testing.T) {
			if _, err := parseExtraVars(extraVarsCmd(t, "-E", arg)); err == nil {
				t.Errorf("expected an error for %q, got none", arg)
			}
		})
	}
}

// TestApplyExtraVarsPrecedence pins the layering. The tunnel vars are
// connection plumbing, so a user var must win, but only the keys it names: a
// -e that quietly dropped the rest would break the connection instead of the
// setting the operator meant to change.
func TestApplyExtraVarsPrecedence(t *testing.T) {
	socks := map[string]string{
		"ansible_connection": "psrp",
		"ansible_port":       "5985",
	}
	got := applyExtraVars(socks, map[string]string{
		"ansible_port":            "5986",
		"ad_reconcile_check_only": "true",
	})

	want := map[string]string{
		"ansible_connection":      "psrp",
		"ansible_port":            "5986",
		"ad_reconcile_check_only": "true",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}
	if socks["ansible_port"] != "5985" {
		t.Errorf("caller's map was mutated: %v", socks)
	}
}

func TestApplyExtraVarsWithoutUserVarsIsPassthrough(t *testing.T) {
	socks := map[string]string{"ansible_connection": "psrp"}
	if got := applyExtraVars(socks, nil); got["ansible_connection"] != "psrp" || len(got) != 1 {
		t.Errorf("got %v, want the tunnel vars unchanged", got)
	}
}

func TestScopeProvisionVarsOverrideWindowsRemoteTemp(t *testing.T) {
	got := scopeProvisionVars("/tmp/scope-key", "127.0.0.1:62103")
	if got["ansible_remote_tmp"] != "/tmp/.ansible-scope" {
		t.Fatalf("remote tmp = %q, want Linux /tmp path", got["ansible_remote_tmp"])
	}
	if !strings.Contains(got["ansible_ssh_common_args"], "127.0.0.1:62103 %h %p") {
		t.Fatalf("SSH common args do not contain SOCKS endpoint: %q", got["ansible_ssh_common_args"])
	}
	if got["ansible_ssh_private_key_file"] != "/tmp/scope-key" {
		t.Fatalf("private key = %q, want /tmp/scope-key", got["ansible_ssh_private_key_file"])
	}
}

func TestSortedPairsIsStable(t *testing.T) {
	got := sortedPairs(map[string]string{"b": "2", "a": "1", "c": "3"})
	want := []string{"a=1", "b=2", "c=3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
