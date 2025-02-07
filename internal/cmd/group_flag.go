package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Clever/csvlint"
	"github.com/spf13/cobra"
	"github.com/tursodatabase/turso-cli/internal/flags"
	"github.com/tursodatabase/turso-cli/internal/prompt"
	"github.com/tursodatabase/turso-cli/internal/turso"
)

var groupBoolFlag bool

func addGroupBoolFlag(cmd *cobra.Command, description string) {
	cmd.Flags().BoolVar(&groupBoolFlag, "group", false, description)
}

var groupFlag string

func addGroupFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&groupFlag, "group", "", "create the database in the specified group")
	cmd.RegisterFlagCompletionFunc("group", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		client, err := authedTursoClient()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		groups, _ := groupNames(client)
		return groups, cobra.ShellCompDirectiveNoFileComp
	})
}

var (
	fromDBFlag    string
	timestampFlag string
)

func addFromDBFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&fromDBFlag, "from-db", "", "Select another database to copy data from. To use data from a past version of the selected database, see the 'timestamp' flag.")
	cmd.RegisterFlagCompletionFunc("from-db", dbNameArg)
	cmd.Flags().StringVar(&timestampFlag, "timestamp", "", "Set a point in time in the past to copy data from the selected database. Must be used with the 'from-db' flag. Must be in RFC3339 format like '2023-09-29T10:16:13-03:00'")
}

func parseTimestampFlag() (*time.Time, error) {
	if timestampFlag == "" {
		return nil, nil
	}
	if fromDBFlag == "" {
		return nil, fmt.Errorf("--timestamp cannot be used without specifying --from-db")
	}

	timestamp, err := time.Parse(time.RFC3339, timestampFlag)
	if err != nil {
		return nil, fmt.Errorf("provided timestamp was not in RFC3339 format like '2023-09-29T10:16:13-03:00'")
	}
	return &timestamp, nil
}

func parseDBSeedFlags(client *turso.Client, isAWS bool) (*turso.DBSeed, error) {
	if countFlags(fromDBFlag, fromDumpFlag, fromFileFlag, fromDumpURLFlag, fromCSVFlag) > 1 {
		return nil, fmt.Errorf("only one of --from prefixed flags can be used at a time")
	}

	timestamp, err := parseTimestampFlag()
	if err != nil {
		return nil, err
	}
	if fromCSVFlag != "" && csvTableNameFlag == "" {
		return nil, fmt.Errorf("--csv-table-name must be used with --from-csv")
	}
	if csvTableNameFlag != "" && fromCSVFlag == "" {
		return nil, fmt.Errorf("--from-csv must be used with --csv-table-name")
	}

	if fromDBFlag != "" {
		return &turso.DBSeed{Type: "database", Name: fromDBFlag, Timestamp: timestamp}, nil
	}

	if fromFileFlag != "" {
		return handleDBFile(client, fromFileFlag, isAWS)
	}

	if fromDumpFlag != "" {
		return handleDumpFile(client, fromDumpFlag)
	}

	if fromCSVFlag != "" {
		csvSeparator, err := flags.CSVSeparator()
		if err != nil {
			return nil, err
		}
		return handleCSVFile(client, fromCSVFlag, csvTableNameFlag, csvSeparator)
	}
	if fromDumpURLFlag != "" {
		return handleDumpURL(fromDumpURLFlag)
	}

	return nil, nil
}

func handleDumpURL(url string) (*turso.DBSeed, error) {
	return &turso.DBSeed{
		Type: "dump",
		URL:  url,
	}, nil
}

func handleDumpFile(client *turso.Client, file string) (*turso.DBSeed, error) {
	dump, err := validateDumpFile(file)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	spinner := prompt.Spinner("Uploading data...")
	defer spinner.Stop()

	dumpURL, err := client.Databases.UploadDump(dump)
	if err != nil {
		return nil, fmt.Errorf("could not upload dump: %w", err)
	}

	spinner.Stop()
	elapsed := time.Since(start)
	fmt.Printf("Uploaded data in %d seconds.\n\n", int(elapsed.Seconds()))

	return handleDumpURL(dumpURL)
}

func validateDumpFile(name string) (*os.File, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("could not open file %s: %w", name, err)
	}
	fileStat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("could not stat file %s: %w", name, err)
	}
	if fileStat.Size() == 0 {
		return nil, fmt.Errorf("dump file is empty")
	}
	if fileStat.Size() > MaxDumpFileSizeBytes {
		return nil, fmt.Errorf("dump file is too large. max allowed size is 2GB")
	}
	if err := checkDumpFileFirstLines(name, file); err != nil {
		return nil, fmt.Errorf("invalid dump file: %w", err)
	}
	return file, nil
}

func checkDumpFileFirstLines(name string, file *os.File) error {
	scanner := bufio.NewScanner(file)
	scanner.Scan()
	if scanner.Text() != "PRAGMA foreign_keys=OFF;" {
		if checkSQLiteFile(name) == nil {
			return fmt.Errorf("you're trying to use a SQLite database file as a dump. Use the --from-db flag instead of --from-dump")
		}
		return fmt.Errorf("file doesn't look like a dump: first line should be 'PRAGMA foreign_keys=OFF;'")
	}

	scanner.Scan()
	if scanner.Text() != "BEGIN TRANSACTION;" {
		if checkSQLiteFile(name) == nil {
			return fmt.Errorf("you're trying to use a SQLite database file as a dump. Use --from-db instead")
		}
		return fmt.Errorf("file doesn't look like a dump: second line should be 'BEGIN TRANSACTION;'")
	}

	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("could not seek to the beginning of dump after validating it: %w", err)
	}
	return nil
}

func countFlags(flags ...string) (count int) {
	for _, flag := range flags {
		if flag != "" {
			count++
		}
	}
	return
}

const MaxAWSDBSizeBytes = 1024 * 1024 * 1024 * 20 // 20 GB
func sqliteFileIntegrityChecks(file string) error {
	log.Printf("Running integrity checks on database file %s", file)
	log.Printf("Checking file size...")
	fileInfo, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	if fileInfo.Size() > MaxAWSDBSizeBytes {
		return fmt.Errorf("database file size exceeds maximum allowed size of 20 GB")
	}

	log.Printf("Checking database settings...")
	output, err := exec.Command("sqlite3", file, ".mode line",
		"select journal_mode as j, page_size as p, auto_vacuum as a, encoding as e from pragma_journal_mode, pragma_page_size, pragma_auto_vacuum, pragma_encoding;").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to check database settings: %w", err)
	}

	settings := string(output)
	if !strings.Contains(settings, "j = wal") {
		log.Printf("Database is not in WAL mode, setting to WAL...")
		// set to WAL
		_, err = exec.Command("sqlite3", file, "PRAGMA journal_mode = WAL;").CombinedOutput()
		if err != nil {
			return fmt.Errorf("database is not in WAL mode, and we failed to set it: %w", err)
		}
		// run truncating checkpoint
		log.Printf("Running checkpoint...")
		_, err = exec.Command("sqlite3", file, "pragma wal_checkpoint(TRUNCATE);").CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to run truncating checkpoint after setting journal mode to WAL: %w", err)
		}
	}
	if !strings.Contains(settings, "p = 4096") {
		return fmt.Errorf("database must use 4KB page size. you can set it with 'sqlite3 yourdb.db 'PRAGMA page_size = 4096'")
	}
	if !strings.Contains(settings, "a = 0") {
		return fmt.Errorf("database must have autovacuum disabled. you can set it with 'sqlite3 yourdb.db 'PRAGMA auto_vacuum = 0'")
	}
	if !strings.Contains(settings, "e = UTF-8") {
		return fmt.Errorf("database must use UTF-8 encoding. you can set it with 'sqlite3 yourdb.db 'PRAGMA encoding = 'UTF-8'	")
	}

	// run quick_check
	log.Printf("Running integrity check...")
	_, err = exec.Command("sqlite3", file, "pragma quick_check;").CombinedOutput()
	if err != nil {
		return fmt.Errorf("integrity check on database failed: %w", err)
	}

	return nil
}

func handleDBFileAWS(file string) (*turso.DBSeed, error) {
	if err := sqliteFileIntegrityChecks(file); err != nil {
		return nil, err
	}

	seed := &turso.DBSeed{
		Type:     "database_upload",
		Filepath: file,
	}

	return seed, nil
}

func handleDBFile(client *turso.Client, file string, isAWS bool) (*turso.DBSeed, error) {
	if err := checkFileExists(file); err != nil {
		return nil, err
	}
	if err := checkSQLiteAvailable(); err != nil {
		return nil, err
	}

	if isAWS {
		return handleDBFileAWS(file)
	}

	if err := checkSQLiteFile(file); err != nil {
		return nil, err
	}

	tmp, err := createTempFile()
	if err != nil {
		return nil, err
	}

	if err := dumpSQLiteDatabase(file, tmp); err != nil {
		return nil, err
	}

	return handleDumpFile(client, tmp.Name())
}

func checkFileExists(file string) error {
	_, err := os.Stat(file)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("could not find file %s", file)
	}
	return err
}

func checkSQLiteAvailable() error {
	_, err := exec.LookPath("sqlite3")
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("could not find sqlite3 on your system. Please install it to use the --from-file flag or use --from-dump instead")
	}
	return err
}

func checkSQLiteFile(file string) error {
	output, err := exec.Command("sqlite3", file, "pragma quick_check;").CombinedOutput()

	execErr := &exec.ExitError{}
	if errors.As(err, &execErr) && execErr.ExitCode() == 26 {
		return fmt.Errorf("file %s is not a valid SQLite database file", file)
	}

	if err != nil {
		return fmt.Errorf("could not check database file: %w: %s", err, output)
	}
	return nil
}

func createTempFile() (*os.File, error) {
	tmp, err := os.CreateTemp("", "")
	if err != nil {
		return nil, fmt.Errorf("could not create temporary file to dump database file: %w", err)
	}
	return tmp, nil
}

func dumpSQLiteDatabase(database string, dump *os.File) error {
	stdErr := &bytes.Buffer{}
	cmd := exec.Command("sqlite3", database, ".dump")
	cmd.Stdout = dump
	cmd.Stderr = stdErr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not dump database file: %w: %x", err, stdErr.Bytes())
	}

	return nil
}

func handleCSVFile(client *turso.Client, file, csvTableName string, separator rune) (*turso.DBSeed, error) {
	if err := checkFileExists(file); err != nil {
		return nil, err
	}
	if err := checkSQLiteAvailable(); err != nil {
		return nil, err
	}

	csvFile, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("could not open CSV file: %w", err)
	}
	defer csvFile.Close()

	errs, invalid, _ := csvlint.Validate(csvFile, separator, false)
	if invalid {
		return nil, fmt.Errorf("CSV file is not valid: %+v", errs)
	}

	tempDB, err := os.CreateTemp("", "")
	if err != nil {
		return nil, fmt.Errorf("could not create temporary file to dump database file: %w", err)
	}
	defer os.Remove(tempDB.Name())

	err = importCSVIntoSQLite(tempDB, csvFile.Name(), csvTableName, separator)
	if err != nil {
		return nil, err
	}

	seed, err := handleDBFile(client, tempDB.Name(), false)
	if err != nil {
		return nil, err
	}
	return seed, nil
}

func importCSVIntoSQLite(tempDB *os.File, csvFile, csvTableName string, separator rune) error {
	stdErr := &bytes.Buffer{}
	cmd := exec.Command("sqlite3", "-csv", tempDB.Name(), fmt.Sprintf(".separator %c", separator), fmt.Sprintf(".import %s %s", csvFile, csvTableName))
	cmd.Stderr = stdErr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not load csv into new database file: %w: %x", err, stdErr.Bytes())
	}
	return nil
}
