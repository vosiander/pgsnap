package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSQLInputInline(t *testing.T) {
	t.Parallel()

	sqlData, err := readSQLInput("SELECT 1;", "", strings.NewReader(""), false)
	if err != nil {
		t.Fatalf("readSQLInput returned error: %v", err)
	}

	if string(sqlData) != "SELECT 1;" {
		t.Fatalf("expected inline SQL, got %q", string(sqlData))
	}
}

func TestReadSQLInputFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "query.sql")
	if err := os.WriteFile(path, []byte("SELECT current_database();"), 0644); err != nil {
		t.Fatalf("failed to write SQL file: %v", err)
	}

	sqlData, err := readSQLInput("", path, strings.NewReader(""), false)
	if err != nil {
		t.Fatalf("readSQLInput returned error: %v", err)
	}

	if string(sqlData) != "SELECT current_database();" {
		t.Fatalf("expected file SQL, got %q", string(sqlData))
	}
}

func TestReadSQLInputStdin(t *testing.T) {
	t.Parallel()

	sqlData, err := readSQLInput("", "", strings.NewReader("SELECT now();"), true)
	if err != nil {
		t.Fatalf("readSQLInput returned error: %v", err)
	}

	if string(sqlData) != "SELECT now();" {
		t.Fatalf("expected stdin SQL, got %q", string(sqlData))
	}
}

func TestReadSQLInputMultipleSourcesFails(t *testing.T) {
	t.Parallel()

	_, err := readSQLInput("SELECT 1;", "", strings.NewReader("SELECT 2;"), true)
	if err == nil {
		t.Fatal("expected error for multiple SQL sources")
	}
}

func TestReadSQLInputNoSourceFails(t *testing.T) {
	t.Parallel()

	_, err := readSQLInput("", "", strings.NewReader(""), false)
	if err == nil {
		t.Fatal("expected error when no SQL source is provided")
	}
}

func TestReadSQLInputEmptySourceFails(t *testing.T) {
	t.Parallel()

	_, err := readSQLInput("   \n\t", "", strings.NewReader(""), false)
	if err == nil {
		t.Fatal("expected error for empty SQL input")
	}
}
