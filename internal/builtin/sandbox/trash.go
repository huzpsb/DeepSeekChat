package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func MoveToTrash(rootDir, path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return os.Remove(path)
	}
	trashDir := filepath.Join(rootDir, ".trash_can")
	if filepath.Dir(path) == trashDir {
		return nil
	}
	_ = os.MkdirAll(trashDir, 0755)
	timestamp := (time.Now().UnixMilli()) % 1_000_000
	dest := filepath.Join(trashDir, fmt.Sprintf("%d_%s", timestamp, filepath.Base(path)))
	if err := os.Rename(path, dest); err != nil {
		if copyErr := copyFile(path, dest); copyErr != nil {
			return fmt.Errorf("rename failed: %v; copy fallback failed: %v", err, copyErr)
		}
		return os.Remove(path)
	}
	return nil
}

func IsIgnoredName(name string) bool {
	return name == ".trash_can" || name == "_runtime" || name == ".git"
}
