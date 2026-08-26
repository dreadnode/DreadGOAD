package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/scoreboard"
	"github.com/spf13/cobra"
)

// ssmOutputLimit is AWS SSM's GetCommandInvocation StandardOutputContent cap
// (24,000 chars). A `cat` at that length was almost certainly truncated.
const ssmOutputLimit = 24000

var scoreFetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Copy an agent report off the attack box to a local path",
	Long: `Reads a report file from the Kali attack box and writes it locally.

Uses the same connection machinery as ` + "`score --live-verify`" + ` — SSM on AWS,
Azure Bastion on Azure — so the attack box and (on Azure) the SSH key are
auto-discovered. On AWS pass --attack-box (the Kali instance id). This exists so
tooling can score a report that lives on the box, which ` + "`score --report`" + ` (a
local path) otherwise can't reach.`,
	Example: `  dreadgoad score fetch --remote /root/report.jsonl --local ./report.jsonl --attack-box i-0abc123
  dreadgoad -p azure score fetch --remote /root/report.jsonl --local ./report.jsonl`,
	RunE: runScoreFetch,
}

func init() {
	scoreCmd.AddCommand(scoreFetchCmd)
	scoreFetchCmd.Flags().String("remote", "", "Path to the report on the attack box (required)")
	scoreFetchCmd.Flags().String("local", "", "Local destination path (default: stdout)")
	// Same connection flags as `score --live-verify` (consumed by buildShellRunner).
	scoreFetchCmd.Flags().String("attack-box", "", "Instance ID (AWS) or resource ID (Azure) of the Kali attack box")
	scoreFetchCmd.Flags().String("region", "", "AWS region for SSM")
	scoreFetchCmd.Flags().String("profile", "", "AWS named profile")
	scoreFetchCmd.Flags().String("ssh-key", "", "Path to SSH private key for the Kali VM (Azure; auto-discovered if omitted)")
	scoreFetchCmd.Flags().String("ssh-user", "kali", "SSH username for the Kali VM (Azure)")
}

// shellSingleQuote wraps s in single quotes for safe interpolation into a remote
// shell command, escaping any embedded single quotes.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func runScoreFetch(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	remote, _ := cmd.Flags().GetString("remote")
	if remote == "" {
		return fmt.Errorf("--remote is required")
	}

	cfg, err := config.Get()
	if err != nil {
		return err
	}

	runner, err := buildShellRunner(ctx, cmd, cfg)
	if err != nil {
		return err
	}

	// `cat` the file over the existing connection — works for SSM and Bastion.
	out, err := runner.RunShell(ctx, "cat -- "+shellSingleQuote(remote), 120*time.Second)
	if err != nil {
		return fmt.Errorf("read %s from attack box: %w", remote, err)
	}

	// SSM truncates stdout at 24,000 chars — a report at that length is almost
	// certainly cut off. Fail loudly rather than write a partial report that
	// would then be mis-scored. (Bastion has no such cap.)
	if _, isSSM := runner.(*scoreboard.SSMShellRunner); isSSM && len(out) >= ssmOutputLimit {
		return fmt.Errorf(
			"report looks truncated at SSM's %d-char stdout limit (%d bytes read); "+
				"the report is too large to fetch this way",
			ssmOutputLimit, len(out),
		)
	}

	local, _ := cmd.Flags().GetString("local")
	if local == "" {
		fmt.Print(out)
		return nil
	}
	if err := os.WriteFile(local, []byte(out), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", local, err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "fetched %s -> %s (%d bytes)\n", remote, local, len(out))
	return nil
}
