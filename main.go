package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	ilog "hschat/internal/log"
	"hschat/internal/server"

	"github.com/dlclark/regexp2"
)

//go:embed all:web all:assets
var staticFiles embed.FS

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC: %v\n%s", r, debug.Stack())
			ilog.Close()
			os.Exit(1)
		}
	}()

	regexp2.DefaultMatchTimeout = time.Second * 5
	srv := server.New(staticFiles)
	log.Println("DsChat starting on http://127.0.0.1:5233")
	log.Fatal(http.ListenAndServe(":5233", srv.Handler()))
}
