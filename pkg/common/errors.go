package common

import "errors"

// Common errors used across pgsnap
var (
	ErrPodNotFound        = errors.New("pod not found")
	ErrMultiplePodsFound  = errors.New("multiple pods found, use --pod to specify exact pod")
	ErrNoDatabaseConfig   = errors.New("no database configuration found in pod environment")
	ErrBackupFailed       = errors.New("backup failed")
	ErrRestoreFailed      = errors.New("restore failed")
	ErrSQLFailed          = errors.New("sql job failed")
	ErrConnectionFailed   = errors.New("database connection failed")
	ErrFileNotFound       = errors.New("backup file not found")
	ErrInvalidBackupFile  = errors.New("invalid backup file")
	ErrPgDumpNotFound     = errors.New("pg_dump not found in PATH")
	ErrPsqlNotFound       = errors.New("psql not found in PATH")
	ErrS3ConfigIncomplete = errors.New("S3 configuration incomplete")
	ErrNoBackupsFound     = errors.New("no backups found")
)
