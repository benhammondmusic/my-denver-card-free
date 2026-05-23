package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/benhammondmusic/my-denver-card-free/internal/models"
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

	for i := range venues {
		v := &venues[i]
		fmt.Printf("scraping: %s (%s)\n", v.Name, v.URL)

		// TODO: launch rod browser and navigate to v.URL
		// TODO: extract free month info and adult_free status from page
		// TODO: update v.FreeMonths, v.AdultFree, v.Notes
		// TODO: set v.LastChecked = time.Now()
		// TODO: on error, set v.ScrapeFailed = true and v.ScrapeError = err.Error()

		fmt.Printf("  -> would update free months: %v\n", v.FreeMonths)
	}

	out, err := json.MarshalIndent(venues, "", "  ")
	if err != nil {
		log.Fatalf("failed to marshal venues: %v", err)
	}

	if err := os.WriteFile("data/venues.json", out, 0644); err != nil {
		log.Fatalf("failed to write venues.json: %v", err)
	}

	fmt.Println("scrape complete (stub — no data was changed)")
}
