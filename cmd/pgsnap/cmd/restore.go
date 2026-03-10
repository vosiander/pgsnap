package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vosiander/pgsnap/pkg/archive"
	"github.com/vosiander/pgsnap/pkg/k8s"
	"github.com/vosiander/pgsnap/pkg/postgres"
	"github.com/vosiander/pgsnap/pkg/storage"
)

var (
	restoreFile       string
	forceRestore      bool
	restoreImage      string
	restoreJobTimeout int
)

var restoreCmd = &cobra.Command{
	Use:   "restore <app-identifier>",
	Short: "Restore PostgreSQL database from backup",
	Long: `Restore PostgreSQL database from a backup file.

The command will:
  1. Discover the application pod in Kubernetes
  2. Extract database connection info from environment variables
  3. Create a Kubernetes Job that runs psql in the cluster
  4. Upload the backup data to the cluster
  5. Restore the database using psql

WARNING: This will overwrite the current database!

Examples:
  # Restore yamtrack database
  pgsnap restore yamtrack --file backups/backup-2026-01-11.sql.gz

  # Restore without confirmation prompt
  pgsnap restore yamtrack --file backup.sql --force

  # Restore with specific pod
  pgsnap restore --pod yamtrack-deployment-abc123 --file backup.sql`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRestore,
}

func init() {
	restoreCmd.Flags().StringVarP(&restoreFile, "file", "f", "", "Backup file to restore (required)")
	restoreCmd.Flags().BoolVar(&forceRestore, "force", false, "Skip confirmation prompt")
	restoreCmd.Flags().StringVar(&restoreImage, "image", "postgres:16-alpine", "PostgreSQL container image")
	restoreCmd.Flags().IntVar(&restoreJobTimeout, "job-timeout", 600, "Job timeout in seconds")
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
	client, restConfig, defaultNamespace, err := k8s.NewClient(globalConfig.Kubeconfig, globalConfig.Context)
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

	// Read backup file content
	fmt.Println("📦 Reading backup file...")
	backupData, err := archive.ReadFileContent(restoreFile)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}

	fmt.Printf("   ✓ Backup loaded (%s)\n", storage.FormatSize(int64(len(backupData))))
	fmt.Println()

	// Create restore job in cluster
	fmt.Println("🚀 Creating restore job in cluster...")
	job, err := k8s.CreateRestoreJob(ctx, client, restConfig, globalConfig.Namespace, dbConfig, backupData, restoreImage)
	if err != nil {
		return fmt.Errorf("failed to create restore job: %w", err)
	}
	defer job.Cleanup(ctx)

	fmt.Printf("   Job created: %s\n", job.Name())
	fmt.Println()

	// Wait for job completion
	fmt.Println("⏳ Waiting for restore job to complete...")
	timeout := time.Duration(restoreJobTimeout) * time.Second
	if err := job.WaitForCompletion(ctx, timeout); err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	fmt.Println("   ✓ Database restored successfully")
	fmt.Println()

	fmt.Println("🎉 Restore completed successfully!")

	return nil
}
