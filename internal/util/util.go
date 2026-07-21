// Package util holds small helpers shared across whodar's packages.
package util

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path through a same-directory temporary file
// and a rename, so a crash never leaves a partial or truncated file behind.
// perm applies to the final file even when path already exists looser.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("atomic write: temp: %w", err)
	}
	name := tmp.Name()
	if err := fillTemp(tmp, data, perm); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("atomic write: rename: %w", err)
	}
	return nil
}

// fillTemp writes data and perm to the open temporary file and closes it.
func fillTemp(f *os.File, data []byte, perm fs.FileMode) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("atomic write: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("atomic write: sync: %w", err)
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return fmt.Errorf("atomic write: chmod: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("atomic write: close: %w", err)
	}
	return nil
}
