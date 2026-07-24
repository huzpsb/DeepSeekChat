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
	return os.Rename(path, dest)
}

func IsIgnoredName(name string) bool {
	return name == ".trash_can" || name == "_runtime"
}
