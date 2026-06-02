package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
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
	pageTimeout      = 30 * time.Second
	canvaTimeout     = 45 * time.Second
	canvaRenderWait  = 3 * time.Second
	canvaBetweenWait = 2 * time.Second
	llmModel         = "claude-haiku-4-5-20251001"
	maxPageChars     = 6000
	// Denver.gov swimming pools page - section-2 = indoor rec centers, section-3 = outdoor pools
	indoorPoolsURL  = "https://www.denvergov.org/Government/Agencies-Departments-Offices/Agencies-Departments-Offices-Directory/Parks-Recreation/Recreation-Centers-Pools/Swimming-Pools#section-2"
	outdoorPoolsURL = "https://www.denvergov.org/Government/Agencies-Departments-Offices/Agencies-Departments-Offices-Directory/Parks-Recreation/Recreation-Centers-Pools/Swimming-Pools#section-3"
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

// sessionType is scraper-internal only; not stored in the model.
type sessionType string

const (
	stFamilySwim  sessionType = "family_swim"
	stOpenSwim    sessionType = "open_swim"
	stLapSwim     sessionType = "lap_swim"
	stAdultSwim   sessionType = "adult_swim"
	stQuietSwim   sessionType = "quiet_swim"
	stAquaFitness sessionType = "aqua_fitness"
	stSwimLessons sessionType = "swim_lessons"
	stSwimTeam    sessionType = "swim_team"
)

func isFamilyFriendly(t sessionType) bool {
	return t == stOpenSwim || t == stFamilySwim
}

var (
	// Matches "2:00 PM", "10:30 AM", "14:00", and bare-hour forms like "11AM", "9PM".
	reTime = regexp.MustCompile(`(?i)\b(\d{1,2})(?::(\d{2}))?\s*(am|pm)?`)

	reCanvaDocID = regexp.MustCompile(`/design/([A-Za-z0-9_-]+)/`)

	// Season date detection: "season opens [Weekday,] Month Day[, Year]"
	reSeasonOpens = regexp.MustCompile(`(?i)season\s+opens?\s+(?:[a-z]+day,?\s+)?([a-z]+)\s+(\d{1,2})(?:,?\s*(20\d\d))?`)
	// "Month Day[ Year] through/to/thru Month Day[, Year]"
	reSeasonRange = regexp.MustCompile(`(?i)([a-z]+)\s+(\d{1,2})(?:,?\s*(20\d\d))?\s+(?:through|to|thru)\s+([a-z]+)\s+(\d{1,2})(?:,?\s*(20\d\d))?`)
	// Standalone 4-digit year
	reYear = regexp.MustCompile(`\b(20\d\d)\b`)

	// Session type keywords mapped to scraper-internal sessionType
	sessionKeywords = []struct {
		words []string
		typ   sessionType
	}{
		{[]string{"family"}, stFamilySwim},
		{[]string{"open", "public", "recreational"}, stOpenSwim},
		{[]string{"lap"}, stLapSwim},
		{[]string{"adult"}, stAdultSwim},
		{[]string{"quiet"}, stQuietSwim},
		{[]string{"aqua", "fitness", "aerobics", "ai chi", "water walking", "arthritis", "aquacise", "zumba", "yoga", "pilates"}, stAquaFitness},
		{[]string{"lesson", "learn"}, stSwimLessons},
		{[]string{"team", "club", "practice", "masters"}, stSwimTeam},
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

var monthNums = map[string]int{
	"january": 1, "jan": 1,
	"february": 2, "feb": 2,
	"march": 3, "mar": 3,
	"april": 4, "apr": 4,
	"may": 5,
	"june": 6, "jun": 6,
	"july": 7, "jul": 7,
	"august": 8, "aug": 8,
	"september": 9, "sep": 9, "sept": 9,
	"october": 10, "oct": 10,
	"november": 11, "nov": 11,
	"december": 12, "dec": 12,
}

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

	// Pass 1: scrape regular venue metadata (skip pool venues and the hub
	// Swimming-Pools pages, which are Canva-heavy and their notes are stable).
	for i := range venues {
		v := &venues[i]
		if v.Category == "pool" {
			continue
		}
		if strings.Contains(v.URL, "/Swimming-Pools") {
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

	// Build map: Canva doc ID -> pool pointer for fast lookup during iframe scrape.
	poolMap := make(map[string]*models.Pool)
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
			docID := extractCanvaDocID(pool.CanvaURL)
			if docID != "" {
				poolMap[docID] = pool
			}
		}
	}
	log.Printf("mapped %d pools with Canva URLs", len(poolMap))

	// Pass 2: fetch Denver.gov pool page once (both sections are on the same
	// page; the anchor fragments only affect scroll position, not HTTP response).
	log.Printf("Pass 2: scraping pool schedules via Canva embeds on Denver.gov")
	detectedStart, detectedEnd, poolErr := scrapePoolPage(browser, indoorPoolsURL, poolMap)
	if poolErr != nil {
		log.Printf("  FAIL: %v", poolErr)
	}

	// Apply detected outdoor season dates to non-indoor pool venues.
	// Detected dates override existing values so the scraper stays authoritative.
	if detectedStart != "" {
		for i := range venues {
			v := &venues[i]
			if v.Category != "pool" || v.Indoor {
				continue
			}
			for j := range v.Pools {
				p := &v.Pools[j]
				p.SeasonStart = detectedStart
				if detectedEnd != "" {
					p.SeasonEnd = detectedEnd
				}
			}
		}
		log.Printf("  applied season %s - %s to outdoor pools", detectedStart, detectedEnd)
	}

	// Update last_checked for all pool venues.
	for i := range venues {
		if venues[i].Category == "pool" {
			venues[i].LastChecked = time.Now().UTC()
		}
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

// fmtDate returns "YYYY-MM-DD" for the given year, month (1-12), and day.
func fmtDate(year, month, day int) string {
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

// detectSeasonDates scans raw page text (HTML is fine) for outdoor pool season
// open/close dates and returns them as "YYYY-MM-DD" strings. Either value may
// be empty if not found.
func detectSeasonDates(text string) (start, end string) {
	lower := strings.ToLower(text)

	year := time.Now().Year()
	if ym := reYear.FindString(lower); ym != "" {
		if y, err := strconv.Atoi(ym); err == nil && y >= year {
			year = y
		}
	}

	// "June 8 through August 16" (range pattern preferred)
	if m := reSeasonRange.FindStringSubmatch(lower); m != nil {
		m1 := monthNums[m[1]]
		d1, _ := strconv.Atoi(m[2])
		y1 := year
		if m[3] != "" {
			if y, err := strconv.Atoi(m[3]); err == nil {
				y1 = y
			}
		}
		m2 := monthNums[m[4]]
		d2, _ := strconv.Atoi(m[5])
		y2 := year
		if m[6] != "" {
			if y, err := strconv.Atoi(m[6]); err == nil {
				y2 = y
			}
		}
		if m1 > 0 && d1 > 0 && m2 > 0 && d2 > 0 {
			return fmtDate(y1, m1, d1), fmtDate(y2, m2, d2)
		}
	}

	// "season opens Monday, June 8" (start-only fallback)
	if m := reSeasonOpens.FindStringSubmatch(lower); m != nil {
		mon := monthNums[m[1]]
		day, _ := strconv.Atoi(m[2])
		y := year
		if m[3] != "" {
			if yy, err := strconv.Atoi(m[3]); err == nil {
				y = yy
			}
		}
		if mon > 0 && day > 0 {
			return fmtDate(y, mon, day), ""
		}
	}

	return "", ""
}

// extractCanvaDocID extracts the Canva document ID from a URL like
// https://www.canva.com/design/DAG989jxCeU/view or an iframe src.
func extractCanvaDocID(src string) string {
	m := reCanvaDocID.FindStringSubmatch(src)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// reCanvaIframeSrc finds Canva iframe src attributes in raw HTML.
var reCanvaIframeSrc = regexp.MustCompile(`<iframe[^>]+src="(https://www\.canva\.com/design/[^"]+)"`)

// fetchCanvaEmbedURLs fetches the Denver.gov pool page with a plain HTTP GET
// (avoids headless-browser detection) and returns a map of
// Canva doc ID -> full embed URL for every Canva iframe found, plus the raw
// page body text for downstream season-date detection.
func fetchCanvaEmbedURLs(pageURL string) (map[string]string, string, error) {
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; my-denver-card-scraper)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read body: %w", err)
	}

	matches := reCanvaIframeSrc.FindAllSubmatch(body, -1)
	result := make(map[string]string)
	for _, m := range matches {
		src := string(m[1])
		docID := extractCanvaDocID(src)
		if docID != "" {
			result[docID] = src
		}
	}
	return result, string(body), nil
}

// scrapePoolPage fetches the Denver.gov pool page with HTTP to extract Canva
// embed URLs (including the auth token needed for unauthenticated access), then
// loads each embed URL directly with Rod and extracts schedule tables.
// It also scans the page body for outdoor season open/close dates and returns
// them as "YYYY-MM-DD" strings (either may be empty if not found).
func scrapePoolPage(browser *rod.Browser, pageURL string, poolMap map[string]*models.Pool) (seasonStart, seasonEnd string, err error) {
	embedURLs, body, err := fetchCanvaEmbedURLs(pageURL)
	if err != nil {
		return "", "", fmt.Errorf("fetch embed URLs: %w", err)
	}
	if len(embedURLs) == 0 {
		return "", "", fmt.Errorf("no Canva iframes found in page HTML")
	}
	log.Printf("  found %d Canva embed URLs in page HTML", len(embedURLs))

	seasonStart, seasonEnd = detectSeasonDates(body)
	if seasonStart != "" {
		log.Printf("  detected season: %s to %s", seasonStart, seasonEnd)
	}

	first := true
	for docID, embedURL := range embedURLs {
		pool, ok := poolMap[docID]
		if !ok {
			log.Printf("  no pool mapped for doc ID %s", docID)
			continue
		}
		if !first {
			time.Sleep(canvaBetweenWait)
		}
		first = false
		log.Printf("  scraping %s", pool.Name)
		sessions, scrapeErr := scrapeCanvaEmbed(browser, embedURL)
		if scrapeErr != nil {
			log.Printf("  FAIL: %v", scrapeErr)
			continue
		}
		if pool.ManualSessions {
			log.Printf("  skipping sessions (manual_sessions=true)")
		} else {
			pool.Sessions = sessions
			log.Printf("  ok: %d sessions", len(sessions))
		}
	}
	return seasonStart, seasonEnd, nil
}

// scrapeCanvaEmbed loads a Canva embed URL with Rod and extracts pool sessions
// from the HTML tables rendered inside the page.
func scrapeCanvaEmbed(browser *rod.Browser, embedURL string) ([]models.PoolSession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), canvaTimeout)
	defer cancel()

	page, err := browser.Page(proto.TargetCreateTarget{URL: embedURL})
	if err != nil {
		return nil, fmt.Errorf("open page: %w", err)
	}
	defer page.MustClose()
	page = page.Context(ctx)

	if err := page.WaitLoad(); err != nil {
		return nil, fmt.Errorf("page load: %w", err)
	}
	time.Sleep(canvaRenderWait)

	tables, err := extractTables(page)
	if err != nil || len(tables) == 0 {
		return nil, fmt.Errorf("no tables found")
	}

	sessions, err := parseSessions(tables)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("no sessions parsed")
	}
	return sessions, nil
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
				txt = strings.ReplaceAll(txt, "\n", " ")
				txt = strings.ReplaceAll(txt, "\r", " ")
				txt = strings.TrimSpace(txt)
				// Expand colspan so each logical column gets the cell value.
				span := 1
				if attr, err := cell.Attribute("colspan"); err == nil && attr != nil {
					if n := atoi(*attr); n > 1 {
						span = n
					}
				}
				for i := 0; i < span; i++ {
					r = append(r, txt)
				}
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

// parseTable handles three common Denver pool schedule formats:
//
//	Format A (day-grid): header row has a session-type column (col 0) then day columns.
//	  Row 0: ["Session", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
//	  Row N: ["Open Swim", "2:00-5:00 PM", "2:00-5:00 PM", "", ...]
//
//	Format C (alt-row): ALL header columns are day names; rows alternate time/type.
//	  Row 0: ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
//	  Row 1 (times): ["6:00-8:45AM", "6:00-8:45AM", ...]
//	  Row 2 (types): ["Lap Swim", "Lap Swim", ...]
//	  Row 3 (times): ["2:00-6:15PM", "2:00-6:15PM", ...]
//	  Row 4 (types): ["Open Swim (No Lanes)", "Open Swim (No Lanes)", ...]
//
//	Format B (row-per-session): each row describes one session block.
//	  ["Open Swim", "Mon/Wed/Fri", "2:00 PM", "5:00 PM"]
func parseTable(tbl [][]string) ([]models.PoolSession, error) {
	if len(tbl) < 2 {
		return nil, fmt.Errorf("table has fewer than 2 rows")
	}
	header := tbl[0]

	dayColIndex := detectDayColumns(header)
	if len(dayColIndex) >= 2 {
		// Format C: col 0 of the header is a day name (no session-type column on left).
		if _, col0IsDay := dayColIndex[0]; col0IsDay {
			return parseAltRows(tbl, dayColIndex)
		}
		// Format D (outdoor pools): col 0 header is "TIME"; time ranges are in col 0,
		// session type names are in the day columns.
		if len(header) > 0 {
			h0 := strings.ToLower(strings.TrimSpace(header[0]))
			if h0 == "time" || h0 == "times" {
				return parseTimeGrid(tbl, dayColIndex)
			}
		}
		// Format A.
		return parseDayGrid(tbl, dayColIndex)
	}

	// Fall back to Format B.
	return parseRowPerSession(tbl)
}

// parseTimeGrid handles Format D tables (outdoor pools).
// Col 0 contains a time range; day columns contain session type names or "CLOSED".
//
//	Row 0: ["TIME", "Mon", "Tue", ...]
//	Row N: ["10:00-12:00PM", "Open Swim", "Open Swim", "", "CLOSED"]
//	Safety-break rows have non-time text in col 0 and are skipped automatically.
func parseTimeGrid(tbl [][]string, dayCol map[int]string) ([]models.PoolSession, error) {
	type key struct{ open, close string }
	grouped := make(map[key][]string)
	var keyOrder []key

	for _, row := range tbl[1:] {
		if len(row) == 0 {
			continue
		}
		open, close, err := parseTimeRange(row[0])
		if err != nil {
			continue // safety-break rows, headings, etc.
		}
		for colIdx, day := range dayCol {
			if colIdx >= len(row) {
				continue
			}
			cell := strings.TrimSpace(row[colIdx])
			if cell == "" {
				continue
			}
			st := detectSessionType(cell)
			if !isFamilyFriendly(st) {
				continue
			}
			k := key{open, close}
			if _, exists := grouped[k]; !exists {
				keyOrder = append(keyOrder, k)
			}
			grouped[k] = append(grouped[k], day)
		}
	}

	if len(grouped) == 0 {
		return nil, fmt.Errorf("time-grid: no sessions extracted")
	}

	var sessions []models.PoolSession
	for _, k := range keyOrder {
		sessions = append(sessions, models.PoolSession{
			FamilyFriendly: true,
			Days:           sortDays(grouped[k]),
			Open:           k.open,
			Close:          k.close,
		})
	}
	return sessions, nil
}

// parseAltRows handles Format C tables where rows alternate: time row, type row, time row, type row...
// The type row may have fewer cells than the time row due to HTML colspan merging cells.
func parseAltRows(tbl [][]string, dayCol map[int]string) ([]models.PoolSession, error) {
	type key struct{ open, close string }
	grouped := make(map[key][]string)
	var keyOrder []key

	rows := tbl[1:] // skip header
	for i := 0; i+1 < len(rows); i += 2 {
		timeRow := rows[i]
		typeRow := rows[i+1]

		for colIdx, day := range dayCol {
			if colIdx >= len(timeRow) {
				continue
			}
			cell := strings.TrimSpace(timeRow[colIdx])
			if cell == "" || cell == "-" {
				continue
			}
			open, close, err := parseTimeRange(cell)
			if err != nil {
				continue
			}

			var st sessionType
			if colIdx < len(typeRow) {
				st = detectSessionType(typeRow[colIdx])
			} else if len(typeRow) > 0 {
				st = detectSessionType(typeRow[len(typeRow)-1])
			}
			if !isFamilyFriendly(st) {
				continue
			}

			k := key{open, close}
			if _, exists := grouped[k]; !exists {
				keyOrder = append(keyOrder, k)
			}
			grouped[k] = append(grouped[k], day)
		}
	}

	if len(grouped) == 0 {
		return nil, fmt.Errorf("alt-row: no sessions extracted")
	}

	var sessions []models.PoolSession
	for _, k := range keyOrder {
		sessions = append(sessions, models.PoolSession{
			FamilyFriendly: true,
			Days:           sortDays(grouped[k]),
			Open:           k.open,
			Close:          k.close,
		})
	}
	return sessions, nil
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
	// group: open+close -> []day (only family-friendly sessions are kept)
	type key struct{ open, close string }
	grouped := make(map[key][]string)
	var keyOrder []key

	for _, row := range tbl[1:] {
		if len(row) == 0 {
			continue
		}
		st := detectSessionType(row[0])
		if !isFamilyFriendly(st) {
			continue
		}
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
			k := key{open, close}
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
			FamilyFriendly: true,
			Days:           days,
			Open:           k.open,
			Close:          k.close,
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
		st := detectSessionType(row[0])
		if !isFamilyFriendly(st) {
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
			FamilyFriendly: true,
			Days:           days,
			Open:           open,
			Close:          close,
		})
	}
	if len(sessions) == 0 {
		return nil, fmt.Errorf("row-per-session: no sessions extracted")
	}
	return sessions, nil
}

// detectSessionType returns the scraper-internal sessionType for a label string,
// or "" if no keyword matches (unknown types are skipped).
func detectSessionType(s string) sessionType {
	lower := strings.ToLower(s)
	for _, kw := range sessionKeywords {
		for _, word := range kw.words {
			if strings.Contains(lower, word) {
				return kw.typ
			}
		}
	}
	return ""
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

	// If only one AM/PM suffix was given (e.g. "2:00-5:00 PM"), try to apply it to open too.
	// Only apply PM when the resulting open time stays before close; otherwise the suffix
	// belongs to close only (e.g. "11:30-3:30PM" → 11:30+PM=23:30 > 15:30, so open stays 11:30).
	if !strings.Contains(strings.ToLower(m[1]), "am") && !strings.Contains(strings.ToLower(m[1]), "pm") {
		suffix := extractAmPm(m[2])
		if suffix == "pm" {
			if candidate := parseTimeStr(m[1] + " pm"); candidate != "" && candidate < close {
				open = candidate
			}
		} else if suffix == "am" {
			if candidate := parseTimeStr(m[1] + " am"); candidate != "" {
				open = candidate
			}
		}
	}

	// Sanity check: open must precede close.
	if open >= close {
		return "", "", fmt.Errorf("open %s not before close %s in %q", open, close, s)
	}
	return open, close, nil
}

// parseTimeStr converts "2:00 PM", "14:00", or bare "11AM" to 24-hour "HH:MM".
func parseTimeStr(s string) string {
	s = strings.TrimSpace(s)
	m := reTime.FindStringSubmatch(s)
	if len(m) < 2 || m[1] == "" {
		return ""
	}
	h := atoi(m[1])
	min := 0
	if len(m) > 2 && m[2] != "" {
		min = atoi(m[2])
	}
	ampm := ""
	if len(m) > 3 {
		ampm = strings.ToLower(strings.TrimSpace(m[3]))
	}
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
