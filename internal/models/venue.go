package models

import "time"

// Program values: "mydenvercard" = benefit specific to cardholders; "general" = free for all Denver kids regardless of card
type Program string

const (
	ProgramMyDenverCard Program = "mydenvercard"
	ProgramGeneral      Program = "general"
)

// FreeSchedule values: "daily" = every day of free_months; "weekends" = Fri/Sat/Sun only; "weekends_and_breaks" = weekends + school break periods
type FreeSchedule string

const (
	ScheduleDaily             FreeSchedule = "daily"
	ScheduleWeekends          FreeSchedule = "weekends"
	ScheduleWeekendsAndBreaks FreeSchedule = "weekends_and_breaks"
)

type Venue struct {
	Name                string       `json:"name"`
	URL                 string       `json:"url"`
	Program             Program      `json:"program"`
	Featured            bool         `json:"featured"`
	FreeMonths          []string     `json:"free_months"`
	FreeSchedule        FreeSchedule `json:"free_schedule"`
	AdultsIncluded      int          `json:"adults_included"` // 0 = none, 1 = one adult, 2 = two adults, etc.
	Notes               string       `json:"notes"`
	TemporarilyClosed   bool         `json:"temporarily_closed"`
	ClosureReason       string       `json:"closure_reason"`
	LastChecked         time.Time    `json:"last_checked"`
	ScrapeFailed        bool         `json:"scrape_failed"`
	ScrapeError         string       `json:"scrape_error"`
	Lat                 float64      `json:"lat"`
	Lng                 float64      `json:"lng"`
	Address             string       `json:"address"`
	Hours               string       `json:"hours"`
	Indoor              bool         `json:"indoor"`
	ReservationRequired bool         `json:"reservation_required"`
	MinAge              int          `json:"min_age"` // 0 = no minimum
	MaxAge              int          `json:"max_age"` // 0 = no maximum
}
