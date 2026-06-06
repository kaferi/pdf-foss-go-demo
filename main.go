package main

import (
	"log"
	"net/http"
	"os"

	"pdf-foss-demo/internal/renderer"
	"pdf-foss-demo/internal/server"
	"pdf-foss-demo/internal/storage"
)

func main() {
	dataDir := getenv("DATA_DIR", "/data")
	webDir := getenv("WEB_DIR", "web")
	addr := getenv("ADDR", ":8080")

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("cannot create data dir %q: %v", dataDir, err)
	}

	store := storage.New(dataDir)
	rnd := renderer.New(store)
	srv := server.New(store, rnd, webDir)

	log.Printf("listening on %s (data=%s web=%s)", addr, dataDir, webDir)
	log.Fatal(http.ListenAndServe(addr, srv))
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
