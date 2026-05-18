package coding

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hschat/internal/builtin/sandbox"
	"hschat/internal/model"
)

func (p *Provider) runShellTool(tool model.ShellTool) string {
	if len(p.fileBlacklist) > 0 {
		checkErr := filepath.Walk(p.rootDir, func(fp string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if sandbox.IsIgnoredName(info.Name()) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.IsDir() {
				return nil
			}
			if info.Size() > 1024*1024 {
				return nil
			}
			data, err := os.ReadFile(fp)
			if err != nil {
				return nil
			}
			content := string(data)
			for _, bl := range p.fileBlacklist {
				if strings.Contains(content, bl) {
					return fmt.Errorf("file %s is potentially malicious", fp)
				}
			}
			return nil
		})
		if checkErr != nil {
			return fmt.Sprintf("Error: ClamAV: %v", checkErr)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(tool.Timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if os.PathSeparator == '\\' {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/c", tool.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", tool.Command)
	}
	cmd.Dir = p.rootDir

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	return fmt.Sprintf("Exit Code: %v\n\n--- Stdout ---\n%s\n--- Stderr ---\n%s", err, outBuf.String(), errBuf.String())
}
