package models

import "time"

type Venue struct {
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	FreeMonths   []string  `json:"free_months"`
	AdultFree    bool      `json:"adult_free"`
	Notes        string    `json:"notes"`
	LastChecked  time.Time `json:"last_checked"`
	ScrapeFailed bool      `json:"scrape_failed"`
	ScrapeError  string    `json:"scrape_error"`
}
