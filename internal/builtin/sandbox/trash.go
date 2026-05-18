package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func (p *Provider) trashPath(base string) string {
	trashDir := filepath.Join(p.rootDir, ".trash_can")
	_ = os.MkdirAll(trashDir, 0755)
	timestamp := (time.Now().UnixNano()) % 1_0000_0000
	return filepath.Join(trashDir, fmt.Sprintf("%d_%s", timestamp, filepath.Base(base)))
}

func (p *Provider) moveToTrash(path string) error {
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		return os.Remove(path)
	}
	return os.Rename(path, p.trashPath(path))
}

func IsIgnoredName(name string) bool {
	return name == ".trash_can" || name == "_runtime"
}
