package model

import "encoding/json"

type SandboxConfig struct {
	RootDirs        []string `json:"root_dirs"`
	ExtBlacklist    []string `json:"ext_blacklist"`
	SandboxDisabled bool     `json:"-"` // set programmatically when raw_shell is enabled; disables sandbox path checks
}

// UnmarshalJSON migrates the legacy singular "root_dir" field into RootDirs.
func (c *SandboxConfig) UnmarshalJSON(data []byte) error {
	var raw struct {
		RootDirs     []string `json:"root_dirs"`
		RootDir      string   `json:"root_dir"`
		ExtBlacklist []string `json:"ext_blacklist"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	c.RootDirs = raw.RootDirs
	if len(c.RootDirs) == 0 && raw.RootDir != "" {
		c.RootDirs = []string{raw.RootDir}
	}
	c.ExtBlacklist = raw.ExtBlacklist
	return nil
}

// DefaultRootDir returns the first configured root dir, or the builtin default.
func (c *SandboxConfig) DefaultRootDir() string {
	if len(c.RootDirs) > 0 && c.RootDirs[0] != "" {
		return c.RootDirs[0]
	}
	return "./agent"
}

// HasRootDir reports whether dir is in the configured root dir list.
func (c *SandboxConfig) HasRootDir(dir string) bool {
	for _, d := range c.RootDirs {
		if d == dir {
			return true
		}
	}
	return false
}
