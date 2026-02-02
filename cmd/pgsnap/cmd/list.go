package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/vosiander/pgsnap/pkg/storage"
)

var listCmd = &cobra.Command{
	Use:   "list <app-identifier>",
	Short: "List available backups for an application",
	Long: `List all available backup files for an application.

Shows backup filename, size, and modification time.

Examples:
  # List backups for yamtrack
  pgsnap list yamtrack

  # List backups in custom directory
  pgsnap list yamtrack --output /path/to/backups`,
	Args: cobra.ExactArgs(1),
	RunE: runList,
}

func init() {
	listCmd.Flags().StringVarP(&backupOutputDir, "output", "o", ".backup", "Backup directory to search")

	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	appIdentifier := args[0]

	fmt.Printf("📋 Listing backups for: %s\n", appIdentifier)
	fmt.Printf("   Directory: %s\n", backupOutputDir)
	fmt.Println()

	// List backups
	backups, err := storage.ListBackups(backupOutputDir, appIdentifier)
	if err != nil {
		return fmt.Errorf("failed to list backups: %w", err)
	}

	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].ModTime.After(backups[j].ModTime)
	})

	// Display backups
	fmt.Printf("Found %d backup(s):\n\n", len(backups))

	for _, backup := range backups {
		fmt.Printf("  📦 %s\n", backup.Filename)
		fmt.Printf("     Size: %s\n", storage.FormatSize(backup.Size))
		fmt.Printf("     Date: %s\n", backup.ModTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("     Path: %s\n", backup.Path)
		fmt.Println()
	}

	return nil
}
