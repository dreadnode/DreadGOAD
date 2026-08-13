package cmd

import (
	"fmt"
	"os"
	"syscall"

	"github.com/dreadnode/dreadgoad/internal/azure"
	"github.com/spf13/cobra"
)

const bastionParentLifetimeFD = 3

// bastionWatchdogCmd is an internal subprocess entry point used by the Azure
// provision tunnel. It stays hidden because its inherited file descriptor is
// meaningful only when the command is launched by StartProvisionTunnel.
var bastionWatchdogCmd = &cobra.Command{
	Use:                "__bastion-watchdog <command> [args...]",
	Hidden:             true,
	DisableFlagParsing: true,
	Args:               cobra.MinimumNArgs(1),
	RunE: func(_ *cobra.Command, args []string) error {
		parentLifetime := os.NewFile(bastionParentLifetimeFD, "dreadgoad-parent-lifetime")
		if parentLifetime == nil {
			return fmt.Errorf("open parent lifetime descriptor %d", bastionParentLifetimeFD)
		}
		// ExtraFiles starts at fd 3. Keep the descriptor private to this
		// watchdog; the supervised az process must not inherit it.
		syscall.CloseOnExec(bastionParentLifetimeFD)
		return azure.RunBastionWatchdog(parentLifetime, args)
	},
}

func init() {
	rootCmd.AddCommand(bastionWatchdogCmd)
}
