package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // register decoder for image.Decode
	"image/jpeg"
	_ "image/png" // register decoder for image.Decode
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// checkPictureTargetPixels 是发给视觉子代理的图片总像素预算
	// （约相当于 1300×1300）。超过的图片按长宽比不变缩小到该量级。
	checkPictureTargetPixels = 1300 * 1300
	// checkPictureMaxFileBytes 拒绝读取超过该大小的文件：base64 之后还要
	// 再涨 1/3，再大的文件对视觉 API 也没有意义。
	checkPictureMaxFileBytes = 64 * 1024 * 1024
	// checkPictureTimeout 覆盖子代理整个 HTTP 调用（含图片上传）。
	checkPictureTimeout = 3 * time.Minute
	// checkPictureDefaultQuestion 是 question 参数缺省时的问题。
	checkPictureDefaultQuestion = "What is in this image? Please describe it in detail, including any text, objects, and anomalies(say, layout overlapping) you can find."
)

// checkPicture implements the check_picture tool: read an image file,
// downscale it to the pixel budget, and ask a configurable vision
// sub-agent (provider/model resolved live from the model_providers
// registry) the given question. Path handling is identical to the other
// sandbox tools — getSafePathWithWarning applies the live
// sandbox-disabled state and the traversal checks.
func (p *Provider) checkPicture(ctx context.Context, args map[string]any) string {
	path, _ := args["path"].(string)
	if path == "" {
		return "Error: check_picture: 'path' argument is required"
	}
	question, _ := args["question"].(string)
	if question == "" {
		question = checkPictureDefaultQuestion
	}

	absPath, warn, err := p.getSafePathWithWarning(path)
	if err != nil {
		return fmt.Sprintf("Error: check_picture: %v", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Sprintf("Error: check_picture: %v", err)
	}
	if info.IsDir() {
		return fmt.Sprintf("Error: check_picture: %s is a directory", path)
	}
	if info.Size() > checkPictureMaxFileBytes {
		return fmt.Sprintf("Error: check_picture: file too large (%d bytes, limit %d)", info.Size(), checkPictureMaxFileBytes)
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Sprintf("Error: check_picture: %v", err)
	}

	mime, payload, err := preparePicture(data)
	if err != nil {
		return fmt.Sprintf("Error: check_picture: %v", err)
	}

	resolver := p.getPictureResolver()
	if resolver == nil {
		return "Error: check_picture: no sub-agent resolver configured"
	}
	endpoint, apiKey, modelName, err := resolver()
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	if apiKey == "" || apiKey == "sk-xxxx" {
		return "Error: check_picture: provider has no api_key configured (edit it in Preferences or config.json)"
	}

	answer, err := askPictureAgent(ctx, endpoint, apiKey, modelName, mime, payload, question)
	if err != nil {
		return fmt.Sprintf("Error: check_picture: %v", err)
	}
	// Mirror the other sandbox tools: an absolute path works but is
	// discouraged with a visible warning.
	if warn && !p.sandboxDisabled {
		answer += "\nWARNING: Please use relative path."
	}
	return answer
}

// preparePicture returns the MIME type and bytes to send. Images the
// stdlib can decode and that exceed the pixel budget are downscaled
// (aspect ratio preserved) and re-encoded as JPEG; anything else
// (already small, or an undecodable-but-recognizable format like WebP)
// is sent as-is.
func preparePicture(data []byte) (mime string, payload []byte, err error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		if m := sniffImageMime(data); m != "" {
			return m, data, nil
		}
		return "", nil, fmt.Errorf("unsupported or corrupt image format")
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w <= 0 || h <= 0 {
		return "", nil, fmt.Errorf("image has empty dimensions (%dx%d)", w, h)
	}
	if w*h <= checkPictureTargetPixels {
		return "image/" + format, data, nil
	}
	scale := math.Sqrt(float64(checkPictureTargetPixels) / float64(w*h))
	nw := max(1, int(math.Round(float64(w)*scale)))
	nh := max(1, int(math.Round(float64(h)*scale)))
	dst := downscaleBilinear(img, nw, nh)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 90}); err != nil {
		return "", nil, fmt.Errorf("re-encode downscaled image: %w", err)
	}
	return "image/jpeg", buf.Bytes(), nil
}

// sniffImageMime recognizes common image magic bytes for files the
// stdlib cannot decode (e.g. WebP), so they can still be sent verbatim.
func sniffImageMime(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 4 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G':
		return "image/png"
	case len(data) >= 4 && string(data[:4]) == "GIF8":
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

// bilinearAxis maps a destination pixel coordinate to the two source
// sample indices and the fractional position between them, for bilinear
// interpolation along one axis. Indices are clamped to [0, srcLen-1].
func bilinearAxis(dstPos, srcLen, dstLen int) (i0, i1 int, frac float64) {
	f := (float64(dstPos)+0.5)*float64(srcLen)/float64(dstLen) - 0.5
	i0 = int(math.Floor(f))
	frac = f - float64(i0)
	i1 = i0 + 1
	if i0 < 0 {
		i0 = 0
	}
	if i1 > srcLen-1 {
		i1 = srcLen - 1
	}
	return i0, i1, frac
}

// downscaleBilinear resizes src to exactly w×h with bilinear
// interpolation, compositing alpha over white so the JPEG re-encode does
// not turn transparent regions black. Only used for downscaling.
func downscaleBilinear(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	at := func(x, y int) color.NRGBA {
		return color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		y0, y1, dy := bilinearAxis(y, sh, h)
		for x := 0; x < w; x++ {
			x0, x1, dx := bilinearAxis(x, sw, w)

			c00, c10 := at(x0, y0), at(x1, y0)
			c01, c11 := at(x0, y1), at(x1, y1)
			lerp := func(a, b, a2, b2 float64) float64 {
				top := a + (b-a)*dx
				bot := a2 + (b2-a2)*dx
				return top + (bot-top)*dy
			}
			r := lerp(float64(c00.R), float64(c10.R), float64(c01.R), float64(c11.R))
			g := lerp(float64(c00.G), float64(c10.G), float64(c01.G), float64(c11.G))
			bl := lerp(float64(c00.B), float64(c10.B), float64(c01.B), float64(c11.B))
			a := lerp(float64(c00.A), float64(c10.A), float64(c01.A), float64(c11.A))

			// Composite over white: out = src*a + 255*(1-a).
			cov := a / 255
			white := 1 - cov
			off := dst.PixOffset(x, y)
			dst.Pix[off+0] = uint8(r*cov + 255*white)
			dst.Pix[off+1] = uint8(g*cov + 255*white)
			dst.Pix[off+2] = uint8(bl*cov + 255*white)
			dst.Pix[off+3] = 0xFF
		}
	}
	return dst
}

// askPictureAgent performs one non-streaming OpenAI-compatible vision
// request. No max_tokens is sent — the sub-agent is configured entirely
// by provider/model selection. 429s are retried immediately a few times,
// mirroring the main LLM client.
func askPictureAgent(ctx context.Context, endpoint, apiKey, modelName, mime string, payload []byte, question string) (string, error) {
	reqBody := map[string]any{
		"model": modelName,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": question},
				{"type": "image_url", "image_url": map[string]string{
					"url": "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(payload),
				}},
			},
		}},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, checkPictureTimeout)
	defer cancel()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = time.Minute
	client := &http.Client{Transport: transport}

	const maxRetries = 3
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
		if err != nil {
			return "", fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err = client.Do(req)
		if err != nil {
			return "", fmt.Errorf("request failed: %w", err)
		}
		if resp.StatusCode != http.StatusTooManyRequests || attempt >= maxRetries {
			break
		}
		resp.Body.Close()
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateForError(string(respBody)))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("empty response from sub-agent")
	}
	return parsed.Choices[0].Message.Content, nil
}

// truncateForError keeps HTTP error bodies in tool results short: a
// gateway HTML error page is useless to the model and wastes context.
func truncateForError(s string) string {
	const maxLen = 1024
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...[truncated]"
}
