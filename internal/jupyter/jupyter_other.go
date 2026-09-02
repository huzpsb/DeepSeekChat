//go:build !linux

package jupyter

// Detect reports whether a Jupyter server is running. The integration is
// Linux-only; on other platforms it never triggers.
func Detect() bool { return false }

// Registered reports whether DsChat is registered with jupyter-server-proxy.
func Registered() bool { return false }

// Register is a no-op on non-Linux platforms.
func Register(port int) (changed bool, err error) { return false, nil }

// Restart is a no-op on non-Linux platforms.
func Restart() error { return nil }
