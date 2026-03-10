# pgsnap

A CLI tool for backing up and restoring PostgreSQL databases running in Kubernetes clusters. It automatically discovers pods and extracts database connection information from environment variables.

## Features

- 🔍 **Automatic Pod Discovery** - Finds pods using labels or name patterns
- 🔐 **Smart Config Extraction** - Reads database credentials from environment variables
- 📦 **Compression** - Automatically compresses backups with zip
- ☁️ **S3 Upload** - Optional upload to S3/MinIO after backup
- 🧪 **In-Cluster SQL Jobs** - Run ad hoc SQL and inspect results via Kubernetes Job logs
- 🛡️ **Safety** - Interactive confirmation prompts for restores
- 🎯 **Generic** - Works with any Kubernetes app using PostgreSQL

## Installation

### Homebrew (macOS/Linux)

```bash
brew tap vosiander/tap
brew install --cask pgsnap
```

### Install Script

```bash
curl -sSL https://raw.githubusercontent.com/vosiander/pgsnap/main/install.sh | bash
```

### Go Install

```bash
go install github.com/vosiander/pgsnap/cmd/pgsnap@latest
```

### From Source

```bash
git clone https://github.com/vosiander/pgsnap
cd pgsnap
go build -o pgsnap ./cmd/pgsnap
```

## Prerequisites

- `pg_dump` and `psql` must be installed and available in PATH
- Access to a Kubernetes cluster (kubeconfig configured)
- PostgreSQL database running in Kubernetes

## Quick Start

### Backup

```bash
# Basic backup
pgsnap backup yamtrack

# Backup with specific namespace
pgsnap backup yamtrack --namespace production

# Backup and upload to S3
pgsnap backup yamtrack --upload-s3
```

### Restore

```bash
# Restore from backup (with confirmation prompt)
pgsnap restore yamtrack --file .backup/yamtrack-2026-01-11-backup.zip

# Force restore without prompt
pgsnap restore yamtrack --file backup.zip --force
```

### SQL

```bash
# List databases
pgsnap sql yamtrack --sql "SELECT datname FROM pg_database;"

# Inspect table rows
pgsnap sql yamtrack --sql "SELECT * FROM users LIMIT 20;"
```

### List Backups

```bash
# List all backups for an app
pgsnap list yamtrack
```

### Show Info

```bash
# Debug: show discovered pod and database config
pgsnap info yamtrack
```

## Usage

### Global Flags

- `--kubeconfig` - Path to kubeconfig file (default: `~/.kube/config`)
- `--context` - Kubernetes context to use
- `--namespace, -n` - Kubernetes namespace
- `--pod` - Exact pod name (skips auto-discovery)

### Backup Command

```bash
pgsnap backup <app-identifier> [flags]
```

**Flags:**
- `--output, -o` - Output directory (default: `.backup`)
- `--upload-s3` - Upload to S3 after backup
- `--no-compress` - Skip compression (output .sql instead of .zip)

**Examples:**
```bash
# Backup yamtrack
pgsnap backup yamtrack

# Backup tinyauth in production namespace
pgsnap backup tinyauth --namespace production

# Backup with custom output directory
pgsnap backup keycloak --output ./backups

# Backup and upload to S3
pgsnap backup yamtrack --upload-s3

# Backup specific pod
pgsnap backup --pod yamtrack-deployment-abc123
```

### Restore Command

```bash
pgsnap restore <app-identifier> --file <backup-file> [flags]
```

**Flags:**
- `--file, -f` - Backup file to restore (required)
- `--force` - Skip confirmation prompt

**Examples:**
```bash
# Restore yamtrack (with confirmation)
pgsnap restore yamtrack --file .backup/yamtrack-2026-01-11-backup.zip

# Restore without confirmation
pgsnap restore yamtrack --file backup.zip --force

# Restore to specific namespace
pgsnap restore yamtrack --file backup.zip --namespace staging
```

### List Command

```bash
pgsnap list <app-identifier> [flags]
```

**Flags:**
- `--output, -o` - Backup directory to search (default: `.backup`)

**Examples:**
```bash
# List backups for yamtrack
pgsnap list yamtrack

# List backups in custom directory
pgsnap list yamtrack --output /path/to/backups
```

### SQL Command

```bash
pgsnap sql <app-identifier> [--sql "SELECT 1"] [--file query.sql]
```

Runs SQL in a Kubernetes Job using the discovered database configuration. The Job and SQL ConfigMap are preserved after execution so you can inspect output with `kubectl logs`.

**Flags:**
- `--sql` - Inline SQL to execute
- `--file, -f` - Path to a SQL file to execute
- `--image` - PostgreSQL container image (default: `postgres:16-alpine`)
- `--job-timeout` - Timeout in seconds to wait for Job completion (default: `300`)

**Examples:**
```bash
# List databases
pgsnap sql yamtrack --sql "SELECT datname FROM pg_database;"

# Inspect table data
pgsnap sql yamtrack --sql "SELECT id, email FROM users LIMIT 20;"

# Run a SQL script
pgsnap sql yamtrack --file ./query.sql

# Pipe SQL from stdin
cat ./query.sql | pgsnap sql yamtrack
```

### Info Command

```bash
pgsnap info <app-identifier> [flags]
```

Shows discovered pod, database configuration, and connection details. Useful for debugging.

**Examples:**
```bash
# Show info for yamtrack
pgsnap info yamtrack

# Show info for specific pod
pgsnap info --pod yamtrack-deployment-abc123
```

### CronBackup Command

```bash
pgsnap cronbackup <app-identifier> [flags]
```

Creates or updates a Kubernetes CronJob for automated database backups to S3/Minio. The CronJob runs on a schedule and includes automatic retention management and optional Discord notifications.

**Flags:**
- `--schedule` - Cron schedule (required, e.g., "0 2 * * *")
- `--s3-endpoint` - S3/Minio endpoint URL (required for create)
- `--s3-bucket` - S3 bucket name (required for create)
- `--s3-access-key` - S3 access key (required for create)
- `--s3-secret-key` - S3 secret key (required for create)
- `--s3-prefix` - S3 prefix/folder for backups (optional)
- `--retention-days` - Number of days to retain backups (default: 7)
- `--discord-webhook` - Discord webhook URL for notifications (optional)
- `--container-image` - PostgreSQL container image (default: "postgres:16-alpine")
- `--suspend` - Suspend the CronJob
- `--delete` - Delete the CronJob and Secret

**Examples:**
```bash
# Create daily backup at 2 AM
pgsnap cronbackup yamtrack \
  --schedule "0 2 * * *" \
  --s3-endpoint https://minio.example.com \
  --s3-bucket backups \
  --s3-access-key minioadmin \
  --s3-secret-key minioadmin

# With Discord notifications and custom retention
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
pgsnap cronbackup yamtrack --delete

# Trigger manual backup from CronJob
kubectl create job --from=cronjob/pgsnap-cronbackup-yamtrack manual-backup-$(date +%s) -n default
```

**How it works:**
1. Discovers the application pod and extracts database credentials
2. Creates a Kubernetes Secret with database and S3 credentials
3. Creates a Kubernetes CronJob that runs on the specified schedule
4. Each CronJob execution:
   - Installs minio mc client (supports both Alpine and Debian-based images)
   - Runs pg_dump to backup the database
   - Compresses the backup with gzip
   - Uploads to S3/Minio using mc client
   - Cleans up old backups based on retention policy
   - Sends Discord notification on success or failure (if webhook configured)

## Configuration

### Environment Variables

This section documents all environment variables used by pgsnap, both at the CLI level and within Kubernetes pods.

#### CLI Environment Variables

These variables are read by the pgsnap CLI tool itself:

**PostgreSQL Tools:**
- `PG_DUMP_PATH` - Path to pg_dump binary (default: `pg_dump`)
- `PSQL_PATH` - Path to psql binary (default: `psql`)

**S3 Configuration (for --upload-s3 flag):**
- `S3_ENDPOINT` - S3 endpoint URL (required for S3 upload)
- `S3_BUCKET` - S3 bucket name (required for S3 upload)
- `S3_PREFIX` - S3 key prefix/folder for organizing backups (optional)
- `S3_ACCESS_KEY` - S3 access key for authentication (required for S3 upload)
- `S3_SECRET_KEY` - S3 secret key for authentication (required for S3 upload)
- `S3_REGION` - S3 region (default: `us-east-1`)

**Kubernetes:**
- `KUBECONFIG` - Path to kubeconfig file (default: `~/.kube/config`)

#### Pod Environment Variables (Database Detection)

pgsnap automatically detects database configuration from pod environment variables. When you run `backup`, `restore`, `sql`, or `info` commands, pgsnap reads these variables from the discovered pod:

**Full URL formats (parsed automatically):**
- `DATABASE_URL` - Full PostgreSQL connection URL (e.g., `postgresql://user:pass@host:5432/dbname`)
- `POSTGRES_URL` - Alternative full connection URL
- `DB_URL` - Alternative full connection URL

**Individual components:**
- **Host:** `POSTGRES_HOST`, `DB_HOST`, `DATABASE_HOST`
- **Port:** `POSTGRES_PORT`, `DB_PORT`, `DATABASE_PORT` (default: 5432 if not specified)
- **Database:** `POSTGRES_DB`, `POSTGRES_DATABASE`, `DB_NAME`, `DATABASE_NAME`
- **User:** `POSTGRES_USER`, `DB_USER`, `DATABASE_USER`
- **Password:** `POSTGRES_PASSWORD`, `DB_PASSWORD`, `DATABASE_PASSWORD`
- **SSL Mode:** `POSTGRES_SSL_MODE`, `DB_SSL_MODE`, `PGSSLMODE`

> **Note:** pgsnap supports reading values from Kubernetes Secrets and ConfigMaps via `valueFrom.secretKeyRef` and `valueFrom.configMapKeyRef`.

#### CronJob Environment Variables

When using the `cronbackup` command, the following environment variables are created and stored in a Kubernetes Secret (`pgsnap-cronbackup-<app>`). These variables are used by the CronJob pods:

**Database Configuration:**
- `PGPASSWORD` - PostgreSQL password (used by pg_dump)
- `DB_HOST` - Database host address
- `DB_PORT` - Database port (typically 5432)
- `DB_USER` - Database username
- `DB_NAME` - Database name

**S3 Configuration:**
- `S3_ENDPOINT` - S3/Minio endpoint URL
- `S3_BUCKET` - S3 bucket name for storing backups
- `S3_ACCESS_KEY` - S3 access key
- `S3_SECRET_KEY` - S3 secret key
- `S3_PREFIX` - S3 prefix/folder path (optional)

**Backup Configuration:**
- `RETENTION_DAYS` - Number of days to retain old backups (default: 7)
- `APP_IDENTIFIER` - Application identifier for logging
- `NAMESPACE` - Kubernetes namespace

**Notification Configuration:**
- `DISCORD_WEBHOOK_URL` - Discord webhook URL for notifications (optional)

#### Environment Variable Priority

When detecting database configuration, pgsnap checks environment variables in this order:

1. Full URL formats (`DATABASE_URL`, `POSTGRES_URL`, `DB_URL`)
2. Standard naming (`POSTGRES_*` variables)
3. Alternative naming (`DB_*` variables)
4. Generic naming (`DATABASE_*` variables)

The first match wins for each configuration parameter.

## Pod Discovery

pgsnap uses multiple strategies to discover pods:

1. **Label selectors:**
   - `app.kubernetes.io/name=<app>`
   - `app=<app>`
   - `app.kubernetes.io/instance=<app>`

2. **Name patterns:**
   - Pods with name containing `<app>`

3. **Exact match:**
   - Use `--pod` flag to specify exact pod name

## S3 Upload Example

```bash
# Configure S3 (MinIO example)
export S3_ENDPOINT="https://minio.example.com"
export S3_BUCKET="postgres-backups"
export S3_PREFIX="yamtrack"
export S3_ACCESS_KEY="your-access-key"
export S3_SECRET_KEY="your-secret-key"

# Backup with S3 upload
pgsnap backup yamtrack --upload-s3
```

## Use Cases

### Backup Multiple Apps

```bash
# Backup all your apps
pgsnap backup yamtrack
pgsnap backup tinyauth
pgsnap backup keycloak
pgsnap backup open-webui
```

### Automated Backups

```bash
#!/bin/bash
# backup-all.sh

APPS="yamtrack tinyauth keycloak"
for app in $APPS; do
  echo "Backing up $app..."
  pgsnap backup $app --upload-s3
done
```

### CI/CD Integration

```yaml
# .gitlab-ci.yml
backup-production:
  stage: backup
  script:
    - pgsnap backup yamtrack --namespace production --upload-s3
  only:
    - schedules
```

## Troubleshooting

### Pod not found

Use the `info` command to debug discovery:

```bash
pgsnap info yamtrack --namespace your-namespace
```

If auto-discovery fails, specify exact pod name:

```bash
pgsnap backup --pod yamtrack-deployment-abc123
```

### Database config not found

Ensure your pod has one of the supported environment variables. Check with:

```bash
kubectl get pod <pod-name> -o jsonpath='{.spec.containers[0].env[*].name}' | tr ' ' '\n' | grep -i postgres
```

### pg_dump not found

Specify the path to pg_dump:

```bash
export PG_DUMP_PATH=/usr/local/bin/pg_dump
pgsnap backup yamtrack
```

## Development

### Build

```bash
go build -o pgsnap
```

### Test

```bash
go test ./...
```

### Add New Command

Commands are modular. To add a new command:

1. Create `cmd/yourcommand.go`
2. Implement the command using Cobra
3. Add to `rootCmd` in `init()`

Example structure:

```go
package cmd

import "github.com/spf13/cobra"

var yourCmd = &cobra.Command{
    Use:   "yourcommand",
    Short: "Description",
    RunE:  runYourCommand,
}

func init() {
    rootCmd.AddCommand(yourCmd)
}

func runYourCommand(cmd *cobra.Command, args []string) error {
    // Your logic here
    return nil
}
```

## License

MIT

## Contributing

Contributions are welcome! Please open an issue or PR.

## Author

Created for managing PostgreSQL backups across multiple Kubernetes applications.
