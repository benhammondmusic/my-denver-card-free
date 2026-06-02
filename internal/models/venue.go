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

type PoolSession struct {
	FamilyFriendly bool     `json:"family_friendly"`
	Days           []string `json:"days"`  // lowercase three-letter: "mon","tue","wed","thu","fri","sat","sun"
	Open           string   `json:"open"`  // 24h "HH:MM"
	Close          string   `json:"close"` // 24h "HH:MM"
}

type Pool struct {
	Name           string        `json:"name"`
	FamilyFriendly bool          `json:"family_friendly"`
	Features       []string      `json:"features,omitempty"`
	Notes          string        `json:"notes,omitempty"`
	SeasonLabel    string        `json:"season_label,omitempty"`
	SeasonStart    string        `json:"season_start,omitempty"` // "YYYY-MM-DD"
	SeasonEnd      string        `json:"season_end,omitempty"`   // "YYYY-MM-DD"
	CanvaURL       string        `json:"canva_url,omitempty"`
	Sessions       []PoolSession `json:"sessions"`
}

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
	Category            string       `json:"category,omitempty"`
	Phone               string       `json:"phone,omitempty"`
	Activities          []string     `json:"activities,omitempty"`
	Pools               []Pool       `json:"pools,omitempty"`
	Playground          bool         `json:"playground,omitempty"`
	EVCharger           bool         `json:"ev_charger,omitempty"`
}
