package main

import (
	"log"
	"net/http"
	"os"

	"github.com/benhammondmusic/my-denver-card-free/internal/handlers"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handlers.Index)
	mux.Handle("GET /", http.FileServer(http.Dir("docs")))

	addr := ":" + port
	log.Printf("server listening on http://localhost%s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
