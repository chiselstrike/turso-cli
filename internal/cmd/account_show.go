package cmd

import (
	"fmt"

	"github.com/chiselstrike/iku-turso-cli/internal/turso"
	"github.com/spf13/cobra"
)

var accountShowCmd = &cobra.Command{
	Use:               "show",
	Short:             "Show your current account plan.",
	Args:              cobra.NoArgs,
	ValidArgsFunction: noFilesArg,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		client, err := createTursoClient()
		if err != nil {
			return err
		}
		metrics, err := client.Metrics.Show()
		if err != nil {
			return err
		}
		fmt.Printf("%v\n", metrics)
		fmt.Printf("You are currently on %s plan.\n", turso.Emph("starter"))
		fmt.Println()
		fmt.Println("Storage: 8 GiB")
		return nil
	},
}
