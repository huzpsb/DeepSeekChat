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

// ExtensionInstalled reports whether the jupyter-server-proxy Python
// package is importable by the interpreter that runs Jupyter. Without the
// extension the ServerProxy.servers config is silently ignored: no
// launcher entry, no /proxy/ endpoints.
func ExtensionInstalled() bool {
	py := interpreter()
	if py == "" {
		return false
	}
	return exec.Command(py, "-c", "import jupyter_server_proxy").Run() == nil
}

// InstallExtension installs jupyter-server-proxy into the Jupyter
// interpreter's environment.
func InstallExtension() error {
	py := interpreter()
	if py == "" {
		return fmt.Errorf("cannot locate the jupyter python interpreter")
	}
	log.Printf("[jupyter] installing jupyter-server-proxy via %s -m pip", py)
	out, err := exec.Command(py, "-m", "pip", "install", "jupyter-server-proxy").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pip install failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// interpreter returns the python executable of the running Jupyter server.
func interpreter() string {
	for _, p := range findProcesses() {
		if len(p.args) > 0 && strings.Contains(filepath.Base(p.args[0]), "python") {
			return p.args[0]
		}
	}
	return ""
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
// whether the config was actually modified. iconPath, when non-empty, is
// referenced by the launcher tile.
func Register(port int, iconPath string) (changed bool, err error) {
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
	// path_info pins the launcher tile URL to the port-proxy route
	// (/proxy/<port>/) instead of the named route (/<name>/): some hosting
	// gateways (autodl) mishandle redirects on the named route, causing a
	// 302 self-loop, while the port route passes cleanly end to end.
	//
	// category is the *translated* Notebook section name ("笔记本"): the
	// JupyterLab launcher only honors kernelIconUrl in the Notebook/Console
	// sections and compares against the localized names, so the English
	// default renders no icon under a Chinese UI. Target deployments
	// (autodl images) use a Chinese UI.
	launcher := fmt.Sprintf(`{"title": "DsChat", "path_info": "proxy/%d/", "category": "笔记本"}`, port)
	if iconPath != "" {
		launcher = fmt.Sprintf(`{"title": "DsChat", "path_info": "proxy/%d/", "icon_path": %q, "category": "笔记本"}`, port, iconPath)
	}
	// Note the isinstance guard: under traitlets 5, reading an unset
	// config attribute returns a LazyConfigValue instead of raising
	// AttributeError, so getattr(..., None) never falls back — and a
	// LazyConfigValue is truthy but not iterable, so "or {}" /
	// dict(...) on it would crash the entire config file load.
	snippet := fmt.Sprintf(`
%[1]s
c = get_config()  # noqa
_dschat_servers = getattr(c.ServerProxy, "servers", None)
if not isinstance(_dschat_servers, dict):
    _dschat_servers = {}
else:
    _dschat_servers = dict(_dschat_servers)
_dschat_servers["dschat"] = {
    "port": %[2]d,
    # open as an embedded JupyterLab tab (iframe) rather than a new
    # browser tab; DsChat sends no X-Frame-Options so framing is allowed
    "new_browser_tab": False,
    "launcher_entry": %[3]s,
}
c.ServerProxy.servers = _dschat_servers
%[4]s
`, markerBegin, port, launcher, markerEnd)
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

// EnsureSubconfig handles Jupyter started with an explicit --config file:
// in that case the default config dir (~/.jupyter) is not loaded at all,
// so the proxy registration there would never take effect. We append a
// single load_subconfig bridge line to the --config file (typically a
// platform-managed overlay, so the footprint stays minimal). It runs on
// every startup, independent of registration state. Reports whether any
// file was modified.
func EnsureSubconfig() (changed bool, err error) {
	target := configPath()
	// Only bridge to a file that actually exists — load_subconfig of a
	// missing file may fail Jupyter's startup.
	if _, err := os.Stat(target); err != nil {
		return false, nil
	}
	needleD := []byte(fmt.Sprintf("load_subconfig(%q)", target))
	needleS := []byte(fmt.Sprintf("load_subconfig('%s')", target))
	for _, proc := range findProcesses() {
		for i, a := range proc.args {
			var path string
			switch {
			case strings.HasPrefix(a, "--config="):
				path = strings.TrimPrefix(a, "--config=")
			case a == "--config" && i+1 < len(proc.args):
				path = proc.args[i+1]
			}
			if path == "" || path == target {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				log.Printf("[jupyter] cannot read --config file %s: %v", path, err)
				continue
			}
			if bytes.Contains(data, needleD) || bytes.Contains(data, needleS) {
				continue
			}
			line := fmt.Sprintf("\n# added by dschat: explicit --config suppresses the default config dir\nload_subconfig(%q)\n", target)
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				log.Printf("[jupyter] cannot append to --config file %s: %v", path, err)
				continue
			}
			if _, err := f.WriteString(line); err != nil {
				f.Close()
				log.Printf("[jupyter] cannot write to --config file %s: %v", path, err)
				continue
			}
			f.Close()
			log.Printf("[jupyter] appended load_subconfig bridge to %s", path)
			changed = true
		}
	}
	return changed, nil
}

// WriteIcon writes icon data next to the Jupyter config and returns its
// path, for use as the launcher_entry icon_path. The JupyterLab launcher
// expects an SVG (icon_path is documented as such and rendered via
// kernelIconUrl); an ico silently fails to render there.
func WriteIcon(data []byte, name string) (string, error) {
	dir := filepath.Dir(configPath())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", err
	}
	return path, nil
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

	// Many container images (autodl's /init, systemd Restart=always, ...)
	// run Jupyter under a watchdog that respawns it automatically. If we
	// blindly relaunch here, the watchdog's copy and ours end up fighting
	// over the same port. Give any supervisor a grace period to bring the
	// server back; only relaunch manually when nothing reappears.
	oldPids := make(map[int]bool, len(procs))
	for _, p := range procs {
		oldPids[p.pid] = true
	}
	respawnDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(respawnDeadline) {
		for _, np := range findProcesses() {
			if !oldPids[np.pid] {
				log.Printf("[jupyter] supervisor respawned pid=%d cmd=%q; skipping manual relaunch", np.pid, strings.Join(np.args, " "))
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	for _, p := range procs {
		if len(p.args) == 0 {
			continue
		}
		cmd := exec.Command(p.args[0], p.args[1:]...)
		cmd.Dir = p.cwd
		// Keep jupyter's stdout/stderr somewhere inspectable instead of
		// /dev/null — config-load errors would otherwise be invisible.
		if f, err := os.OpenFile(filepath.Join(p.cwd, "jupyter.dschat.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			defer f.Close()
			cmd.Stdout = f
			cmd.Stderr = f
		}
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
