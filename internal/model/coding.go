package model

type CodingConfig struct {
	RootDir      string               `json:"root_dir"`
	ShellTools   map[string]ShellTool `json:"shell_tools"`
	Blacklist    []string             `json:"blacklist"`
	ExtBlacklist []string             `json:"ext_blacklist"`
}

type ShellTool struct {
	Description string `json:"description"`
	Command     string `json:"command"`
	Timeout     int    `json:"timeout"`
}
