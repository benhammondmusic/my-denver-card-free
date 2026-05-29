package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/benhammondmusic/my-denver-card-free/internal/models"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const (
	pageTimeout     = 30 * time.Second
	canvaTimeout    = 45 * time.Second
	canvaRenderWait = 3 * time.Second
	llmModel        = "claude-haiku-4-5-20251001"
	maxPageChars    = 6000
)

// scraped holds fields the LLM may return as changed for regular venues.
type scraped struct {
	FreeMonths        []string            `json:"free_months,omitempty"`
	FreeSchedule      models.FreeSchedule `json:"free_schedule,omitempty"`
	AdultsIncluded    *int                `json:"adults_included,omitempty"`
	Notes             string              `json:"notes,omitempty"`
	TemporarilyClosed *bool               `json:"temporarily_closed,omitempty"`
	ClosureReason     string              `json:"closure_reason,omitempty"`
	Uncertain         bool                `json:"uncertain,omitempty"`
}

// ---- POOL SCHEDULE PARSING ----

var (
	// Matches "2:00 PM", "10:30 AM", "14:00"
	reTime = regexp.MustCompile(`(?i)\b(\d{1,2}):(\d{2})\s*(am|pm)?\b`)

	// Session type keywords mapped to canonical SessionType
	sessionKeywords = []struct {
		words []string
		typ   models.SessionType
	}{
		{[]string{"family"}, models.SessionFamilySwim},
		{[]string{"lap"}, models.SessionLapSwim},
		{[]string{"adult"}, models.SessionAdultSwim},
		{[]string{"quiet"}, models.SessionQuietSwim},
		{[]string{"aqua", "fitness", "aerobics"}, models.SessionAquaFitness},
		{[]string{"lesson", "learn"}, models.SessionSwimLessons},
		{[]string{"team", "club", "practice"}, models.SessionSwimTeam},
		{[]string{"open", "swim", "public", "recreational"}, models.SessionOpenSwim},
	}

	// Day name -> 3-letter abbrev
	dayMap = map[string]string{
		"monday": "mon", "mon": "mon", "m": "mon",
		"tuesday": "tue", "tue": "tue", "tu": "tue", "tues": "tue",
		"wednesday": "wed", "wed": "wed", "w": "wed",
		"thursday": "thu", "thu": "thu", "th": "thu", "thur": "thu", "thurs": "thu",
		"friday": "fri", "fri": "fri", "f": "fri",
		"saturday": "sat", "sat": "sat", "sa": "sat",
		"sunday": "sun", "sun": "sun", "su": "sun",
	}

	dayOrder = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
)

func main() {
	raw, err := os.ReadFile("data/venues.json")
	if err != nil {
		log.Fatalf("read venues.json: %v", err)
	}
	var venues []models.Venue
	if err := json.Unmarshal(raw, &venues); err != nil {
		log.Fatalf("unmarshal venues: %v", err)
	}

	browser := makeBrowser()
	defer browser.MustClose()

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		log.Println("warning: ANTHROPIC_API_KEY not set; LLM fallback disabled")
	}

	// Pass 1: scrape regular venue metadata (skip pool venues - generic URL).
	for i := range venues {
		v := &venues[i]
		if v.Category == "pool" {
			continue
		}
		if v.TemporarilyClosed {
			log.Printf("scraping (watching for reopen): %s", v.Name)
		} else {
			log.Printf("scraping: %s", v.Name)
		}
		if err := scrapeVenue(browser, apiKey, v); err != nil {
			v.ScrapeFailed = true
			v.ScrapeError = err.Error()
			log.Printf("  FAIL: %v", err)
		} else {
			v.ScrapeFailed = false
			v.ScrapeError = ""
			log.Printf("  ok")
		}
		v.LastChecked = time.Now().UTC()
	}

	// Pass 2: scrape pool session schedules directly from Canva HTML tables.
	for i := range venues {
		v := &venues[i]
		if v.Category != "pool" {
			continue
		}
		for j := range v.Pools {
			pool := &v.Pools[j]
			if pool.CanvaURL == "" {
				continue
			}
			log.Printf("scraping pool schedule: %s", v.Name)
			if err := scrapePoolSchedule(browser, pool); err != nil {
				log.Printf("  FAIL: %v", err)
			} else {
				log.Printf("  ok (%d sessions)", len(pool.Sessions))
			}
		}
		v.LastChecked = time.Now().UTC()
	}

	out, err := json.MarshalIndent(venues, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile("data/venues.json", out, 0644); err != nil {
		log.Fatalf("write venues.json: %v", err)
	}
	log.Println("scrape complete")
}

func makeBrowser() *rod.Browser {
	if bin := os.Getenv("CHROME_BIN"); bin != "" {
		u := launcher.New().Bin(bin).NoSandbox(true).MustLaunch()
		return rod.New().ControlURL(u).MustConnect()
	}
	return rod.New().MustConnect()
}

// scrapePoolSchedule navigates to the Canva schedule page, reads the HTML
// tables directly from the DOM, and populates pool.Sessions.
func scrapePoolSchedule(browser *rod.Browser, pool *models.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), canvaTimeout)
	defer cancel()

	page, err := browser.Page(proto.TargetCreateTarget{URL: pool.CanvaURL})
	if err != nil {
		return fmt.Errorf("open canva page: %w", err)
	}
	defer page.MustClose()
	page = page.Context(ctx)

	if err := page.WaitLoad(); err != nil {
		return fmt.Errorf("canva page load: %w", err)
	}
	time.Sleep(canvaRenderWait)

	// Extract every table on the page as a 2-D slice of cell text.
	tables, err := extractTables(page)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return fmt.Errorf("no tables found on canva page")
	}

	sessions, err := parseSessions(tables)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		return fmt.Errorf("table found but no sessions parsed")
	}

	pool.Sessions = sessions
	return nil
}

// extractTables walks every <table> on the page and returns each as a slice
// of rows, each row a slice of trimmed cell strings.
func extractTables(page *rod.Page) ([][][]string, error) {
	tables, err := page.Elements("table")
	if err != nil || len(tables) == 0 {
		return nil, fmt.Errorf("no <table> elements found")
	}
	var out [][][]string
	for _, t := range tables {
		rows, _ := t.Elements("tr")
		var tbl [][]string
		for _, row := range rows {
			cells, _ := row.Elements("td, th")
			var r []string
			for _, cell := range cells {
				txt, _ := cell.Text()
				r = append(r, strings.TrimSpace(txt))
			}
			if len(r) > 0 {
				tbl = append(tbl, r)
			}
		}
		if len(tbl) > 1 { // at least a header row + one data row
			out = append(out, tbl)
		}
	}
	return out, nil
}

// parseSessions tries each extracted table until one yields sessions.
func parseSessions(tables [][][]string) ([]models.PoolSession, error) {
	for _, tbl := range tables {
		sessions, err := parseTable(tbl)
		if err == nil && len(sessions) > 0 {
			return sessions, nil
		}
		log.Printf("  table parse attempt failed: %v", err)
	}
	return nil, fmt.Errorf("no table yielded parseable sessions")
}

// parseTable handles two common Denver pool schedule formats:
//
//	Format A (day-grid): header row contains day names as column headers.
//	  Row 0: ["Session", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
//	  Row N: ["Open Swim", "2:00-5:00 PM", "2:00-5:00 PM", "", ...]
//
//	Format B (row-per-session): each row describes one session block.
//	  ["Open Swim", "Mon/Wed/Fri", "2:00 PM", "5:00 PM"]
func parseTable(tbl [][]string) ([]models.PoolSession, error) {
	if len(tbl) < 2 {
		return nil, fmt.Errorf("table has fewer than 2 rows")
	}
	header := tbl[0]

	// Detect Format A: does the header contain recognisable day names?
	dayColIndex := detectDayColumns(header)
	if len(dayColIndex) >= 2 {
		return parseDayGrid(tbl, dayColIndex)
	}

	// Fall back to Format B.
	return parseRowPerSession(tbl)
}

// detectDayColumns returns a map of column index -> day abbreviation for any
// column whose header text looks like a day of the week.
func detectDayColumns(header []string) map[int]string {
	m := make(map[int]string)
	for i, h := range header {
		if abbr, ok := dayMap[strings.ToLower(strings.TrimSpace(h))]; ok {
			m[i] = abbr
		}
	}
	return m
}

// parseDayGrid handles Format A tables.
// Each data row is a session type; each day column contains a time range or is empty.
func parseDayGrid(tbl [][]string, dayCol map[int]string) ([]models.PoolSession, error) {
	// group: sessionType+open+close -> []day
	type key struct{ typ, open, close string }
	grouped := make(map[key][]string)
	var keyOrder []key

	for _, row := range tbl[1:] {
		if len(row) == 0 {
			continue
		}
		sessionType := detectSessionType(row[0])
		for colIdx, day := range dayCol {
			if colIdx >= len(row) {
				continue
			}
			cell := strings.TrimSpace(row[colIdx])
			if cell == "" || cell == "-" || cell == "–" || cell == "closed" {
				continue
			}
			open, close, err := parseTimeRange(cell)
			if err != nil {
				continue
			}
			k := key{string(sessionType), open, close}
			if _, exists := grouped[k]; !exists {
				keyOrder = append(keyOrder, k)
			}
			grouped[k] = append(grouped[k], day)
		}
	}

	if len(grouped) == 0 {
		return nil, fmt.Errorf("day-grid: no sessions extracted")
	}

	var sessions []models.PoolSession
	for _, k := range keyOrder {
		days := sortDays(grouped[k])
		sessions = append(sessions, models.PoolSession{
			Type:  models.SessionType(k.typ),
			Days:  days,
			Open:  k.open,
			Close: k.close,
		})
	}
	return sessions, nil
}

// parseRowPerSession handles Format B tables.
// Each row is expected to have: [session type, days, open time, close time]
// or: [session type, days, "time - time"].
func parseRowPerSession(tbl [][]string) ([]models.PoolSession, error) {
	var sessions []models.PoolSession
	for _, row := range tbl {
		if len(row) < 2 {
			continue
		}
		sessionType := detectSessionType(row[0])
		if sessionType == "" {
			continue
		}

		var days []string
		var open, close string

		switch {
		case len(row) >= 4:
			// [type, days, open, close]
			days = parseDays(row[1])
			open = parseTimeStr(row[2])
			close = parseTimeStr(row[3])
		case len(row) == 3:
			// [type, days, "open - close"]
			days = parseDays(row[1])
			var err error
			open, close, err = parseTimeRange(row[2])
			if err != nil {
				continue
			}
		case len(row) == 2:
			// [type, "days open - close"] — try to split
			days = parseDays(row[1])
			open, close, _ = parseTimeRange(row[1])
		}

		if len(days) == 0 || open == "" || close == "" {
			continue
		}
		sessions = append(sessions, models.PoolSession{
			Type:  sessionType,
			Days:  days,
			Open:  open,
			Close: close,
		})
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("row-per-session: no sessions extracted")
	}
	return sessions, nil
}

// detectSessionType returns the best-matching SessionType for a string,
// or SessionOpenSwim as the default when nothing else matches.
func detectSessionType(s string) models.SessionType {
	lower := strings.ToLower(s)
	for _, kw := range sessionKeywords {
		for _, word := range kw.words {
			if strings.Contains(lower, word) {
				return kw.typ
			}
		}
	}
	// Skip rows that look like headers or noise
	if lower == "" || strings.Contains(lower, "session") || strings.Contains(lower, "type") {
		return ""
	}
	return models.SessionOpenSwim
}

// parseDays extracts recognised day abbreviations from a free-form string
// like "Mon/Wed/Fri", "Monday - Friday", "M, T, W".
func parseDays(s string) []string {
	seen := make(map[string]bool)
	var days []string
	// Expand "Mon-Fri" style ranges
	s = expandDayRanges(s)
	// Tokenise on common separators
	tokens := regexp.MustCompile(`[\s,/|&]+`).Split(strings.ToLower(s), -1)
	for _, t := range tokens {
		t = strings.Trim(t, ".")
		if abbr, ok := dayMap[t]; ok && !seen[abbr] {
			seen[abbr] = true
			days = append(days, abbr)
		}
	}
	return sortDays(days)
}

// expandDayRanges converts "Mon-Fri" into "Mon Tue Wed Thu Fri".
func expandDayRanges(s string) string {
	re := regexp.MustCompile(`(?i)(mon|tue|wed|thu|fri|sat|sun)\s*[-–]\s*(mon|tue|wed|thu|fri|sat|sun)`)
	return re.ReplaceAllStringFunc(s, func(m string) string {
		parts := re.FindStringSubmatch(m)
		if len(parts) < 3 {
			return m
		}
		start := dayAbbr(parts[1])
		end := dayAbbr(parts[2])
		si, ei := dayIndex(start), dayIndex(end)
		if si < 0 || ei < 0 || si > ei {
			return m
		}
		return strings.Join(dayOrder[si:ei+1], " ")
	})
}

// parseTimeRange extracts open/close from strings like:
// "2:00 PM - 5:00 PM", "14:00-17:00", "2-5pm", "2:00PM-5:30PM".
func parseTimeRange(s string) (open, close string, err error) {
	// Normalise separators
	s = strings.ReplaceAll(s, "–", "-")
	s = strings.ReplaceAll(s, " to ", " - ")

	// Split on dash that separates two times (not within a single time like "2:00")
	// Try "HH:MM - HH:MM" or "HH:MM-HH:MM" patterns
	reSplit := regexp.MustCompile(`(?i)(\d{1,2}(?::\d{2})?\s*(?:am|pm)?)\s*[-–]\s*(\d{1,2}(?::\d{2})?\s*(?:am|pm)?)`)
	m := reSplit.FindStringSubmatch(s)
	if len(m) < 3 {
		return "", "", fmt.Errorf("no time range in %q", s)
	}
	open = parseTimeStr(m[1])
	close = parseTimeStr(m[2])
	if open == "" || close == "" {
		return "", "", fmt.Errorf("could not parse times in %q", s)
	}

	// If only one AM/PM suffix was given (e.g. "2:00-5:00 PM"), apply it to both.
	if !strings.Contains(strings.ToLower(m[1]), "am") && !strings.Contains(strings.ToLower(m[1]), "pm") {
		suffix := extractAmPm(m[2])
		if suffix != "" {
			open = parseTimeStr(m[1] + " " + suffix)
		}
	}
	return open, close, nil
}

// parseTimeStr converts "2:00 PM" or "14:00" to 24-hour "HH:MM".
func parseTimeStr(s string) string {
	s = strings.TrimSpace(s)
	m := reTime.FindStringSubmatch(s)
	if len(m) < 3 {
		return ""
	}
	h := atoi(m[1])
	min := atoi(m[2])
	ampm := strings.ToLower(strings.TrimSpace(m[3]))

	if ampm == "pm" && h != 12 {
		h += 12
	} else if ampm == "am" && h == 12 {
		h = 0
	}
	return fmt.Sprintf("%02d:%02d", h, min)
}

func extractAmPm(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "pm") {
		return "pm"
	}
	if strings.Contains(lower, "am") {
		return "am"
	}
	return ""
}

func dayAbbr(s string) string {
	if a, ok := dayMap[strings.ToLower(s)]; ok {
		return a
	}
	return strings.ToLower(s)
}

func dayIndex(abbr string) int {
	for i, d := range dayOrder {
		if d == abbr {
			return i
		}
	}
	return -1
}

func sortDays(days []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, d := range dayOrder {
		for _, day := range days {
			if day == d && !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	return out
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// ---- REGULAR VENUE SCRAPING ----

func scrapeVenue(browser *rod.Browser, apiKey string, v *models.Venue) error {
	ctx, cancel := context.WithTimeout(context.Background(), pageTimeout)
	defer cancel()

	page, err := browser.Page(proto.TargetCreateTarget{URL: v.URL})
	if err != nil {
		return fmt.Errorf("open page: %w", err)
	}
	defer page.MustClose()
	page = page.Context(ctx)

	if err := page.WaitLoad(); err != nil {
		return fmt.Errorf("page load timeout: %w", err)
	}

	body, err := page.Element("body")
	if err != nil {
		return fmt.Errorf("no body element: %w", err)
	}
	pageText, err := body.Text()
	if err != nil {
		return fmt.Errorf("body text: %w", err)
	}
	if len(pageText) > maxPageChars {
		pageText = pageText[:maxPageChars] + "\n[truncated]"
	}

	if apiKey != "" && !programmaticMatchOK(v, pageText) {
		log.Printf("  programmatic check uncertain; calling LLM")
		return checkWithLLM(ctx, apiKey, v, pageText)
	}

	return nil
}

func programmaticMatchOK(v *models.Venue, pageText string) bool {
	if v.Notes == "" {
		return false
	}
	lower := strings.ToLower(pageText)
	words := strings.Fields(v.Notes)
	if len(words) < 4 {
		return false
	}
	matches := 0
	total := 0
	for i := 0; i+2 < len(words); i++ {
		phrase := strings.ToLower(strings.Join(words[i:i+3], " "))
		total++
		if strings.Contains(lower, phrase) {
			matches++
		}
	}
	return total > 0 && float64(matches)/float64(total) >= 0.5
}

func checkWithLLM(ctx context.Context, apiKey string, v *models.Venue, pageText string) error {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	currentJSON, _ := json.MarshalIndent(map[string]any{
		"free_months":        v.FreeMonths,
		"free_schedule":      v.FreeSchedule,
		"adults_included":    v.AdultsIncluded,
		"notes":              v.Notes,
		"temporarily_closed": v.TemporarilyClosed,
		"closure_reason":     v.ClosureReason,
	}, "", "  ")

	prompt := fmt.Sprintf(`You are verifying free admission details for a Denver cultural venue.

Venue: %s
URL: %s

Current recorded data:
%s

Page content:
%s

Return a JSON object with ONLY the fields that differ from the current data, or {} if nothing has changed.
Valid fields: free_months (array of full English month names), free_schedule ("daily"/"weekends"/"weekends_and_breaks"), adults_included (integer 0-2), notes (string, no em dashes), temporarily_closed (boolean), closure_reason (string).
If the venue is currently recorded as temporarily_closed=true but the page shows it is now open, return temporarily_closed=false along with any updated free admission details you can find.
If you cannot determine whether anything has changed, return {"uncertain":true}.
Return only valid JSON, no other text.`,
		v.Name, v.URL, string(currentJSON), pageText)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(llmModel),
		MaxTokens: 512,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return fmt.Errorf("LLM request failed: %w", err)
	}

	var responseText string
	for _, block := range msg.Content {
		if block.Type == "text" {
			responseText = strings.TrimSpace(block.Text)
			break
		}
	}
	if responseText == "" {
		return fmt.Errorf("empty LLM response")
	}

	var result scraped
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		return fmt.Errorf("parse LLM response %q: %w", responseText, err)
	}
	if result.Uncertain {
		return fmt.Errorf("LLM could not determine current data from page content")
	}

	applyChanges(v, result)
	return nil
}

func applyChanges(v *models.Venue, s scraped) {
	if len(s.FreeMonths) > 0 {
		log.Printf("  free_months: %v -> %v", v.FreeMonths, s.FreeMonths)
		v.FreeMonths = s.FreeMonths
	}
	if s.FreeSchedule != "" {
		log.Printf("  free_schedule: %s -> %s", v.FreeSchedule, s.FreeSchedule)
		v.FreeSchedule = s.FreeSchedule
	}
	if s.AdultsIncluded != nil {
		log.Printf("  adults_included: %d -> %d", v.AdultsIncluded, *s.AdultsIncluded)
		v.AdultsIncluded = *s.AdultsIncluded
	}
	if s.Notes != "" {
		v.Notes = s.Notes
	}
	if s.TemporarilyClosed != nil {
		log.Printf("  temporarily_closed: %v -> %v", v.TemporarilyClosed, *s.TemporarilyClosed)
		v.TemporarilyClosed = *s.TemporarilyClosed
		if !*s.TemporarilyClosed {
			v.ClosureReason = ""
		}
	}
	if s.ClosureReason != "" {
		v.ClosureReason = s.ClosureReason
	}
}
