package postgres

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/vosiander/pgsnap/pkg/common"
)

// BackupOptions holds options for database backup
type BackupOptions struct {
	PgDumpPath string
	OutputFile string
	DBConfig   *DBConfig
}

// Backup creates a PostgreSQL backup
func Backup(opts BackupOptions) error {
	// Validate pg_dump exists
	if err := validatePgDump(opts.PgDumpPath); err != nil {
		return err
	}

	// Ensure output directory exists
	outputDir := filepath.Dir(opts.OutputFile)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Build pg_dump command
	args := []string{
		"--format=plain",
		"--no-owner",
		"--no-acl",
		"--clean",
		"--if-exists",
		fmt.Sprintf("--host=%s", opts.DBConfig.Host),
		fmt.Sprintf("--port=%d", opts.DBConfig.Port),
		fmt.Sprintf("--username=%s", opts.DBConfig.User),
		fmt.Sprintf("--dbname=%s", opts.DBConfig.Database),
		fmt.Sprintf("--file=%s", opts.OutputFile),
	}

	// Create command
	cmd := exec.Command(opts.PgDumpPath, args...)

	// Set PGPASSWORD environment variable
	cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", opts.DBConfig.Password))

	// Set stdin, stdout, stderr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Execute backup
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %v", common.ErrBackupFailed, err)
	}

	return nil
}

// validatePgDump checks if pg_dump is available
func validatePgDump(pgDumpPath string) error {
	cmd := exec.Command(pgDumpPath, "--version")
	if err := cmd.Run(); err != nil {
		return common.ErrPgDumpNotFound
	}
	return nil
}
