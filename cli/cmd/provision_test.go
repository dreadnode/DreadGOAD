package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

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
