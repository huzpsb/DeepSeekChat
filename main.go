package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"hschat/internal/cli"
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

	prompt := flag.String("prompt", "", "Run in headless CLI mode with the given prompt")
	title := flag.String("title", "", "Chat title for CLI mode (defaults to timestamp)")
	flag.Parse()

	if *prompt != "" {
		if err := cli.Run(*prompt, *title); err != nil {
			fmt.Fprintf(os.Stderr, "CLI error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	regexp2.DefaultMatchTimeout = time.Second * 5
	srv := server.New(staticFiles)
	log.Println("DsChat starting on http://127.0.0.1:5233")
	log.Fatal(http.ListenAndServe(":5233", srv.Handler()))
}
