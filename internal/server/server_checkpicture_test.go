package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hschat/internal/model"
	"hschat/internal/storage"
)

func seedCheckPictureConfig(t *testing.T) {
	t.Helper()
	if err := storage.SaveConfig(&model.MCPConfig{
		ModelProviders: []model.ModelProvider{
			{Name: "deepseek", Endpoint: "http://ds", APIKey: "k", Models: []string{"deepseek-flash", "deepseek-v4-pro"}},
			{Name: "qwen", Endpoint: "http://qw", APIKey: "k2", Models: []string{"qv"}},
		},
		Provider: "deepseek",
		Model:    "deepseek-flash",
	}); err != nil {
		t.Fatal(err)
	}
}

// An empty check_picture selection shows the effective deepseek default
// in GET /api/config.
func TestHandleGetConfig_CheckPictureDefault(t *testing.T) {
	setupServerTest(t)
	seedCheckPictureConfig(t)
	srv := New(testStaticFS)

	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET failed: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `"provider":"deepseek"`) || !strings.Contains(body, `"check_picture":{`) {
		t.Errorf("expected deepseek default in config response: %s", body)
	}
}

func TestHandleSetConfig_CheckPicture(t *testing.T) {
	setupServerTest(t)
	seedCheckPictureConfig(t)
	srv := New(testStaticFS)

	put := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/config", strings.NewReader(body)))
		return w
	}

	// Valid provider + model switch.
	if w := put(`{"check_picture_provider":"qwen","check_picture_model":"qv"}`); w.Code != http.StatusOK {
		t.Fatalf("PUT failed: %d %s", w.Code, w.Body.String())
	}
	cfg := srv.mcpMgr.Config()
	if cfg.CheckPicture.Provider != "qwen" || cfg.CheckPicture.Model != "qv" {
		t.Errorf("selection not persisted: %+v", cfg.CheckPicture)
	}

	// Unknown provider rejected, selection untouched.
	if w := put(`{"check_picture_provider":"nope"}`); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown provider, got %d %s", w.Code, w.Body.String())
	}
	cfg = srv.mcpMgr.Config()
	if cfg.CheckPicture.Provider != "qwen" {
		t.Errorf("rejected PUT mutated selection: %+v", cfg.CheckPicture)
	}

	// Model not offered by the selected provider rejected.
	if w := put(`{"check_picture_model":"deepseek-flash"}`); w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for foreign model, got %d %s", w.Code, w.Body.String())
	}

	// Switching provider clears a model the new provider doesn't offer.
	if w := put(`{"check_picture_provider":"deepseek"}`); w.Code != http.StatusOK {
		t.Fatalf("PUT failed: %d %s", w.Code, w.Body.String())
	}
	cfg = srv.mcpMgr.Config()
	if cfg.CheckPicture.Provider != "deepseek" || cfg.CheckPicture.Model != "" {
		t.Errorf("expected stale model cleared on provider switch, got %+v", cfg.CheckPicture)
	}
}

// Deleting the selected check_picture provider via a providers-list
// replacement resets the selection instead of leaving a dangling name.
func TestHandleSetConfig_CheckPictureOrphanedByProviderList(t *testing.T) {
	setupServerTest(t)
	seedCheckPictureConfig(t)
	srv := New(testStaticFS)

	put := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/config", strings.NewReader(body)))
		return w
	}
	if w := put(`{"check_picture_provider":"qwen","check_picture_model":"qv"}`); w.Code != http.StatusOK {
		t.Fatalf("PUT failed: %d %s", w.Code, w.Body.String())
	}
	body := `{"providers":[{"name":"deepseek","endpoint":"http://ds","api_key":"k","models":["deepseek-flash"]}]}`
	if w := put(body); w.Code != http.StatusOK {
		t.Fatalf("PUT failed: %d %s", w.Code, w.Body.String())
	}
	cfg := srv.mcpMgr.Config()
	if cfg.CheckPicture.Provider != "" || cfg.CheckPicture.Model != "" {
		t.Errorf("expected orphaned selection reset, got %+v", cfg.CheckPicture)
	}
}
