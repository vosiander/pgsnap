package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/vosiander/pgsnap/pkg/archive"
	"github.com/vosiander/pgsnap/pkg/common"
	"github.com/vosiander/pgsnap/pkg/k8s"
	"github.com/vosiander/pgsnap/pkg/postgres"
	"github.com/vosiander/pgsnap/pkg/storage"
)

var (
	backupOutputDir string
	uploadS3        bool
	noCompress      bool
	postgresImage   string
	jobTimeout      int
)

var backupCmd = &cobra.Command{
	Use:   "backup <app-identifier>",
	Short: "Backup PostgreSQL database for an application",
	Long: `Backup PostgreSQL database for a Kubernetes application.

The command will:
  1. Discover the application pod in Kubernetes
  2. Extract database connection info from environment variables
  3. Create a Kubernetes Job that runs pg_dump in the cluster
  4. Download the backup from the completed Job
  5. Compress the backup into a zip file
  6. Optionally upload to S3 if configured

Examples:
  # Backup yamtrack database
  pgsnap backup yamtrack

  # Backup with specific namespace
  pgsnap backup yamtrack --namespace production

  # Backup and upload to S3
  pgsnap backup yamtrack --upload-s3

  # Backup with specific pod
  pgsnap backup --pod yamtrack-deployment-abc123`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBackup,
}

func init() {
	backupCmd.Flags().StringVarP(&backupOutputDir, "output", "o", ".backup", "Output directory for backups")
	backupCmd.Flags().BoolVar(&uploadS3, "upload-s3", false, "Upload backup to S3 after creation")
	backupCmd.Flags().BoolVar(&noCompress, "no-compress", false, "Skip compression (output .sql instead of .zip)")
	backupCmd.Flags().StringVar(&postgresImage, "image", "postgres:16-alpine", "PostgreSQL container image")
	backupCmd.Flags().IntVar(&jobTimeout, "job-timeout", 300, "Job timeout in seconds")

	rootCmd.AddCommand(backupCmd)
}

func runBackup(cmd *cobra.Command, args []string) error {
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

	fmt.Printf("🔍 Starting backup for: %s\n", appIdentifier)
	fmt.Println()

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

	// Create backup job in cluster
	fmt.Println("🚀 Creating backup job in cluster...")
	job, err := k8s.CreateBackupJob(ctx, client, restConfig, globalConfig.Namespace, dbConfig, postgresImage)
	if err != nil {
		return fmt.Errorf("failed to create backup job: %w", err)
	}
	defer job.Cleanup(ctx)

	fmt.Printf("   Job created: %s\n", job.Name())
	fmt.Println()

	// Wait for job completion
	fmt.Println("⏳ Waiting for backup job to complete...")
	timeout := time.Duration(jobTimeout) * time.Second
	if err := job.WaitForCompletion(ctx, timeout); err != nil {
		return fmt.Errorf("backup job failed: %w", err)
	}

	fmt.Println("   ✓ Backup completed in cluster")
	fmt.Println()

	// Download backup from pod
	fmt.Println("📥 Downloading backup from cluster...")
	sqlFile := filepath.Join(backupOutputDir, appIdentifier+"-backup.tar")
	if err := job.DownloadBackup(ctx, sqlFile); err != nil {
		return fmt.Errorf("failed to download backup: %w", err)
	}

	// At this point, sqlFile should be .sql (tar extraction happened in DownloadBackup)
	sqlFile = sqlFile[:len(sqlFile)-4] + ".sql"

	fmt.Println("   ✓ Backup downloaded successfully")
	fmt.Println()

	// Compress if needed
	finalFile := sqlFile
	if !noCompress {
		fmt.Println("📦 Compressing backup...")
		timestamp := storage.GenerateBackupFilename(appIdentifier)
		zipFile := filepath.Join(backupOutputDir, timestamp)

		if err := archive.Compress(sqlFile, zipFile); err != nil {
			return fmt.Errorf("compression failed: %w", err)
		}

		// Remove uncompressed SQL file
		os.Remove(sqlFile)
		finalFile = zipFile

		// Get file size
		info, _ := os.Stat(finalFile)
		fmt.Printf("   ✓ Compressed to: %s (%s)\n", filepath.Base(finalFile), storage.FormatSize(info.Size()))
		fmt.Println()
	}

	fmt.Printf("✅ Backup saved to: %s\n", finalFile)
	fmt.Println()

	// Upload to S3 if requested
	if uploadS3 {
		fmt.Println("☁️  Uploading to S3...")

		s3Config, err := common.LoadS3Config()
		if err != nil {
			return fmt.Errorf("S3 not configured: %w", err)
		}

		uploader, err := storage.NewS3Uploader(s3Config)
		if err != nil {
			return fmt.Errorf("failed to create S3 uploader: %w", err)
		}

		s3URL, err := uploader.Upload(ctx, finalFile)
		if err != nil {
			return fmt.Errorf("S3 upload failed: %w", err)
		}

		fmt.Printf("   ✓ Uploaded to: %s\n", s3URL)
		fmt.Println()
	}

	fmt.Println("🎉 Backup completed successfully!")

	return nil
}
