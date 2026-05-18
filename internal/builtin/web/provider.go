package web

import (
	"hschat/internal/builtin"
	"hschat/internal/storage"
)

type Provider struct {
	rootDir string
	headers map[string]string
	proxy   string
}

func New(rootDir string) builtin.Provider {
	return &Provider{
		rootDir: rootDir,
		headers: make(map[string]string),
	}
}

func (p *Provider) Name() string {
	return "WebMCP"
}

func (p *Provider) Initialize(configPath string) error {
	cfg, err := storage.LoadWebConfig()
	if err != nil {
		return err
	}

	if cfg.Headers == nil {
		cfg.Headers = map[string]string{
			"User-Agent":                "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36",
			"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8",
			"Accept-Language":           "en-US,en;q=0.9",
			"sec-ch-ua":                 `"Google Chrome";v="143", "Chromium";v="143", "Not A(Brand";v="24"`,
			"sec-ch-ua-mobile":          "?0",
			"sec-ch-ua-platform":        `"Windows"`,
			"Sec-Fetch-Dest":            "document",
			"Sec-Fetch-Mode":            "navigate",
			"Sec-Fetch-Site":            "none",
			"Sec-Fetch-User":            "?1",
			"Upgrade-Insecure-Requests": "1",
		}
	}
	p.headers = cfg.Headers
	p.proxy = cfg.Proxy

	storage.SaveWebConfig(cfg)

	return nil
}

func (p *Provider) Close() error {
	return nil
}
