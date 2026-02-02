package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Compress compresses a file into a zip archive
func Compress(sourceFile, destZip string) error {
	// Create zip file
	zipFile, err := os.Create(destZip)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer zipFile.Close()

	// Create zip writer
	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	// Open source file
	file, err := os.Open(sourceFile)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer file.Close()

	// Get file info
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Create zip header
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("failed to create zip header: %w", err)
	}

	// Use only the base name in the archive
	header.Name = filepath.Base(sourceFile)
	header.Method = zip.Deflate

	// Create writer for file in zip
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("failed to create zip entry: %w", err)
	}

	// Copy file content to zip
	if _, err := io.Copy(writer, file); err != nil {
		return fmt.Errorf("failed to write to zip: %w", err)
	}

	return nil
}

// Decompress extracts a file from a zip archive
func Decompress(zipPath, destFile string) error {
	// Open zip file
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer reader.Close()

	// Check if zip has files
	if len(reader.File) == 0 {
		return fmt.Errorf("zip file is empty")
	}

	// Extract first file in zip
	zippedFile := reader.File[0]

	// Open zipped file
	fileReader, err := zippedFile.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer fileReader.Close()

	// Create destination file
	destFileWriter, err := os.Create(destFile)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destFileWriter.Close()

	// Copy content
	if _, err := io.Copy(destFileWriter, fileReader); err != nil {
		return fmt.Errorf("failed to extract file: %w", err)
	}

	return nil
}
