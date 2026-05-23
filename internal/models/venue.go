package models

import "time"

// Program values: "mydenvercard" = benefit specific to cardholders; "general" = free for all Denver kids regardless of card
type Program string

const (
	ProgramMyDenverCard Program = "mydenvercard"
	ProgramGeneral      Program = "general"
)

type Venue struct {
	Name               string    `json:"name"`
	URL                string    `json:"url"`
	Program            Program   `json:"program"`
	FreeMonths         []string  `json:"free_months"`
	AdultFree          bool      `json:"adult_free"`
	Notes              string    `json:"notes"`
	TemporarilyClosed  bool      `json:"temporarily_closed"`
	ClosureReason      string    `json:"closure_reason"`
	LastChecked        time.Time `json:"last_checked"`
	ScrapeFailed       bool      `json:"scrape_failed"`
	ScrapeError        string    `json:"scrape_error"`
}
