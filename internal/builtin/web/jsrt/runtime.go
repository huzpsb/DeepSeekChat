package jsrt

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/proxy"

	"hschat/internal/builtin/sandbox"
)

type Config struct {
	RootDir string
	Headers map[string]string
	Proxy   string
}

type console struct {
	buf     bytes.Buffer
	maxSize int
	mu      sync.Mutex
	vm      *goja.Runtime
}

func newConsole(vm *goja.Runtime) *console {
	return &console{maxSize: 100 * 1024, vm: vm}
}

func (c *console) write(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buf.Len()+len(s) > c.maxSize {
		c.vm.Interrupt("console buffer overflow (100KB limit)")
		return
	}
	c.buf.WriteString(s)
}

func (c *console) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

type Runtime struct {
	cfg     *Config
	console *console
	client  *http.Client
}

func New(cfg *Config) *Runtime {
	r := &Runtime{cfg: cfg}
	r.client = r.createHTTPClient()
	return r
}

func (r *Runtime) Close() {
	r.client.CloseIdleConnections()
}

func (r *Runtime) Execute(fileName string, timeout time.Duration) (consoleOutput string, returnValue string, elapsed time.Duration, execErr error) {
	start := time.Now()

	path, err := sandbox.SafePath(r.cfg.RootDir, fileName)
	if err != nil {
		return "", "", time.Since(start), fmt.Errorf("failed to resolve script path: %w", err)
	}

	code, err := os.ReadFile(path)
	if err != nil {
		return "", "", time.Since(start), fmt.Errorf("failed to read script: %w", err)
	}

	vm := goja.New()
	r.console = newConsole(vm)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	go func() {
		<-ctx.Done()
		vm.Interrupt("execution timeout")
	}()

	r.registerFunctions(vm)

	val, err := vm.RunString(string(code))
	elapsed = time.Since(start)
	consoleOutput = r.console.String()

	if err != nil {
		return consoleOutput, "", elapsed, err
	}

	if val != nil && val != goja.Undefined() {
		returnValue = val.String()
	}

	return consoleOutput, returnValue, elapsed, nil
}

func (r *Runtime) registerFunctions(vm *goja.Runtime) {
	vm.Set("web_fetch", func(call goja.FunctionCall) goja.Value {
		return r.webFetch(vm, call)
	})

	vm.Set("webjs_tree", func(call goja.FunctionCall) goja.Value {
		return r.webjsTree(vm, call)
	})

	vm.Set("webjs_read", func(call goja.FunctionCall) goja.Value {
		return r.webjsRead(vm, call)
	})

	vm.Set("webjs_write", func(call goja.FunctionCall) goja.Value {
		return r.webjsWrite(vm, call)
	})

	vm.Set("webjs_delete", func(call goja.FunctionCall) goja.Value {
		return r.webjsDelete(vm, call)
	})

	vm.Set("webjs_create_folder", func(call goja.FunctionCall) goja.Value {
		return r.webjsCreateFolder(vm, call)
	})

	cons := vm.NewObject()
	cons.Set("log", func(call goja.FunctionCall) goja.Value {
		var parts []string
		for _, arg := range call.Arguments {
			parts = append(parts, arg.String())
		}
		r.console.write(strings.Join(parts, " ") + "\n")
		return goja.Undefined()
	})
	vm.Set("console", cons)
}

func (r *Runtime) createHTTPClient() *http.Client {
	transport := &http.Transport{}

	proxyURL, err := url.Parse(r.cfg.Proxy)
	hasProxy := err == nil && r.cfg.Proxy != ""

	if hasProxy && (proxyURL.Scheme == "http" || proxyURL.Scheme == "https") {
		transport.Proxy = http.ProxyURL(proxyURL)
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return r.dialTLSWithHTTPProxy(ctx, proxyURL, addr)
		}
	} else if hasProxy && proxyURL.Scheme == "socks5" {
		socksDialer, err := proxy.FromURL(proxyURL, proxy.Direct)
		if err == nil {
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				return socksDialer.Dial("tcp", addr)
			}
			transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := socksDialer.Dial("tcp", addr)
				if err != nil {
					return nil, err
				}
				return r.upgradeToUTLS(conn, addr)
			}
		}
	} else {
		transport.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				return nil, err
			}
			return r.upgradeToUTLS(conn, addr)
		}
	}

	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}

func (r *Runtime) dialTLSWithHTTPProxy(ctx context.Context, proxyURL *url.URL, addr string) (net.Conn, error) {
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		proxyAddr += ":80"
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to proxy: %w", err)
	}

	authHeader := ""
	if proxyURL.User != nil {
		authHeader = "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(proxyURL.User.String())) + "\r\n"
	}

	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", addr, addr, authHeader)
	if _, err := io.WriteString(conn, connectReq); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send CONNECT: %w", err)
	}

	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to read proxy CONNECT response: %w", err)
	}

	respLine := strings.SplitN(string(buf[:n]), "\r\n", 2)[0]
	if !strings.Contains(respLine, " 200 ") {
		conn.Close()
		return nil, fmt.Errorf("proxy CONNECT rejected: %s", respLine)
	}

	return r.upgradeToUTLS(conn, addr)
}

func (r *Runtime) upgradeToUTLS(conn net.Conn, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("invalid address: %w", err)
	}

	utlsConfig := &utls.Config{
		ServerName: host,
	}

	uconn := utls.UClient(conn, utlsConfig, utls.HelloCustom)
	spec, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to get chrome spec: %v", err)
	}
	for i, ext := range spec.Extensions {
		if alpn, ok := ext.(*utls.ALPNExtension); ok {
			alpn.AlpnProtocols = []string{"http/1.1"}
			spec.Extensions[i] = alpn
			break
		}
	}
	if err := uconn.ApplyPreset(&spec); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to apply preset: %v", err)
	}
	if err := uconn.Handshake(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("TLS handshake failed: %w", err)
	}

	return uconn, nil
}

func isTextContentType(contentType string) bool {
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return true
	}
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	switch mediaType {
	case "application/json", "application/xml", "application/javascript",
		"application/xhtml+xml", "application/x-www-form-urlencoded",
		"application/ld+json", "application/rss+xml", "application/atom+xml":
		return true
	}
	return false
}

func (r *Runtime) webFetch(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if len(call.Arguments) < 1 {
		panic(vm.NewGoError(fmt.Errorf("web_fetch requires at least 1 argument (url)")))
	}
	urlStr := call.Argument(0).String()

	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		panic(vm.NewGoError(fmt.Errorf("invalid URL: %w", err)))
	}

	if req.URL.Port() == "5233" {
		panic(vm.NewGoError(fmt.Errorf("access to port 5233 is blocked")))
	}

	for k, v := range r.cfg.Headers {
		req.Header.Set(k, v)
	}

	if len(call.Arguments) >= 2 && !goja.IsUndefined(call.Argument(1)) && !goja.IsNull(call.Argument(1)) {
		customHeaders := call.Argument(1).Export()
		headerMap, ok := customHeaders.(map[string]interface{})
		if !ok {
			panic(vm.NewGoError(fmt.Errorf("web_fetch second argument must be a headers object")))
		}
		for k, v := range headerMap {
			if strings.EqualFold(k, "User-Agent") {
				panic(vm.NewGoError(fmt.Errorf("User-Agent header is managed internally and cannot be overridden")))
			}
			req.Header.Set(k, fmt.Sprint(v))
		}
	}

	resp, err := r.client.Do(req)
	if err != nil {
		panic(vm.NewGoError(fmt.Errorf("fetch failed: %w", err)))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		panic(vm.NewGoError(fmt.Errorf("failed to read response body: %w", err)))
	}

	headers := make(map[string]string)
	for k := range resp.Header {
		headers[k] = resp.Header.Get(k)
	}

	var bodyVal interface{}
	contentType := resp.Header.Get("Content-Type")
	if isTextContentType(contentType) {
		bodyVal = string(body)
	} else {
		ab := vm.NewArrayBuffer(body)
		uint8Cons := vm.Get("Uint8Array")
		ua, err := vm.New(uint8Cons, vm.ToValue(ab))
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("failed to create Uint8Array: %w", err)))
		}
		bodyVal = ua
	}

	result := map[string]interface{}{
		"status":  resp.StatusCode,
		"body":    bodyVal,
		"headers": headers,
	}

	val := vm.ToValue(result)
	return val
}

func (r *Runtime) webjsTree(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	var dir string
	depth := 2

	for _, arg := range call.Arguments {
		if goja.IsUndefined(arg) || goja.IsNull(arg) {
			continue
		}
		exported := arg.Export()
		switch v := exported.(type) {
		case string:
			if v != "" {
				dir = v
			}
		case float64:
			d := int(v)
			if d < 1 {
				d = 1
			}
			if d > 10 {
				d = 10
			}
			depth = d
		case int64:
			d := int(v)
			if d < 1 {
				d = 1
			}
			if d > 10 {
				d = 10
			}
			depth = d
		}
	}

	startDir := r.cfg.RootDir
	if dir != "" {
		resolved, err := sandbox.SafePath(r.cfg.RootDir, dir)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			panic(vm.NewGoError(fmt.Errorf("webjs_tree: not a valid directory: %s", dir)))
		}
		startDir = resolved
	}

	var result []string
	_ = filepath.Walk(startDir, func(fp string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(startDir, fp)
		if rel == "." {
			return nil
		}
		if sandbox.IsIgnoredName(info.Name()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		sepCount := len(strings.Split(rel, string(os.PathSeparator)))
		if sepCount > depth {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			result = append(result, rel+"/")
		} else {
			result = append(result, rel)
		}
		return nil
	})

	val := vm.ToValue(result)
	return val
}

func (r *Runtime) webjsRead(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if len(call.Arguments) < 1 {
		panic(vm.NewGoError(fmt.Errorf("webjs_read requires 1 argument (path)")))
	}
	name := call.Argument(0).String()
	path, err := sandbox.SafePath(r.cfg.RootDir, name)
	if err != nil {
		panic(vm.NewGoError(err))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		panic(vm.NewGoError(fmt.Errorf("webjs_read failed: %w", err)))
	}
	return vm.ToValue(string(data))
}

func (r *Runtime) webjsWrite(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if len(call.Arguments) < 2 {
		panic(vm.NewGoError(fmt.Errorf("webjs_write requires 2 arguments (path, content)")))
	}
	name := call.Argument(0).String()
	content := call.Argument(1).String()
	path, err := sandbox.SafePath(r.cfg.RootDir, name)
	if err != nil {
		panic(vm.NewGoError(err))
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		panic(vm.NewGoError(fmt.Errorf("webjs_write failed: %w", err)))
	}
	return goja.Undefined()
}

func (r *Runtime) webjsDelete(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if len(call.Arguments) < 1 {
		panic(vm.NewGoError(fmt.Errorf("webjs_delete requires 1 argument (path)")))
	}
	name := call.Argument(0).String()
	path, err := sandbox.SafePath(r.cfg.RootDir, name)
	if err != nil {
		panic(vm.NewGoError(err))
	}

	info, err := os.Stat(path)
	if err != nil {
		panic(vm.NewGoError(fmt.Errorf("webjs_delete failed: %w", err)))
	}
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			panic(vm.NewGoError(fmt.Errorf("webjs_delete failed: %w", err)))
		}
		if len(entries) > 0 {
			panic(vm.NewGoError(fmt.Errorf("directory is not empty")))
		}
	}

	if err := os.Remove(path); err != nil {
		panic(vm.NewGoError(fmt.Errorf("webjs_delete failed: %w", err)))
	}
	return goja.Undefined()
}

func (r *Runtime) webjsCreateFolder(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	if len(call.Arguments) < 1 {
		panic(vm.NewGoError(fmt.Errorf("webjs_create_folder requires 1 argument (path)")))
	}
	name := call.Argument(0).String()
	path, err := sandbox.SafePath(r.cfg.RootDir, name)
	if err != nil {
		panic(vm.NewGoError(err))
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		panic(vm.NewGoError(fmt.Errorf("webjs_create_folder failed: %w", err)))
	}
	return goja.Undefined()
}
