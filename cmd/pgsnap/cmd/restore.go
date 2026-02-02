package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vosiander/pgsnap/pkg/archive"
	"github.com/vosiander/pgsnap/pkg/k8s"
	"github.com/vosiander/pgsnap/pkg/postgres"
)

var (
	restoreFile  string
	forceRestore bool
)

var restoreCmd = &cobra.Command{
	Use:   "restore <app-identifier>",
	Short: "Restore PostgreSQL database from backup",
	Long: `Restore PostgreSQL database from a backup file.

The command will:
  1. Discover the application pod in Kubernetes
  2. Extract database connection info from environment variables
  3. Extract the backup from zip file
  4. Restore the database using psql

WARNING: This will overwrite the current database!

Examples:
  # Restore yamtrack database
  pgsnap restore yamtrack --file .backup/yamtrack-2026-01-11-backup.zip

  # Restore without confirmation prompt
  pgsnap restore yamtrack --file backup.zip --force

  # Restore with specific pod
  pgsnap restore --pod yamtrack-deployment-abc123 --file backup.zip`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRestore,
}

func init() {
	restoreCmd.Flags().StringVarP(&restoreFile, "file", "f", "", "Backup file to restore (required)")
	restoreCmd.Flags().BoolVar(&forceRestore, "force", false, "Skip confirmation prompt")
	restoreCmd.MarkFlagRequired("file")

	rootCmd.AddCommand(restoreCmd)
}

func runRestore(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Get app identifier (or use pod name if specified)
	var appIdentifier string
	var err error
	if podName != "" {
		appIdentifier = "custom"
	} else {
		appIdentifier, err = getAppIdentifier(args)
		if err != nil {
			return err
		}
	}

	fmt.Printf("🔍 Starting restore for: %s\n", appIdentifier)
	fmt.Println()

	// Validate backup file exists
	if _, err := os.Stat(restoreFile); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", restoreFile)
	}

	// Confirmation prompt
	if !forceRestore {
		fmt.Printf("⚠️  WARNING: This will overwrite the database for %s!\n", appIdentifier)
		fmt.Print("   Continue? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response != "y" && response != "yes" {
			fmt.Println("   Restore cancelled.")
			return nil
		}
		fmt.Println()
	}

	// Create Kubernetes client
	fmt.Println("📦 Connecting to Kubernetes...")
	client, _, defaultNamespace, err := k8s.NewClient(globalConfig.Kubeconfig, globalConfig.Context)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Use default namespace if not specified
	if globalConfig.Namespace == "" {
		globalConfig.Namespace = defaultNamespace
	}

	fmt.Printf("   Namespace: %s\n", globalConfig.Namespace)
	fmt.Println()

	// Discover pod
	fmt.Println("🔎 Discovering pod...")
	discovery := k8s.NewDiscovery(client, globalConfig.Namespace, appIdentifier, podName)
	pod, err := discovery.FindPod(ctx)
	if err != nil {
		return fmt.Errorf("failed to discover pod: %w", err)
	}

	fmt.Printf("   Found pod: %s\n", pod.Name)
	fmt.Println()

	// Extract environment variables
	fmt.Println("🔐 Extracting database configuration...")
	envVars, err := k8s.ExtractEnvVars(ctx, client, pod)
	if err != nil {
		return fmt.Errorf("failed to extract environment variables: %w", err)
	}

	// Parse database config
	dbConfig, err := postgres.ParseFromEnvVars(envVars)
	if err != nil {
		return fmt.Errorf("failed to parse database config: %w", err)
	}

	fmt.Printf("   Host: %s:%d\n", dbConfig.Host, dbConfig.Port)
	fmt.Printf("   Database: %s\n", dbConfig.Database)
	fmt.Printf("   User: %s\n", dbConfig.User)
	fmt.Println()

	// Extract backup file
	fmt.Println("📦 Extracting backup...")
	tmpDir := os.TempDir()
	sqlFile := filepath.Join(tmpDir, "restore-temp.sql")

	// Check if file is already SQL or needs decompression
	if strings.HasSuffix(restoreFile, ".zip") {
		if err := archive.Decompress(restoreFile, sqlFile); err != nil {
			return fmt.Errorf("failed to extract backup: %w", err)
		}
		defer os.Remove(sqlFile) // Clean up temp file
	} else {
		// Assume it's already a SQL file
		sqlFile = restoreFile
	}

	fmt.Println("   ✓ Backup extracted")
	fmt.Println()

	// Restore database
	fmt.Println("♻️  Restoring database...")
	restoreOpts := postgres.RestoreOptions{
		PsqlPath:  globalConfig.PsqlPath,
		InputFile: sqlFile,
		DBConfig:  dbConfig,
	}

	if err := postgres.Restore(restoreOpts); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	fmt.Println("   ✓ Database restored successfully")
	fmt.Println()

	fmt.Println("🎉 Restore completed successfully!")

	return nil
}
