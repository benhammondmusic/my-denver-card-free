package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/benhammondmusic/my-denver-card-free/internal/models"
	"github.com/benhammondmusic/my-denver-card-free/templates"
)

func Index(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile("data/venues.json")
	if err != nil {
		log.Printf("failed to read venues.json: %v", err)
		http.Error(w, "could not load venue data", http.StatusInternalServerError)
		return
	}

	var venues []models.Venue
	if err := json.Unmarshal(data, &venues); err != nil {
		log.Printf("failed to unmarshal venues: %v", err)
		http.Error(w, "could not parse venue data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.Index(venues).Render(r.Context(), w); err != nil {
		log.Printf("failed to render template: %v", err)
	}
}
