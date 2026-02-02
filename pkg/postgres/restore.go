package postgres

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/vosiander/pgsnap/pkg/common"
)

// RestoreOptions holds options for database restore
type RestoreOptions struct {
	PsqlPath  string
	InputFile string
	DBConfig  *DBConfig
}

// Restore restores a PostgreSQL backup
func Restore(opts RestoreOptions) error {
	// Validate psql exists
	if err := validatePsql(opts.PsqlPath); err != nil {
		return err
	}

	// Validate input file exists
	if _, err := os.Stat(opts.InputFile); os.IsNotExist(err) {
		return common.ErrFileNotFound
	}

	// Build psql command
	args := []string{
		fmt.Sprintf("--host=%s", opts.DBConfig.Host),
		fmt.Sprintf("--port=%d", opts.DBConfig.Port),
		fmt.Sprintf("--username=%s", opts.DBConfig.User),
		fmt.Sprintf("--dbname=%s", opts.DBConfig.Database),
		"--file", opts.InputFile,
	}

	// Create command
	cmd := exec.Command(opts.PsqlPath, args...)

	// Set PGPASSWORD environment variable
	cmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", opts.DBConfig.Password))

	// Set stdin, stdout, stderr
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Execute restore
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %v", common.ErrRestoreFailed, err)
	}

	return nil
}

// validatePsql checks if psql is available
func validatePsql(psqlPath string) error {
	cmd := exec.Command(psqlPath, "--version")
	if err := cmd.Run(); err != nil {
		return common.ErrPsqlNotFound
	}
	return nil
}
