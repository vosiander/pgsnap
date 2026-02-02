package storage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vosiander/pgsnap/pkg/common"
)

// BackupInfo holds information about a backup file
type BackupInfo struct {
	Filename string
	Path     string
	Size     int64
	ModTime  time.Time
	App      string
}

// ListBackups lists all backup files for an app
func ListBackups(backupDir, appIdentifier string) ([]BackupInfo, error) {
	var backups []BackupInfo

	// Check if directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return nil, common.ErrNoBackupsFound
	}

	// Walk through backup directory
	err := filepath.Walk(backupDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file matches app identifier and is a zip file
		if strings.HasPrefix(info.Name(), appIdentifier+"-") && strings.HasSuffix(info.Name(), ".zip") {
			backups = append(backups, BackupInfo{
				Filename: info.Name(),
				Path:     path,
				Size:     info.Size(),
				ModTime:  info.ModTime(),
				App:      appIdentifier,
			})
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	if len(backups) == 0 {
		return nil, common.ErrNoBackupsFound
	}

	return backups, nil
}

// GenerateBackupFilename generates a timestamped backup filename
func GenerateBackupFilename(appIdentifier string) string {
	timestamp := time.Now().Format("2006-01-02-15-04-05")
	return fmt.Sprintf("%s-%s-backup.zip", appIdentifier, timestamp)
}

// FormatSize formats file size in human-readable format
func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
