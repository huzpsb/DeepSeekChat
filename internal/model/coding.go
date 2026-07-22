package model

type CodingConfig struct {
	ShellTools map[string]ShellTool `json:"shell_tools"`
	Blacklist  []string             `json:"blacklist"`
	RawShell   *RawShellConfig      `json:"raw_shell"`
}

type ShellTool struct {
	Description       string `json:"description"`
	Command           string `json:"command"`
	Timeout           int    `json:"timeout"`
	RelativeOverwrite *bool  `json:"relative_overwrite,omitempty"`
}

type RawShellConfig struct {
	Enabled  bool     `json:"enabled"`
	Shell    []string `json:"shell"`
	Preamble string   `json:"preamble"`
}
