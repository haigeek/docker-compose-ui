package safe

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteWithBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte("version: '3'"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	fs := NewFileStore()
	backup, err := fs.WriteWithBackup(path, fi.ModTime().UnixMilli(), []byte("services:{}"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if backup == "" {
		t.Fatalf("expected backup path")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup should exist: %v", err)
	}
}

func TestWriteWithBackupMtimeConflict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(path, []byte("version: '3'"), 0o644); err != nil {
		t.Fatal(err)
	}

	fs := NewFileStore()
	_, err := fs.WriteWithBackup(path, 1, []byte("services:{}"))
	if err != ErrMtimeConflict {
		t.Fatalf("expected ErrMtimeConflict, got %v", err)
	}
}
