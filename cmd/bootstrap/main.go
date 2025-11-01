package main

import (
	"fmt"
	"log"
	"maintainerd/db"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	apiTokenEnvVar             = "FOSSA_API_TOKEN" //nolint:gosec
	spreadsheetEnvVar          = "MD_WORKSHEET"
	googleWorkspaceCredentials = "WORKSPACE_CREDENTIALS_FILE" //nolint:gosec
	defaultDBPath              = "maintainers.db"
	defaultMaxBackups          = 5
	backupFileExt              = ".bak"
)

func main() {
	var dbPath string
	var seed bool
	var doBackup bool
	var maxBackups int

	rootCmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Bootstrap the database schema and optionally seed it",
		Run: func(cmd *cobra.Command, args []string) {
			spreadsheetID := viper.GetString(spreadsheetEnvVar)
			if spreadsheetID == "" {
				log.Fatalf("ERROR: environment variable %s is not set", spreadsheetEnvVar)
			}

			fossaToken := viper.GetString(apiTokenEnvVar)
			if fossaToken == "" {
				log.Fatalf("ERROR: environment variable %s is not set", apiTokenEnvVar)
			}

			credentialsPath := viper.GetString(googleWorkspaceCredentials)
			if credentialsPath == "" {
				log.Fatalf("ERROR: environment variable %s is not set", googleWorkspaceCredentials)
			}
			if doBackup {
				if _, err := os.Stat(dbPath); err == nil {
					info, err := os.Stat(dbPath)
					if err == nil {
						log.Printf("existing database file size: %d bytes", info.Size())
					}
					backupPath := fmt.Sprintf("%s.%s%s", dbPath, time.Now().Format("20060102-150405"), backupFileExt)
					err = copyFile(dbPath, backupPath)
					if err != nil {
						log.Fatalf("failed to create DB backup: %v", err)
					}
					log.Printf("existing database backed up to %s", backupPath)
					pruneOldBackups(dbPath, maxBackups)
				}
			}
			_, err := db.BootstrapSQLite(dbPath, spreadsheetID, credentialsPath, fossaToken, seed)
			if err != nil {
				log.Fatalf("bootstrap failed: %v", err)
			}

		},
	}

	rootCmd.Flags().StringVar(&dbPath, "db", defaultDBPath, "Path to SQLite database file")
	rootCmd.Flags().BoolVar(&seed, "seed", true, "Whether to load seed data into the database")
	rootCmd.Flags().BoolVar(&doBackup, "backup", true, "Whether to create a backup of the database if it exists")
	rootCmd.Flags().IntVar(&maxBackups, "max-backups", defaultMaxBackups, "Maximum number of backups to retain")

	viper.AutomaticEnv() // binds environment variables to viper config

	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("command failed: %v", err)
	}
}
func copyFile(src, dst string) error {
	sourceFileStat, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !sourceFileStat.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}

	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func(destination *os.File) {
		err := destination.Close()
		if err != nil {
			log.Printf("warning: failed to close file %s: %v", dst, err)
		}
	}(destination)

	_, err = destination.ReadFrom(source)
	return err
}
func pruneOldBackups(dbPath string, max int) {
	dir := filepath.Dir(dbPath)
	base := filepath.Base(dbPath)
	prefix := base + "."
	files, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("warning: failed to read backup directory: %v", err)
		return
	}

	var backups []string
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) && strings.HasSuffix(f.Name(), backupFileExt) {
			backups = append(backups, filepath.Join(dir, f.Name()))
		}
	}

	if len(backups) <= max {
		return
	}

	sort.Strings(backups)
	toRemove := backups[:len(backups)-max]
	for _, file := range toRemove {
		err := os.Remove(file)
		if err != nil {
			log.Printf("warning: failed to remove old backup %s: %v", file, err)
		} else {
			log.Printf("removed old backup: %s", file)
		}
	}
}
