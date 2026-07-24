package model

type SandboxConfig struct {
	RootDir         string   `json:"root_dir"`
	ExtBlacklist    []string `json:"ext_blacklist"`
	SandboxDisabled bool     `json:"-"` // set programmatically when raw_shell is enabled; disables sandbox path checks
}
