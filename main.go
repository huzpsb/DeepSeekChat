package main

import (
	"embed"
	"log"
	"net/http"

	"hschat/internal/server"
)

//go:embed all:web all:assets
var staticFiles embed.FS

func main() {
	srv := server.New(staticFiles)
	log.Println("DsChat starting on http://127.0.0.1:5233")
	log.Fatal(http.ListenAndServe(":5233", srv.Handler()))
}
