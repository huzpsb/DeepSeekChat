package jsrt

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/dop251/goja"

	"hschat/internal/builtin/sandbox"
)

type batchTask struct {
	url      string
	dir      string
	retries  int
	filename string
}

func (r *Runtime) ensureBatchInit() {
	r.dlMu.Lock()
	defer r.dlMu.Unlock()
	if r.dlInit {
		return
	}
	r.dlCtx, r.dlCancel = context.WithCancel(r.execCtx)
	r.dlCh = make(chan batchTask, 1000)
	for i := 0; i < 20; i++ {
		r.dlWg.Add(1)
		go r.batchWorker()
	}
	r.dlInit = true
}

func (r *Runtime) batchWorker() {
	defer r.dlWg.Done()
	for {
		select {
		case <-r.dlCtx.Done():
			return
		case task, ok := <-r.dlCh:
			if !ok {
				return
			}
			r.batchDownloadOne(task)
			atomic.AddInt32(&r.dlPending, -1)
		}
	}
}

func (r *Runtime) batchDownloadOne(task batchTask) {
	for attempt := 0; attempt <= task.retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		if r.tryBatchDownload(task) == nil {
			return
		}
	}
}

func (r *Runtime) tryBatchDownload(task batchTask) error {
	reqCtx, cancel := context.WithTimeout(r.dlCtx, 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, task.url, nil)
	if err != nil {
		return err
	}

	for k, v := range r.cfg.Headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	filename := task.filename
	if filename == "" {
		filename = filenameFromURL(task.url)
	}
	outPath := filepath.Join(task.dir, filename)

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, io.LimitReader(resp.Body, 50*1024*1024))
	return err
}

func filenameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "index"
	}
	base := filepath.Base(u.Path)
	if base == "." || base == "/" || base == "" {
		return "index"
	}
	return base
}

func (r *Runtime) webjsBatchDownloadAppend(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	r.ensureBatchInit()

	if len(call.Arguments) < 2 {
		panic(vm.NewGoError(fmt.Errorf("batch_download_append requires 2 arguments (url, dir)")))
	}

	rawURL := call.Argument(0).String()
	dirArg := call.Argument(1).String()

	resolvedDir, err := sandbox.SafePath(r.cfg.RootDir, dirArg)
	if err != nil {
		panic(vm.NewGoError(err))
	}

	info, err := os.Stat(resolvedDir)
	if err != nil || !info.IsDir() {
		panic(vm.NewGoError(fmt.Errorf("batch_download_append: not a valid directory: %s", dirArg)))
	}

	retries := 3
	if len(call.Arguments) >= 3 && !goja.IsUndefined(call.Argument(2)) && !goja.IsNull(call.Argument(2)) {
		exported := call.Argument(2).Export()
		switch v := exported.(type) {
		case float64:
			retries = int(v)
		case int64:
			retries = int(v)
		}
		if retries < 0 {
			retries = 0
		}
	}

	filename := ""
	if len(call.Arguments) >= 4 && !goja.IsUndefined(call.Argument(3)) && !goja.IsNull(call.Argument(3)) {
		filename = call.Argument(3).String()
	}

	if filename == "" {
		filename = filenameFromURL(rawURL)
	}

	outPath := filepath.Join(resolvedDir, filename)
	if _, err := os.Stat(outPath); err == nil {
		return goja.Undefined()
	}

	task := batchTask{
		url:      rawURL,
		dir:      resolvedDir,
		retries:  retries,
		filename: filename,
	}

	select {
	case r.dlCh <- task:
		atomic.AddInt32(&r.dlPending, 1)
	case <-r.dlCtx.Done():
		panic(vm.NewGoError(fmt.Errorf("batch_download_append: download pool has been cleared")))
	}

	return goja.Undefined()
}

func (r *Runtime) webjsBatchDownloadRemaining(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	r.dlMu.Lock()
	defer r.dlMu.Unlock()
	if !r.dlInit {
		return vm.ToValue(0)
	}
	return vm.ToValue(int(atomic.LoadInt32(&r.dlPending)))
}

func (r *Runtime) batchCleanup() {
	r.dlMu.Lock()
	defer r.dlMu.Unlock()
	if r.dlInit {
		r.dlCancel()
		close(r.dlCh)
		r.dlWg.Wait()
		r.dlInit = false
		r.dlPending = 0
	}
}

func (r *Runtime) webjsBatchDownloadClear(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	r.batchCleanup()
	return goja.Undefined()
}
