package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dreadnode/dreadgoad/internal/config"
	"github.com/dreadnode/dreadgoad/internal/lab"
	"github.com/spf13/cobra"
)

var labListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available GOAD labs and their providers",
	RunE:  runLabList,
}

func init() {
	labCmd.AddCommand(labListCmd)
	labListCmd.Flags().Bool("json", false, "Output as JSON")
}

func runLabList(cmd *cobra.Command, args []string) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	labs, err := lab.DiscoverLabs(cfg.ProjectRoot)
	if err != nil {
		return err
	}

	if len(labs) == 0 {
		fmt.Println("No labs found in ad/ directory")
		return nil
	}

	jsonFlag, _ := cmd.Flags().GetBool("json")
	if jsonFlag {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(labs)
	}

	fmt.Printf("%-20s %-40s %s\n", "LAB", "PROVIDERS", "HOSTS")
	fmt.Println(strings.Repeat("-", 80))
	for _, l := range labs {
		fmt.Printf("%-20s %-40s %s\n",
			l.Name,
			strings.Join(l.Providers, ", "),
			strings.Join(l.Hosts, ", "),
		)
	}
	return nil
}
