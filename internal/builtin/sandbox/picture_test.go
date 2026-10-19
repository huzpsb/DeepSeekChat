package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	return buf.Bytes()
}

func setupPictureProvider(t *testing.T) *Provider {
	t.Helper()
	return &Provider{rootDir: t.TempDir()}
}

func writePicture(t *testing.T, p *Provider, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p.rootDir, name), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBilinearAxis(t *testing.T) {
	// 2:1 downscale: dst pixel centers land exactly between src pixels.
	i0, i1, frac := bilinearAxis(0, 4, 2)
	if i0 != 0 || i1 != 1 || frac != 0.5 {
		t.Errorf("got (%d, %d, %f), want (0, 1, 0.5)", i0, i1, frac)
	}
	// srcLen=1: both indices clamp to the only pixel, frac irrelevant.
	i0, i1, _ = bilinearAxis(0, 1, 1)
	if i0 != 0 || i1 != 0 {
		t.Errorf("expected clamp to single pixel, got (%d, %d)", i0, i1)
	}
	// Fraction must stay in [0, 1) and indices in range for every dst.
	for dst := 0; dst < 919; dst++ {
		i0, i1, frac := bilinearAxis(dst, 1300, 919)
		if i0 < 0 || i1 > 1299 || frac < 0 || frac >= 1 {
			t.Fatalf("dst %d: out of range (%d, %d, %f)", dst, i0, i1, frac)
		}
	}
}

func TestPreparePicture_SmallPassthrough(t *testing.T) {
	data := makePNG(t, 100, 50)
	mime, payload, err := preparePicture(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("expected image/png, got %s", mime)
	}
	if !bytes.Equal(payload, data) {
		t.Errorf("small image should be sent as-is, payload differs")
	}
}

func TestPreparePicture_Downscale(t *testing.T) {
	data := makePNG(t, 2600, 1300) // 3.38M pixels > 1.69M budget
	mime, payload, err := preparePicture(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/jpeg" {
		t.Errorf("expected image/jpeg re-encode, got %s", mime)
	}
	img, err := jpeg.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decode downscaled: %v", err)
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	total := w * h
	if total > int(float64(checkPictureTargetPixels)*1.05) || total < int(float64(checkPictureTargetPixels)*0.9) {
		t.Errorf("total pixels %d outside ~%d budget", total, checkPictureTargetPixels)
	}
	// Aspect ratio 2:1 must be preserved within rounding.
	ratio := float64(w) / float64(h)
	if ratio < 1.98 || ratio > 2.02 {
		t.Errorf("aspect ratio not preserved: %dx%d (ratio %f)", w, h, ratio)
	}
}

// Transparent PNGs are composited over white — a bare JPEG re-encode
// would render transparent regions black.
func TestPreparePicture_AlphaCompositedOnWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2600, 1300))
	for y := 0; y < 1300; y++ {
		for x := 0; x < 2600; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 0}) // fully transparent red
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("png encode: %v", err)
	}
	_, payload, err := preparePicture(buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("decode downscaled: %v", err)
	}
	r, g, b, _ := decoded.At(500, 500).RGBA()
	if r>>8 < 240 || g>>8 < 240 || b>>8 < 240 {
		t.Errorf("transparent area not composited on white: got (%d,%d,%d)", r>>8, g>>8, b>>8)
	}
}

func TestPreparePicture_WebPPassthrough(t *testing.T) {
	// Minimal WebP magic; the stdlib has no WebP decoder, so the
	// passthrough path must recognize it by sniffing.
	data := append([]byte("RIFF\x00\x00\x00\x00WEBP"), []byte("fake-webp-payload")...)
	mime, payload, err := preparePicture(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/webp" {
		t.Errorf("expected image/webp, got %s", mime)
	}
	if !bytes.Equal(payload, data) {
		t.Errorf("undecodable image should be sent as-is")
	}
}

func TestPreparePicture_UnknownFormat(t *testing.T) {
	_, _, err := preparePicture([]byte("definitely not an image"))
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported-format error, got %v", err)
	}
}

func TestSniffImageMime(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D}, "image/png"},
		{"gif", []byte("GIF89a"), "image/gif"},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBP"), "image/webp"},
		{"empty", []byte{}, ""},
		{"text", []byte("hello"), ""},
		{"riff-not-webp", []byte("RIFF\x00\x00\x00\x00WAVE"), ""},
	}
	for _, c := range cases {
		if got := sniffImageMime(c.data); got != c.want {
			t.Errorf("%s: expected %q, got %q", c.name, c.want, got)
		}
	}
}

func TestTools_CheckPictureRegistered(t *testing.T) {
	p := setupPictureProvider(t)
	var found bool
	for _, td := range p.Tools() {
		if td.Name != "check_picture" {
			continue
		}
		found = true
		schema, ok := td.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("expected InputSchema map, got %T", td.InputSchema)
		}
		required, _ := schema["required"].([]string)
		if len(required) != 2 || required[0] != "path" || required[1] != "question" {
			t.Errorf("expected required [path question], got %v", required)
		}
	}
	if !found {
		t.Errorf("check_picture not registered in Tools()")
	}
}

// stubPictureServer captures the request body and Authorization header
// and replies with a fixed answer.
func stubPictureServer(t *testing.T, answer string, gotAuth *string, gotBody *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotAuth = r.Header.Get("Authorization")
		data, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(data, gotBody)
		w.Header().Set("Content-Type", "application/json")
		resp, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": answer}},
			},
		})
		_, _ = w.Write(resp)
	}))
}

func stubResolver(endpoint string) func() (string, string, string, error) {
	return func() (string, string, string, error) {
		return endpoint, "test-key", "vision-model", nil
	}
}

func TestCallTool_CheckPicture_EndToEnd(t *testing.T) {
	p := setupPictureProvider(t)
	pngData := makePNG(t, 40, 30)
	writePicture(t, p, "pic.png", pngData)

	var gotAuth string
	var gotBody map[string]any
	srv := stubPictureServer(t, "a red square", &gotAuth, &gotBody)
	defer srv.Close()
	p.SetPictureAgentResolver(stubResolver(srv.URL))

	result, err := p.CallTool(context.Background(), "check_picture", map[string]any{
		"path":     "pic.png",
		"question": "what is this?",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content[0].Text != "a red square" {
		t.Errorf("expected sub-agent answer, got %q", result.Content[0].Text)
	}
	if gotAuth != "Bearer test-key" {
		t.Errorf("expected Bearer test-key, got %q", gotAuth)
	}
	if gotBody["model"] != "vision-model" {
		t.Errorf("expected model vision-model, got %v", gotBody["model"])
	}
	if _, ok := gotBody["max_tokens"]; ok {
		t.Errorf("request must not carry max_tokens")
	}
	messages, _ := gotBody["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %v", messages)
	}
	content, _ := messages[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %v", content)
	}
	textPart, _ := content[0].(map[string]any)
	if textPart["type"] != "text" || textPart["text"] != "what is this?" {
		t.Errorf("bad text part: %v", textPart)
	}
	imgPart, _ := content[1].(map[string]any)
	url, _ := imgPart["image_url"].(map[string]any)["url"].(string)
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("bad image_url: %.60s...", url)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(url, prefix))
	if err != nil || !bytes.Equal(raw, pngData) {
		t.Errorf("small image should be sent as-is: decode err=%v equal=%v", err, bytes.Equal(raw, pngData))
	}
}

func TestCallTool_CheckPicture_DefaultQuestion(t *testing.T) {
	p := setupPictureProvider(t)
	writePicture(t, p, "pic.png", makePNG(t, 10, 10))
	var gotAuth string
	var gotBody map[string]any
	srv := stubPictureServer(t, "ok", &gotAuth, &gotBody)
	defer srv.Close()
	p.SetPictureAgentResolver(stubResolver(srv.URL))

	if _, err := p.CallTool(context.Background(), "check_picture", map[string]any{"path": "pic.png"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content, _ := gotBody["messages"].([]any)[0].(map[string]any)["content"].([]any)
	textPart, _ := content[0].(map[string]any)
	if textPart["text"] != checkPictureDefaultQuestion {
		t.Errorf("expected default question, got %v", textPart["text"])
	}
}

func TestCallTool_CheckPicture_PathTraversal(t *testing.T) {
	p := setupPictureProvider(t)
	p.SetPictureAgentResolver(stubResolver("http://127.0.0.1:1"))
	result, err := p.CallTool(context.Background(), "check_picture", map[string]any{
		"path":     "../../secret.png",
		"question": "q",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "access denied") {
		t.Errorf("expected sandbox access denied, got %q", result.Content[0].Text)
	}
}

// An absolute path outside the root is rejected while the sandbox is
// enforced — same as the other sandbox tools.
func TestCallTool_CheckPicture_AbsolutePathOutsideRootDenied(t *testing.T) {
	p := setupPictureProvider(t)
	p.SetPictureAgentResolver(stubResolver("http://127.0.0.1:1"))

	outside := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(outside, makePNG(t, 10, 10), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := p.CallTool(context.Background(), "check_picture", map[string]any{
		"path":     outside,
		"question": "q",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "access denied") {
		t.Errorf("expected sandbox access denied, got %q", result.Content[0].Text)
	}
}

// With the sandbox disabled (raw shell enabled), an absolute path outside
// the root resolves exactly like it does for the other sandbox tools.
func TestCallTool_CheckPicture_AbsolutePathAllowedWhenSandboxDisabled(t *testing.T) {
	p := setupPictureProvider(t)
	p.SetSandboxDisabled(true)

	var gotAuth string
	var gotBody map[string]any
	srv := stubPictureServer(t, "a cat", &gotAuth, &gotBody)
	defer srv.Close()
	p.SetPictureAgentResolver(stubResolver(srv.URL))

	outside := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(outside, makePNG(t, 10, 10), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := p.CallTool(context.Background(), "check_picture", map[string]any{
		"path":     outside,
		"question": "q",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content[0].Text != "a cat" {
		t.Errorf("expected sub-agent answer, got %q", result.Content[0].Text)
	}
}

// An absolute path inside the root works but carries the same
// use-relative-path warning as the other sandbox tools.
func TestCallTool_CheckPicture_AbsolutePathInsideRootWarns(t *testing.T) {
	p := setupPictureProvider(t)
	writePicture(t, p, "pic.png", makePNG(t, 10, 10))
	var gotAuth string
	var gotBody map[string]any
	srv := stubPictureServer(t, "ok", &gotAuth, &gotBody)
	defer srv.Close()
	p.SetPictureAgentResolver(stubResolver(srv.URL))

	result, err := p.CallTool(context.Background(), "check_picture", map[string]any{
		"path":     filepath.Join(p.rootDir, "pic.png"),
		"question": "q",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "WARNING: Please use relative path.") {
		t.Errorf("expected relative-path warning, got %q", result.Content[0].Text)
	}
}

func TestCallTool_CheckPicture_MissingAPIKey(t *testing.T) {
	p := setupPictureProvider(t)
	writePicture(t, p, "pic.png", makePNG(t, 10, 10))
	p.SetPictureAgentResolver(func() (string, string, string, error) {
		return "http://127.0.0.1:1", "sk-xxxx", "m", nil
	})
	result, err := p.CallTool(context.Background(), "check_picture", map[string]any{"path": "pic.png", "question": "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "api_key") {
		t.Errorf("expected api_key error, got %q", result.Content[0].Text)
	}
}

func TestCallTool_CheckPicture_ResolverError(t *testing.T) {
	p := setupPictureProvider(t)
	writePicture(t, p, "pic.png", makePNG(t, 10, 10))
	p.SetPictureAgentResolver(func() (string, string, string, error) {
		return "", "", "", errors.New(`check_picture provider "gone" not found in model_providers`)
	})
	result, err := p.CallTool(context.Background(), "check_picture", map[string]any{"path": "pic.png", "question": "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "not found in model_providers") {
		t.Errorf("expected resolver error surfaced, got %q", result.Content[0].Text)
	}
}

func TestCallTool_CheckPicture_NoResolver(t *testing.T) {
	p := setupPictureProvider(t)
	writePicture(t, p, "pic.png", makePNG(t, 10, 10))
	result, err := p.CallTool(context.Background(), "check_picture", map[string]any{"path": "pic.png", "question": "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "no sub-agent resolver") {
		t.Errorf("expected no-resolver error, got %q", result.Content[0].Text)
	}
}

func TestCallTool_CheckPicture_HTTPError(t *testing.T) {
	p := setupPictureProvider(t)
	writePicture(t, p, "pic.png", makePNG(t, 10, 10))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad image"}}`))
	}))
	defer srv.Close()
	p.SetPictureAgentResolver(stubResolver(srv.URL))
	result, err := p.CallTool(context.Background(), "check_picture", map[string]any{"path": "pic.png", "question": "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Content[0].Text, "HTTP 400") || !strings.Contains(result.Content[0].Text, "bad image") {
		t.Errorf("expected HTTP error body surfaced, got %q", result.Content[0].Text)
	}
}

// The per-call root dir override (chat root dirs) applies to check_picture
// exactly like to the other sandbox tools.
func TestCallTool_CheckPicture_PerCallRootDir(t *testing.T) {
	p := setupPictureProvider(t)
	chatRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(chatRoot, "pic.png"), makePNG(t, 10, 10), 0644); err != nil {
		t.Fatal(err)
	}
	var gotAuth string
	var gotBody map[string]any
	srv := stubPictureServer(t, "found it", &gotAuth, &gotBody)
	defer srv.Close()
	p.SetPictureAgentResolver(stubResolver(srv.URL))

	ctx := WithRootDir(context.Background(), chatRoot)
	result, err := p.CallTool(ctx, "check_picture", map[string]any{"path": "pic.png", "question": "q"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Content[0].Text != "found it" {
		t.Errorf("expected sub-agent answer via per-call root, got %q", result.Content[0].Text)
	}
}
