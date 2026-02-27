package safe

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type FileStore struct{}

func NewFileStore() *FileStore { return &FileStore{} }

func (f *FileStore) Read(path string) ([]byte, os.FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return b, fi, nil
}

func (f *FileStore) WriteWithBackup(path string, expectedMtime int64, content []byte) (backupPath string, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if expectedMtime > 0 && fi.ModTime().UnixMilli() != expectedMtime {
		return "", ErrMtimeConflict
	}

	backupPath = fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
	if err := copyFile(path, backupPath); err != nil {
		return "", err
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, content, fi.Mode()); err != nil {
		return backupPath, err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return backupPath, err
	}
	return backupPath, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
