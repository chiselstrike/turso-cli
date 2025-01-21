package cmd

import (
	"fmt"

	"github.com/dustin/go-humanize"
	"github.com/rodaine/table"
	"github.com/spf13/cobra"
	"github.com/tursodatabase/turso-cli/internal"
	"github.com/tursodatabase/turso-cli/internal/turso"
)

func init() {
	dbCmd.AddCommand(dbInspectCmd)
	addVerboseFlag(dbInspectCmd)
	addQueriesFlag(dbInspectCmd)
}

var dbInspectCmd = &cobra.Command{
	Use:               "inspect <database-name>",
	Aliases:           []string{"usage {database_name}"},
	Short:             "Inspect database.",
	Example:           "turso db inspect name-of-my-amazing-db",
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: dbNameArg,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if name == "" {
			return fmt.Errorf("please specify a database name")
		}
		cmd.SilenceUsage = true

		client, err := authedTursoClient()
		if err != nil {
			return err
		}

		if queriesFlag {
			return handleInspectQueries(client, name)
		}

		db, err := getDatabase(client, name, true)
		if err != nil {
			return err
		}

		instances, dbUsage, err := instancesAndUsage(client, db.Name)
		if err != nil {
			return err
		}

		fmt.Printf("Total space used: %s\n", humanize.Bytes(dbUsage.Usage.StorageBytesUsed))
		fmt.Printf("Number of rows read: %d\n", dbUsage.Usage.RowsRead)
		fmt.Printf("Number of rows written: %d\n", dbUsage.Usage.RowsWritten)
		if dbUsage.Usage.BytesSynced != 0 {
			fmt.Printf("Embedded syncs: %s\n", humanize.Bytes(dbUsage.Usage.BytesSynced))
		}

		if len(instances) == 0 {
			fmt.Printf("\n🛠 Run %s to finish your database creation!\n", internal.Emph("turso db replicate "+db.Name))
			return nil
		}

		if !verboseFlag {
			return nil
		}

		instancesUsage := getInstanceUsageMap(dbUsage.Instances)
		tbl := table.New("LOCATION", "TYPE", "INSTANCE NAME", "ROWS READ", "ROWS WRITTEN", "TOTAL STORAGE", "BYTES SYNCED")
		for _, instance := range instances {
			usg, ok := instancesUsage[instance.Uuid]
			if !ok {
				tbl.AddRow(instance.Region, instance.Type, instance.Name, "-", "-", "-")
				continue
			}
			tbl.AddRow(instance.Region, instance.Type, instance.Name, usg.RowsRead, usg.RowsWritten, humanize.Bytes(usg.StorageBytesUsed), humanize.Bytes(usg.BytesSynced))
		}

		fmt.Println()
		tbl.Print()
		fmt.Println()

		return nil
	},
}

func getInstanceUsageMap(usages []turso.InstanceUsage) map[string]turso.Usage {
	m := make(map[string]turso.Usage)
	for _, usg := range usages {
		m[usg.UUID] = usg.Usage
	}
	return m
}

func handleInspectQueries(client *turso.Client, database string) error {
	stats, err := client.Databases.Stats(database)
	if err != nil {
		return err
	}
	tbl := table.New("QUERY", "ROWS WRITTEN", "ROWS READ")
	for _, query := range stats {
		tbl.AddRow(query.Query, query.RowsWritten, query.RowsRead)
	}
	tbl.Print()
	fmt.Println()
	return nil
}
