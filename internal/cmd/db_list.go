package cmd

import (
	"sort"

	"github.com/spf13/cobra"
)

func init() {
	dbCmd.AddCommand(listCmd)
}

var listCmd = &cobra.Command{
	Use:               "list",
	Short:             "List databases.",
	Args:              cobra.NoArgs,
	ValidArgsFunction: noFilesArg,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		client, err := createTursoClientFromAccessToken(true)
		if err != nil {
			return err
		}
		databases, err := client.Databases.List()
		if err != nil {
			return err
		}
		setDatabasesCache(databases)
		var data [][]string
		for _, database := range databases {
			data = append(data, []string{
				database.Name,
				getDatabaseRegions(database),
				getDatabaseUrl(&database)},
			)
		}

		sort.Slice(data, func(i, j int) bool {
			return data[i][0] > data[j][0]
		})

		printTable([]string{"Name", "Locations", "URL"}, data)
		return nil
	},
}
