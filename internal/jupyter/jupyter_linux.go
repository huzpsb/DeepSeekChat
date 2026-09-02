//go:build linux

// Package jupyter implements the one-time Linux/Jupyter integration:
// detecting a running Jupyter server, registering DsChat with
// jupyter-server-proxy, and restarting Jupyter so the proxy config is
// picked up. The goal is a one-command startup on a Jupyter box: launch
// DsChat, refresh the page, set an API key, chat.
package jupyter

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	markerBegin = "# dschat-proxy-begin"
	markerEnd   = "# dschat-proxy-end"
)

// jupyterTokens identify main Jupyter server processes in /proc cmdlines.
var jupyterTokens = []string{
	"jupyter-lab",
	"jupyter-notebook",
	"jupyter-server",
	"jupyter_server",
	"jupyterhub",
	"jupyterlab",
}

type process struct {
	pid  int
	args []string
	cwd  string
}

// Detect reports whether a Jupyter server process is currently running.
func Detect() bool {
	return len(findProcesses()) > 0
}

func findProcesses() []process {
	self := os.Getpid()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var procs []process
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		match := false
		for _, tok := range jupyterTokens {
			if strings.Contains(string(raw), tok) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		parts := bytes.Split(bytes.TrimRight(raw, "\x00"), []byte{0})
		args := make([]string, 0, len(parts))
		for _, p := range parts {
			args = append(args, string(p))
		}
		cwd, _ := os.Readlink(filepath.Join("/proc", e.Name(), "cwd"))
		procs = append(procs, process{pid: pid, args: args, cwd: cwd})
	}
	return procs
}

func configPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".jupyter", "jupyter_server_config.py")
}

// Registered reports whether the DsChat proxy block is already present in
// the Jupyter server config.
func Registered() bool {
	data, err := os.ReadFile(configPath())
	return err == nil && bytes.Contains(data, []byte(markerBegin))
}

// Register appends the DsChat server-proxy block to the Jupyter server
// config. It is a no-op when the block is already present, and reports
// whether the config was actually modified.
func Register(port int) (changed bool, err error) {
	path := configPath()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if bytes.Contains(data, []byte(markerBegin)) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	snippet := fmt.Sprintf(`
%[1]s
c = get_config()  # noqa
_existing = getattr(c.ServerProxy, "servers", None) or {}
c.ServerProxy.servers = dict(_existing)
c.ServerProxy.servers["dschat"] = {
    "port": %[2]d,
    "new_browser_tab": True,
    "launcher_entry": {"title": "DsChat"},
}
%[3]s
`, markerBegin, port, markerEnd)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if _, err := f.WriteString(snippet); err != nil {
		return false, err
	}
	log.Printf("[jupyter] registered dschat proxy (port=%d) in %s", port, path)
	return true, nil
}

// Restart restarts the running Jupyter server so it picks up the new proxy
// config. It prefers systemd units and otherwise terminates the processes
// and relaunches them detached with their original cmdline and cwd.
func Restart() error {
	for _, scope := range [][]string{{"--user"}, {}} {
		for _, unit := range []string{"jupyter", "jupyterlab", "jupyter-lab", "jupyter-notebook", "notebook", "jupyterhub"} {
			args := append(append([]string{}, scope...), "restart", unit)
			if exec.Command("systemctl", args...).Run() == nil {
				log.Printf("[jupyter] restarted via systemctl %s", strings.Join(args, " "))
				return nil
			}
		}
	}

	procs := findProcesses()
	if len(procs) == 0 {
		return fmt.Errorf("no jupyter process found to restart")
	}
	for _, p := range procs {
		log.Printf("[jupyter] stopping pid=%d cmd=%q", p.pid, strings.Join(p.args, " "))
		_ = syscall.Kill(p.pid, syscall.SIGTERM)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		alive := false
		for _, p := range procs {
			if _, err := os.Stat(fmt.Sprintf("/proc/%d", p.pid)); err == nil {
				alive = true
				break
			}
		}
		if !alive {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	for _, p := range procs {
		if len(p.args) == 0 {
			continue
		}
		cmd := exec.Command(p.args[0], p.args[1:]...)
		cmd.Dir = p.cwd
		// Detach into a new session so the relaunched server survives
		// DsChat (and its screen/tmux window) exiting.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			log.Printf("[jupyter] relaunch failed cmd=%q err=%v", strings.Join(p.args, " "), err)
			continue
		}
		_ = cmd.Process.Release()
		log.Printf("[jupyter] relaunched cmd=%q", strings.Join(p.args, " "))
	}
	return nil
}
