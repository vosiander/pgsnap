package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/vosiander/pgsnap/pkg/common"
	"github.com/vosiander/pgsnap/pkg/k8s"
	"github.com/vosiander/pgsnap/pkg/postgres"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	cronSchedule   string
	s3Endpoint     string
	s3Bucket       string
	s3AccessKey    string
	s3SecretKey    string
	s3Prefix       string
	retentionDays  int
	discordWebhook string
	suspend        bool
	deleteCronJob  bool
)

var cronbackupCmd = &cobra.Command{
	Use:   "cronbackup <app-identifier>",
	Short: "Create/update a CronJob for automated database backups to S3",
	Long: `Create or update a Kubernetes CronJob that automatically backs up a database to S3/Minio on a schedule.

The command will:
  1. Discover the application pod in Kubernetes
  2. Extract database connection info from environment variables
  3. Create a Kubernetes Secret with database and S3 credentials
  4. Create a Kubernetes CronJob that runs on the specified schedule
  5. The CronJob will:
     - Run pg_dump to backup the database
     - Compress the backup
     - Upload to S3/Minio using mc client
     - Clean up old backups based on retention policy
     - Send Discord notifications on success/failure (optional)

Examples:
  # Create a CronJob that runs daily at 2 AM (using flags)
  pgsnap cronbackup yamtrack \
    --schedule "0 2 * * *" \
    --s3-endpoint https://minio.example.com \
    --s3-bucket backups \
    --s3-access-key minioadmin \
    --s3-secret-key minioadmin

  # Using environment variables (set in .env file)
  export S3_ENDPOINT=https://minio.example.com
  export S3_BUCKET=backups
  export S3_ACCESS_KEY=minioadmin
  export S3_SECRET_KEY=minioadmin
  pgsnap cronbackup yamtrack --schedule "0 2 * * *"

  # With optional Discord notifications and custom retention
  pgsnap cronbackup yamtrack \
    --schedule "0 */6 * * *" \
    --s3-endpoint https://minio.example.com \
    --s3-bucket backups \
    --s3-access-key minioadmin \
    --s3-secret-key minioadmin \
    --s3-prefix yamtrack/ \
    --retention-days 14 \
    --discord-webhook https://discord.com/api/webhooks/...

  # Update existing CronJob schedule
  pgsnap cronbackup yamtrack --schedule "0 4 * * *"

  # Suspend CronJob
  pgsnap cronbackup yamtrack --suspend

  # Delete CronJob
  pgsnap cronbackup yamtrack --delete`,
	Args: cobra.ExactArgs(1),
	RunE: runCronbackup,
}

func init() {
	cronbackupCmd.Flags().StringVar(&cronSchedule, "schedule", "", "Cron schedule (required for create/update)")
	cronbackupCmd.Flags().StringVar(&s3Endpoint, "s3-endpoint", "", "S3/Minio endpoint URL (required for create)")
	cronbackupCmd.Flags().StringVar(&s3Bucket, "s3-bucket", "", "S3 bucket name (required for create)")
	cronbackupCmd.Flags().StringVar(&s3AccessKey, "s3-access-key", "", "S3 access key (required for create)")
	cronbackupCmd.Flags().StringVar(&s3SecretKey, "s3-secret-key", "", "S3 secret key (required for create)")
	cronbackupCmd.Flags().StringVar(&s3Prefix, "s3-prefix", "", "S3 prefix/folder for backups")
	cronbackupCmd.Flags().IntVar(&retentionDays, "retention-days", 7, "Number of days to retain backups")
	cronbackupCmd.Flags().StringVar(&discordWebhook, "discord-webhook", "", "Discord webhook URL for notifications")
	cronbackupCmd.Flags().StringVar(&postgresImage, "image", "postgres:16-alpine", "PostgreSQL container image")
	cronbackupCmd.Flags().BoolVar(&suspend, "suspend", false, "Suspend the CronJob")
	cronbackupCmd.Flags().BoolVar(&deleteCronJob, "delete", false, "Delete the CronJob")

	rootCmd.AddCommand(cronbackupCmd)
}

func runCronbackup(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	appIdentifier := args[0]

	fmt.Printf("📅 Managing CronJob for: %s\n", appIdentifier)
	fmt.Println()

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

	// Handle delete operation
	if deleteCronJob {
		fmt.Println("🗑️  Deleting CronJob...")
		err := k8s.DeleteBackupCronJob(ctx, client, globalConfig.Namespace, appIdentifier)
		if err != nil {
			return fmt.Errorf("failed to delete CronJob: %w", err)
		}
		fmt.Println("   ✓ CronJob deleted successfully")
		fmt.Println()
		fmt.Println("🎉 CronJob deletion completed!")
		return nil
	}

	// Validate required flags for create/update
	if cronSchedule == "" {
		return fmt.Errorf("--schedule is required")
	}

	// Fall back to environment variables if flags not provided
	if s3Endpoint == "" {
		s3Endpoint = os.Getenv("S3_ENDPOINT")
	}
	if s3Bucket == "" {
		s3Bucket = os.Getenv("S3_BUCKET")
	}
	if s3AccessKey == "" {
		s3AccessKey = os.Getenv("S3_ACCESS_KEY")
	}
	if s3SecretKey == "" {
		s3SecretKey = os.Getenv("S3_SECRET_KEY")
	}
	if s3Prefix == "" {
		s3Prefix = os.Getenv("S3_PREFIX")
	}
	if discordWebhook == "" {
		discordWebhook = os.Getenv("DISCORD_WEBHOOK_URL")
	}

	// Check if we're creating or updating
	cronJobName := fmt.Sprintf("pgsnap-cronbackup-%s", appIdentifier)
	_, err = client.BatchV1().CronJobs(globalConfig.Namespace).Get(ctx, cronJobName, metav1.GetOptions{})
	isUpdate := err == nil

	// For creation, require S3 credentials (from flags or env vars)
	if !isUpdate && !suspend {
		if s3Endpoint == "" || s3Bucket == "" || s3AccessKey == "" || s3SecretKey == "" {
			return fmt.Errorf("S3 credentials are required for creating a new CronJob. Provide via flags (--s3-endpoint, --s3-bucket, --s3-access-key, --s3-secret-key) or environment variables (S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY, S3_SECRET_KEY)")
		}
	}

	// For updates, get existing config if S3 credentials not provided
	var s3Config *common.S3Config
	var dbConfig *postgres.DBConfig

	if isUpdate && (s3Endpoint == "" || s3Bucket == "" || s3AccessKey == "" || s3SecretKey == "") {
		// Updating existing CronJob - get credentials from existing secret
		fmt.Println("📋 Reading existing configuration...")
		secretName := fmt.Sprintf("pgsnap-cronbackup-%s", appIdentifier)
		secret, err := client.CoreV1().Secrets(globalConfig.Namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get existing secret: %w", err)
		}

		// Use existing credentials if not provided in flags
		if s3Endpoint == "" {
			s3Endpoint = string(secret.Data["S3_ENDPOINT"])
		}
		if s3Bucket == "" {
			s3Bucket = string(secret.Data["S3_BUCKET"])
		}
		if s3AccessKey == "" {
			s3AccessKey = string(secret.Data["S3_ACCESS_KEY"])
		}
		if s3SecretKey == "" {
			s3SecretKey = string(secret.Data["S3_SECRET_KEY"])
		}
		if s3Prefix == "" {
			s3Prefix = string(secret.Data["S3_PREFIX"])
		}

		dbConfig = &postgres.DBConfig{
			Host:     string(secret.Data["DB_HOST"]),
			Port:     parseIntOrDefault(string(secret.Data["DB_PORT"]), 5432),
			User:     string(secret.Data["DB_USER"]),
			Password: string(secret.Data["PGPASSWORD"]),
			Database: string(secret.Data["DB_NAME"]),
		}
	} else {
		// Discover pod and extract database credentials
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
		dbConfig, err = postgres.ParseFromEnvVars(envVars)
		if err != nil {
			return fmt.Errorf("failed to parse database config: %w", err)
		}

		fmt.Printf("   Host: %s:%d\n", dbConfig.Host, dbConfig.Port)
		fmt.Printf("   Database: %s\n", dbConfig.Database)
		fmt.Printf("   User: %s\n", dbConfig.User)
		fmt.Println()
	}

	// Create S3 config
	s3Config = &common.S3Config{
		Endpoint:  s3Endpoint,
		Bucket:    s3Bucket,
		Prefix:    s3Prefix,
		AccessKey: s3AccessKey,
		SecretKey: s3SecretKey,
	}

	// Create CronJob config
	cronJobConfig := &k8s.CronJobConfig{
		AppIdentifier:  appIdentifier,
		Schedule:       cronSchedule,
		ContainerImage: postgresImage,
		DBConfig:       dbConfig,
		S3Config:       s3Config,
		RetentionDays:  retentionDays,
		DiscordWebhook: discordWebhook,
		Suspend:        suspend,
	}

	// Create or update CronJob
	if isUpdate {
		fmt.Println("🔄 Updating existing CronJob...")
	} else {
		fmt.Println("🚀 Creating new CronJob...")
	}

	err = k8s.CreateBackupCronJob(ctx, client, globalConfig.Namespace, cronJobConfig)
	if err != nil {
		return fmt.Errorf("failed to create/update CronJob: %w", err)
	}

	fmt.Println("   ✓ CronJob configured successfully")
	fmt.Println()

	// Display configuration summary
	fmt.Println("📊 CronJob Configuration:")
	fmt.Printf("   Name: pgsnap-cronbackup-%s\n", appIdentifier)
	fmt.Printf("   Schedule: %s\n", cronSchedule)
	fmt.Printf("   PostgreSQL Image: %s\n", postgresImage)
	fmt.Printf("   S3 Endpoint: %s\n", s3Endpoint)
	fmt.Printf("   S3 Bucket: %s\n", s3Bucket)
	if s3Prefix != "" {
		fmt.Printf("   S3 Prefix: %s\n", s3Prefix)
	}
	fmt.Printf("   Retention: %d days\n", retentionDays)
	if discordWebhook != "" {
		fmt.Printf("   Discord Notifications: Enabled\n")
	} else {
		fmt.Printf("   Discord Notifications: Disabled\n")
	}
	if suspend {
		fmt.Printf("   Status: Suspended\n")
	} else {
		fmt.Printf("   Status: Active\n")
	}
	fmt.Println()

	fmt.Println("🎉 CronJob setup completed!")
	fmt.Println()
	fmt.Println("💡 Next steps:")
	fmt.Println("   - View CronJob: kubectl get cronjob -n", globalConfig.Namespace)
	fmt.Println("   - View Secret: kubectl get secret pgsnap-cronbackup-"+appIdentifier, "-n", globalConfig.Namespace)
	fmt.Println("   - Trigger manual run: kubectl create job --from=cronjob/pgsnap-cronbackup-"+appIdentifier, "manual-backup-$(date +%s) -n", globalConfig.Namespace)

	return nil
}

// Helper function to parse int from string with default
func parseIntOrDefault(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	var val int
	fmt.Sscanf(s, "%d", &val)
	if val == 0 {
		return defaultVal
	}
	return val
}
