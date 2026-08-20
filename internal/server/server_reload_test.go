package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

// TestHandleMCPReload_SwapsLLMClient verifies that editing provider/model in
// config.json followed by POST /api/mcp/reload not only refreshes the MCP
// manager's config (and therefore GET /api/config) but also rebuilds the
// engine's LLM client — otherwise the UI would offer a provider that
// inference silently doesn't use.
func TestHandleMCPReload_SwapsLLMClient(t *testing.T) {
	setupServerTest(t)
	storage.SaveConfig(&model.MCPConfig{
		ModelProviders: []model.ModelProvider{
			{Name: "a", Endpoint: "http://a", APIKey: "ka", Models: []string{"m1"}},
		},
		Provider: "a",
		Model:    "m1",
	})

	srv := New(testStaticFS)
	before := srv.engine.Client()
	if before.Endpoint() != "http://a" || before.Model() != "m1" {
		t.Fatalf("unexpected initial client: endpoint=%q model=%q", before.Endpoint(), before.Model())
	}

	// Edit config.json on disk while the server is "running".
	storage.SaveConfig(&model.MCPConfig{
		ModelProviders: []model.ModelProvider{
			{Name: "b", Endpoint: "http://b", APIKey: "kb", Models: []string{"m2"}},
		},
		Provider: "b",
		Model:    "m2",
	})

	req := httptest.NewRequest("POST", "/api/mcp/reload", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("reload failed: %d %s", w.Code, w.Body.String())
	}

	after := srv.engine.Client()
	if after == before {
		t.Errorf("LLM client was not swapped")
	}
	if after.Endpoint() != "http://b" || after.Model() != "m2" {
		t.Errorf("client still has old config: endpoint=%q model=%q", after.Endpoint(), after.Model())
	}

	// The config endpoint must agree with what inference will use.
	w = httptest.NewRecorder()
	srv.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/config", nil))
	body := w.Body.String()
	if !strings.Contains(body, `"provider":"b"`) || !strings.Contains(body, `"model":"m2"`) {
		t.Errorf("GET /api/config disagrees with LLM client: %s", body)
	}
}
