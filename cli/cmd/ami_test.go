package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestGetFlagString(t *testing.T) {
	newCmd := func(flagVal string) *cobra.Command {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("instance-profile", "", "")
		if flagVal != "" {
			if err := cmd.Flags().Set("instance-profile", flagVal); err != nil {
				t.Fatalf("set flag: %v", err)
			}
		}
		return cmd
	}

	tests := []struct {
		name      string
		flagVal   string
		fallback1 string
		fallback2 string
		want      string
	}{
		{
			name:      "flag wins over config fallback",
			flagVal:   "FlagProfile",
			fallback1: "ConfigProfile",
			fallback2: "default",
			want:      "FlagProfile",
		},
		{
			name:      "config fallback used when flag empty",
			flagVal:   "",
			fallback1: "ConfigProfile",
			fallback2: "default",
			want:      "ConfigProfile",
		},
		{
			name:      "second fallback when both flag and config empty",
			flagVal:   "",
			fallback1: "",
			fallback2: "default",
			want:      "default",
		},
		{
			name:      "empty when all sources empty",
			flagVal:   "",
			fallback1: "",
			fallback2: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCmd(tt.flagVal)
			got := getFlagString(cmd, "instance-profile", tt.fallback1, tt.fallback2)
			if got != tt.want {
				t.Errorf("getFlagString() = %q, want %q", got, tt.want)
			}
		})
	}
}
