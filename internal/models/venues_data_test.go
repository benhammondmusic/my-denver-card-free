package models

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// Guards against a bad scrape or hand edit breaking data/venues.json.
func TestVenuesData(t *testing.T) {
	raw, err := os.ReadFile("../../data/venues.json")
	if err != nil {
		t.Fatal(err)
	}
	var venues []Venue
	if err := json.Unmarshal(raw, &venues); err != nil {
		t.Fatalf("parse venues.json: %v", err)
	}
	if len(venues) < 10 {
		t.Fatalf("only %d venues; data likely truncated", len(venues))
	}
	months := []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
	for _, v := range venues {
		if v.Name == "" {
			t.Errorf("venue with empty name")
			continue
		}
		if v.Lat == 0 || v.Lng == 0 {
			t.Errorf("%s: missing lat/lng", v.Name)
		}
		if v.Program != ProgramMyDenverCard && v.Program != ProgramGeneral {
			t.Errorf("%s: unknown program %q", v.Name, v.Program)
		}
		switch v.FreeSchedule {
		case "", ScheduleDaily, ScheduleWeekends, ScheduleWeekendsAndBreaks:
		default:
			t.Errorf("%s: unknown free_schedule %q", v.Name, v.FreeSchedule)
		}
		for _, m := range v.FreeMonths {
			if !slices.Contains(months, m) {
				t.Errorf("%s: invalid month %q", v.Name, m)
			}
		}
	}
}
