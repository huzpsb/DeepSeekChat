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
	"unicode/utf8"

	"hschat/internal/builtin/sandbox"
	"hschat/internal/encoding"
	"hschat/internal/model"
)

const (
	// defaultRunOutputSizeLimit 是 run 工具 stdout/stderr 各自默认的最大输出字节数。
	defaultRunOutputSizeLimit = 10 * 1024
	// runOutputKeepBytes 是截断后 stdout/stderr 各自保留的首尾字节数。
	runOutputKeepBytes = 1024
)

func (p *Provider) runShellTool(ctx context.Context, tool model.ShellTool, rootDir string) string {
	return p.runShellToolWithLimit(ctx, tool, rootDir, 0)
}

func (p *Provider) runShellToolWithLimit(ctx context.Context, tool model.ShellTool, rootDir string, outputSizeLimit int) string {
	rawEnabled := p.rawShell != nil && p.rawShell.Enabled

	if outputSizeLimit > 0 && outputSizeLimit < defaultRunOutputSizeLimit {
		outputSizeLimit = defaultRunOutputSizeLimit
	}

	if !rawEnabled && len(p.fileBlacklist) > 0 {
		checkErr := filepath.Walk(rootDir, func(fp string, info os.FileInfo, err error) error {
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
		cmd.Dir = rootDir
	}

	setProcessGroup(cmd)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		return formatShellResult(err, "", "", outputSizeLimit)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return formatShellResult(err, encoding.DecodeGB18030(outBuf.Bytes()), encoding.DecodeGB18030(errBuf.Bytes()), outputSizeLimit)
	case <-ctx.Done():
		killProcessTree(cmd)
		<-done
		return formatShellResult(ctx.Err(), encoding.DecodeGB18030(outBuf.Bytes()), encoding.DecodeGB18030(errBuf.Bytes()), outputSizeLimit)
	}
}

func formatShellResult(exitInfo any, stdout, stderr string, outputSizeLimit int) string {
	if outputSizeLimit > 0 {
		stdout = truncateShellOutput(stdout, outputSizeLimit)
		stderr = truncateShellOutput(stderr, outputSizeLimit)
	}
	return fmt.Sprintf("Exit Code: %v\n\n--- Stdout ---\n%s\n--- Stderr ---\n%s", exitInfo, stdout, stderr)
}

// truncateShellOutput 将单个输出流截断为首尾各 runOutputKeepBytes 字节，
// 并附加一眼可见的 truncated 标记。
func truncateShellOutput(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	keep := runOutputKeepBytes
	if keep > len(s) {
		keep = len(s)
	}
	head := cutUTF8Head(s, keep)
	tail := cutUTF8Tail(s, keep)
	return fmt.Sprintf("%s\n...[truncated]...\n%s\nOutput was %d bytes and has been truncated. output_size_limit=%d bytes; showing first and last %d bytes.", head, tail, len(s), limit, keep)
}

func cutUTF8Head(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

func cutUTF8Tail(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
}
