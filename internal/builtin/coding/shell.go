package coding

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hschat/internal/builtin/sandbox"
	"hschat/internal/encoding"
	"hschat/internal/model"
)

func (p *Provider) runShellTool(ctx context.Context, tool model.ShellTool) string {
	rawEnabled := p.rawShell != nil && p.rawShell.Enabled

	if !rawEnabled && len(p.fileBlacklist) > 0 {
		checkErr := filepath.Walk(p.getRootDir(), func(fp string, info os.FileInfo, err error) error {
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
	if rawEnabled && len(p.fileBlacklist) > 0 && !p.rawShellWarned {
		log.Printf("[WARNING] raw_shell is enabled, blacklist scan is bypassed. Blacklist entries: %v", p.fileBlacklist)
		p.rawShellWarned = true
	}

	ctx, cancel := context.WithTimeout(ctx, time.Duration(tool.Timeout)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if p.rawShell != nil && p.rawShell.Enabled {
		fullCmd := strings.Replace(p.rawShell.Preamble, "$original", tool.Command, -1)
		args := append(p.rawShell.Shell[1:], fullCmd)
		cmd = exec.Command(p.rawShell.Shell[0], args...)
	} else if os.PathSeparator == '\\' {
		cmd = exec.Command("powershell", "-NoProfile", "-Command", tool.Command)
	} else {
		cmd = exec.Command("sh", "-c", tool.Command)
	}
	if tool.RelativeOverwrite == nil || *tool.RelativeOverwrite {
		cmd.Dir = p.getRootDir()
	}

	setProcessGroup(cmd)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("Exit Code: %v\n\n--- Stdout ---\n%s\n--- Stderr ---\n%s", err, "", "")
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return fmt.Sprintf("Exit Code: %v\n\n--- Stdout ---\n%s\n--- Stderr ---\n%s", err, encoding.DecodeGB18030(outBuf.Bytes()), encoding.DecodeGB18030(errBuf.Bytes()))
	case <-ctx.Done():
		killProcessTree(cmd)
		<-done
		return fmt.Sprintf("Exit Code: %v\n\n--- Stdout ---\n%s\n--- Stderr ---\n%s", ctx.Err(), encoding.DecodeGB18030(outBuf.Bytes()), encoding.DecodeGB18030(errBuf.Bytes()))
	}
}
