package k8s

import (
	"context"
	"fmt"
	"strconv"

	"github.com/vosiander/pgsnap/pkg/common"
	"github.com/vosiander/pgsnap/pkg/postgres"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// CronJobConfig holds configuration for creating a backup CronJob
type CronJobConfig struct {
	AppIdentifier  string
	Schedule       string
	ContainerImage string
	DBConfig       *postgres.DBConfig
	S3Config       *common.S3Config
	RetentionDays  int
	DiscordWebhook string
	Suspend        bool
}

// CreateBackupCronJob creates or updates a Kubernetes CronJob for automated backups
func CreateBackupCronJob(ctx context.Context, client *kubernetes.Clientset, namespace string, config *CronJobConfig) error {
	secretName := fmt.Sprintf("pgsnap-cronbackup-%s", config.AppIdentifier)
	cronJobName := fmt.Sprintf("pgsnap-cronbackup-%s", config.AppIdentifier)

	// Create or update Secret with credentials
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "pgsnap",
				"operation": "cronbackup",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"PGPASSWORD":          config.DBConfig.Password,
			"DB_HOST":             config.DBConfig.Host,
			"DB_PORT":             strconv.Itoa(config.DBConfig.Port),
			"DB_USER":             config.DBConfig.User,
			"DB_NAME":             config.DBConfig.Database,
			"S3_ENDPOINT":         config.S3Config.Endpoint,
			"S3_BUCKET":           config.S3Config.Bucket,
			"S3_ACCESS_KEY":       config.S3Config.AccessKey,
			"S3_SECRET_KEY":       config.S3Config.SecretKey,
			"S3_PREFIX":           config.S3Config.Prefix,
			"RETENTION_DAYS":      strconv.Itoa(config.RetentionDays),
			"DISCORD_WEBHOOK_URL": config.DiscordWebhook,
			"APP_IDENTIFIER":      config.AppIdentifier,
			"NAMESPACE":           namespace,
		},
	}

	// Try to create or update secret
	_, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		// Secret doesn't exist, create it
		_, err = client.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}
	} else {
		// Secret exists, update it
		_, err = client.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}
	}

	// Create backup script
	backupScript := generateBackupScript()

	// Create or update CronJob
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cronJobName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":       "pgsnap",
				"operation": "cronbackup",
			},
		},
		Spec: batchv1.CronJobSpec{
			Schedule: config.Schedule,
			JobTemplate: batchv1.JobTemplateSpec{
				Spec: batchv1.JobSpec{
					BackoffLimit: int32Ptr(0),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyNever,
							Containers: []corev1.Container{
								{
									Name:  "backup",
									Image: config.ContainerImage,
									Command: []string{
										"/bin/sh",
										"-c",
										backupScript,
									},
									EnvFrom: []corev1.EnvFromSource{
										{
											SecretRef: &corev1.SecretEnvSource{
												LocalObjectReference: corev1.LocalObjectReference{
													Name: secretName,
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			Suspend: &config.Suspend,
		},
	}

	// Try to create or update CronJob
	_, err = client.BatchV1().CronJobs(namespace).Get(ctx, cronJobName, metav1.GetOptions{})
	if err != nil {
		// CronJob doesn't exist, create it
		_, err = client.BatchV1().CronJobs(namespace).Create(ctx, cronJob, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create cronjob: %w", err)
		}
	} else {
		// CronJob exists, update it
		_, err = client.BatchV1().CronJobs(namespace).Update(ctx, cronJob, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update cronjob: %w", err)
		}
	}

	return nil
}

// DeleteBackupCronJob deletes a backup CronJob and its Secret
func DeleteBackupCronJob(ctx context.Context, client *kubernetes.Clientset, namespace, appIdentifier string) error {
	secretName := fmt.Sprintf("pgsnap-cronbackup-%s", appIdentifier)
	cronJobName := fmt.Sprintf("pgsnap-cronbackup-%s", appIdentifier)

	// Delete CronJob
	deletePolicy := metav1.DeletePropagationForeground
	err := client.BatchV1().CronJobs(namespace).Delete(ctx, cronJobName, metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	})
	if err != nil {
		return fmt.Errorf("failed to delete cronjob: %w", err)
	}

	// Delete Secret
	err = client.CoreV1().Secrets(namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	return nil
}

// generateBackupScript generates the backup script that runs in the CronJob
func generateBackupScript() string {
	return `
set -e

# Function to send Discord notification
send_discord() {
    if [ -z "$DISCORD_WEBHOOK_URL" ]; then
        return 0
    fi
    
    local title="$1"
    local description="$2"
    local color="$3"
    local fields="$4"
    
    curl -s -X POST "$DISCORD_WEBHOOK_URL" \
        -H "Content-Type: application/json" \
        -d "{
            \"embeds\": [{
                \"title\": \"$title\",
                \"description\": \"$description\",
                \"color\": $color,
                \"fields\": $fields,
                \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"
            }]
        }"
}

# Function to handle errors
handle_error() {
    local error_msg="$1"
    local error_stage="$2"
    
    send_discord \
        "❌ Backup Failed" \
        "Database backup encountered an error" \
        15158332 \
        "[{\"name\":\"Database\",\"value\":\"$APP_IDENTIFIER\",\"inline\":true},{\"name\":\"Namespace\",\"value\":\"$NAMESPACE\",\"inline\":true},{\"name\":\"Error\",\"value\":\"$error_msg\",\"inline\":false},{\"name\":\"Failed At\",\"value\":\"$error_stage\",\"inline\":true}]"
    
    exit 1
}

START_TIME=$(date +%s)

# Step 1: Install mc (supports both apk and apt)
echo "Installing minio client..."
if command -v apk > /dev/null 2>&1; then
    apk add --no-cache curl || handle_error "Failed to install curl" "Package installation (apk)"
elif command -v apt-get > /dev/null 2>&1; then
    apt-get update && apt-get install -y curl || handle_error "Failed to install curl" "Package installation (apt)"
else
    handle_error "Neither apk nor apt-get found" "Package manager detection"
fi

# Download and install mc
curl -fsSL -o /usr/local/bin/mc https://dl.min.io/client/mc/release/linux-amd64/mc || handle_error "Failed to download mc" "mc download"
chmod +x /usr/local/bin/mc

# Verify installation
if ! command -v mc > /dev/null 2>&1; then
    handle_error "mc command not found after installation" "mc verification"
fi

echo "mc installed successfully"

# Step 2: Run pg_dump
echo "Running pg_dump..."
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_FILE="backup-$TIMESTAMP.sql.gz"

pg_dump \
    --format=plain \
    --no-owner \
    --no-acl \
    --clean \
    --if-exists \
    --host=$DB_HOST \
    --port=$DB_PORT \
    --username=$DB_USER \
    --dbname=$DB_NAME \
    --file=/tmp/dump.sql || handle_error "pg_dump failed" "pg_dump execution"

echo "pg_dump completed"

# Step 3: Compress backup
echo "Compressing backup..."
gzip /tmp/dump.sql || handle_error "Compression failed" "gzip compression"
BACKUP_SIZE=$(du -h /tmp/dump.sql.gz | cut -f1)

echo "Backup compressed: $BACKUP_SIZE"

# Step 4: Configure mc and upload to S3
echo "Configuring minio client..."
mc alias set minio $S3_ENDPOINT $S3_ACCESS_KEY $S3_SECRET_KEY || handle_error "Failed to configure mc alias" "mc configuration"

echo "Uploading to S3..."
S3_PATH="minio/$S3_BUCKET"
if [ -n "$S3_PREFIX" ]; then
    S3_PATH="$S3_PATH/$S3_PREFIX"
fi

mc cp /tmp/dump.sql.gz "$S3_PATH/$BACKUP_FILE" || handle_error "Failed to upload to S3" "S3 upload"

echo "Upload completed"

# Step 5: Cleanup old backups
echo "Cleaning up old backups (retention: $RETENTION_DAYS days)..."
mc rm --recursive --force --older-than ${RETENTION_DAYS}d "$S3_PATH/" 2>/dev/null || echo "No old backups to remove"

# Calculate duration
END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

echo "Backup completed successfully in ${DURATION}s"

# Step 6: Send success notification
send_discord \
    "✅ Backup Successful" \
    "Database backup completed successfully" \
    3066993 \
    "[{\"name\":\"Database\",\"value\":\"$APP_IDENTIFIER\",\"inline\":true},{\"name\":\"Namespace\",\"value\":\"$NAMESPACE\",\"inline\":true},{\"name\":\"Backup File\",\"value\":\"$BACKUP_FILE\",\"inline\":false},{\"name\":\"Size\",\"value\":\"$BACKUP_SIZE\",\"inline\":true},{\"name\":\"Duration\",\"value\":\"${DURATION}s\",\"inline\":true}]"

echo "All done!"
`
}
