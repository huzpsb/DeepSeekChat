package model

type SandboxConfig struct {
	RootDir             string   `json:"root_dir"`
	ExtBlacklist        []string `json:"ext_blacklist"`
	DisablePathWarnings bool     `json:"-"` // set programmatically when raw_shell is enabled
}
