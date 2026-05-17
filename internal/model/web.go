package model

type WebConfig struct {
	Headers map[string]string `json:"headers"`
	Proxy   string            `json:"proxy"`
}
