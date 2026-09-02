//go:build linux

package jupyter

import (
	"log"
	"os"
	"os/exec"
)

// EnsureScreen re-executes the current process inside a detached screen
// session when it is not already running under a terminal multiplexer.
// This guards against the classic "started the server from a jupyter
// terminal without screen" footgun, where the process dies together with
// the browser terminal. It returns true when the caller should exit
// immediately — either because the process was relaunched, or because a
// detached session is already running (refusing to start a second
// instance that would fight over the same port).
func EnsureScreen() (shouldExit bool) {
	// STY is set by screen; DSCHAT_IN_SCREEN is our own belt-and-braces
	// guard against a relaunch loop.
	if os.Getenv("STY") != "" || os.Getenv("DSCHAT_IN_SCREEN") != "" {
		return false
	}
	screen, err := exec.LookPath("screen")
	if err != nil {
		log.Printf("[screen] screen not installed; running in foreground")
		return false
	}

	const name = "dschat"
	// -Q select . exits 0 when the session exists and is alive.
	if exec.Command(screen, "-S", name, "-Q", "select", ".").Run() == nil {
		log.Printf("[screen] already running (attach with: screen -r %s); refusing to start a second instance", name)
		return true
	}

	exe, err := os.Executable()
	if err != nil {
		log.Printf("[screen] cannot resolve own executable: %v; running in foreground", err)
		return false
	}
	args := append([]string{"-dmS", name, exe}, os.Args[1:]...)
	cmd := exec.Command(screen, args...)
	cmd.Env = append(os.Environ(), "DSCHAT_IN_SCREEN=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[screen] failed to relaunch inside screen: %v: %s; running in foreground", err, out)
		return false
	}
	log.Printf("[screen] check screen %s (attach with: screen -r %s)", name, name)
	return true
}
