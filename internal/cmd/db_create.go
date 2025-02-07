package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/athoscouto/codename"
	"github.com/spf13/cobra"
	"github.com/tursodatabase/turso-cli/internal"
	"github.com/tursodatabase/turso-cli/internal/flags"
	"github.com/tursodatabase/turso-cli/internal/prompt"
	"github.com/tursodatabase/turso-cli/internal/turso"
)

func showSchemaDeprecationNotice() {
	fmt.Println(internal.Warn("Notice: Schema Databases are deprecated."))
	fmt.Println(internal.Warn("For more information, visit: https://tur.so/schema-deprecated\n"))
}

const MaxDumpFileSizeBytes = 8 << 30

func init() {
	dbCmd.AddCommand(createCmd)
	addGroupFlag(createCmd)
	addFromDBFlag(createCmd)
	addDbFromDumpFlag(createCmd)
	addDbFromDumpURLFlag(createCmd)
	addDbFromFileFlag(createCmd)
	addDbFromCSVFlag(createCmd)
	addCSVTableNameFlag(createCmd)
	flags.AddCSVSeparator(createCmd)
	addLocationFlag(createCmd, "Location ID. If no ID is specified, closest location to you is used by default.")
	addWaitFlag(createCmd, "Wait for the database to be ready to receive requests.")
	addCanaryFlag(createCmd)
	addEnableExtensionsFlag(createCmd)
	addSchemaFlag(createCmd)
	addTypeFlag(createCmd)
	addSizeLimitFlag(createCmd)
	addBetaFlag(createCmd)
}

var createCmd = &cobra.Command{
	Use:               "create [flags] [database-name]",
	Short:             "Create a database.",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: noFilesArg,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if schemaFlag != "" || typeFlag == "schema" {
			showSchemaDeprecationNotice()
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		name, err := getDatabaseName(args)
		if err != nil {
			return err
		}

		client, err := authedTursoClient()
		if err != nil {
			return err
		}

		group, isDefault, err := groupFromFlag(client)
		if err != nil {
			return err
		}

		location, err := locationFromFlag(client)
		if err != nil {
			return err
		}

		isAWS := strings.HasPrefix(group.Primary, "aws-")
		seed, err := parseDBSeedFlags(client, isAWS)
		if err != nil {
			return err
		}

		version := "latest"
		if canaryFlag {
			version = "canary"
		}

		groupName := group.Name
		if isDefault {
			groupName = "default"
		}

		if err := ensureGroup(client, groupName, location, version); err != nil {
			return err
		}

		start := time.Now()
		spinner := prompt.Spinner(fmt.Sprintf("Creating database %s in group %s...", internal.Emph(name), internal.Emph(groupName)))
		defer spinner.Stop()

		if _, err = client.Databases.Create(name, location, "", "", groupName, schemaFlag, typeFlag == "schema", seed, sizeLimitFlag, spinner); err != nil {
			return fmt.Errorf("could not create database %s: %w", name, err)
		}

		spinner.Stop()
		elapsed := time.Since(start)
		fmt.Printf("Created database %s at group %s in %s.\n\n", internal.Emph(name), internal.Emph(groupName), elapsed.Round(time.Millisecond).String())

		fmt.Printf("Start an interactive SQL shell with:\n\n")
		fmt.Printf("   %s\n\n", internal.Emph("turso db shell "+name))
		fmt.Printf("To see information about the database, including a connection URL, run:\n\n")
		fmt.Printf("   %s\n\n", internal.Emph("turso db show "+name))
		fmt.Printf("To get an authentication token for the database, run:\n\n")
		fmt.Printf("   %s\n\n", internal.Emph("turso db tokens create "+name))
		invalidateDatabasesCache()
		return nil
	},
}

func ensureGroup(client *turso.Client, group, location, version string) error {
	if ok, err := shouldCreateGroup(client, group, location); !ok {
		return err
	}
	if err := createGroup(client, group, location, version); err != nil {
		return err
	}
	return client.Groups.WaitLocation(group, location)
}

func getDatabaseName(args []string) (string, error) {
	if len(args) > 0 && len(args[0]) > 0 {
		return args[0], nil
	}

	rng, err := codename.DefaultRNG()
	if err != nil {
		return "", err
	}
	return codename.Generate(rng, 0), nil
}

// Returns (group, isDefault, error)
func groupFromFlag(client *turso.Client) (turso.Group, bool, error) {
	groups, err := getGroups(client)
	if err != nil {
		return turso.Group{}, false, err
	}

	if groupFlag != "" {
		if !groupExists(groups, groupFlag) {
			return turso.Group{}, false, fmt.Errorf("group %s does not exist", groupFlag)
		}
		for _, group := range groups {
			if group.Name == groupFlag {
				return group, false, nil
			}
		}
		return turso.Group{}, false, fmt.Errorf("group %s does not exist", groupFlag)
	}

	switch {
	case len(groups) == 0:
		return turso.Group{Name: "default"}, true, nil
	case len(groups) == 1:
		return groups[0], true, nil
	default:
		return turso.Group{}, false, fmt.Errorf("you have more than one database group. Please specify one with %s", internal.Emph("--group"))

	}
}

func groupExists(groups []turso.Group, name string) bool {
	for _, group := range groups {
		if group.Name == name {
			return true
		}
	}
	return false
}

func locationFromFlag(client *turso.Client) (string, error) {
	loc := locationFlag
	if loc == "" {
		loc, _ = closestLocation(client)
	}
	if !isValidLocation(client, loc) {
		return "", fmt.Errorf("location '%s' is not valid", loc)
	}
	return loc, nil
}

func shouldCreateGroup(client *turso.Client, name, location string) (bool, error) {
	groups, err := getGroups(client)
	if err != nil {
		return false, err
	}
	// we only create the default group automatically
	return name == "default" && len(groups) == 0, nil
}
