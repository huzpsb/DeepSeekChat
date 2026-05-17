package model

type CodingConfig struct {
	ShellTools map[string]ShellTool `json:"shell_tools"`
	Blacklist  []string             `json:"blacklist"`
}

type ShellTool struct {
	Description string `json:"description"`
	Command     string `json:"command"`
	Timeout     int    `json:"timeout"`
}
