//go:build !linux

package jupyter

// Detect reports whether a Jupyter server is running. The integration is
// Linux-only; on other platforms it never triggers.
func Detect() bool { return false }

// Registered reports whether DsChat is registered with jupyter-server-proxy.
func Registered() bool { return false }

// Register is a no-op on non-Linux platforms.
func Register(port int, iconPath string) (changed bool, err error) { return false, nil }

// EnsureSubconfig is a no-op on non-Linux platforms.
func EnsureSubconfig() (changed bool, err error) { return false, nil }

// WriteIcon is unused on non-Linux platforms.
func WriteIcon(data []byte, name string) (string, error) { return "", nil }

// EnsureScreen only does something on Linux (where screen exists); on
// other platforms it never triggers.
func EnsureScreen() (shouldExit bool) { return false }

// ExtensionInstalled always reports true on non-Linux platforms (the whole
// integration never triggers there anyway).
func ExtensionInstalled() bool { return true }

// InstallExtension is a no-op on non-Linux platforms.
func InstallExtension() error { return nil }

// Restart is a no-op on non-Linux platforms.
func Restart() error { return nil }
