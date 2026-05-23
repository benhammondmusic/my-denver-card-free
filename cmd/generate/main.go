package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/benhammondmusic/my-denver-card-free/internal/models"
	"github.com/benhammondmusic/my-denver-card-free/templates"
)

func main() {
	data, err := os.ReadFile("data/venues.json")
	if err != nil {
		log.Fatalf("failed to read venues.json: %v", err)
	}

	var venues []models.Venue
	if err := json.Unmarshal(data, &venues); err != nil {
		log.Fatalf("failed to unmarshal venues: %v", err)
	}

	if err := os.MkdirAll("docs", 0755); err != nil {
		log.Fatalf("failed to create docs dir: %v", err)
	}

	f, err := os.Create("docs/index.html")
	if err != nil {
		log.Fatalf("failed to create public/index.html: %v", err)
	}
	defer f.Close()

	if err := templates.Index(venues).Render(context.Background(), f); err != nil {
		log.Fatalf("failed to render template: %v", err)
	}

	log.Println("generated docs/index.html")
}
