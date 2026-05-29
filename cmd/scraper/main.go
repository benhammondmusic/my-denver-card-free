package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
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

// scrapedSessions holds the parsed pool schedule from a Canva page.
type scrapedSessions struct {
	Sessions  []models.PoolSession `json:"sessions"`
	Uncertain bool                 `json:"uncertain,omitempty"`
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

	// Pass 1: scrape regular venue metadata (skip pool venues - their URL is generic).
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

	// Pass 2: scrape pool session schedules from Canva docs.
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
			if err := scrapePoolSchedule(browser, apiKey, pool); err != nil {
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

// scrapePoolSchedule loads pool.CanvaURL, extracts the rendered table text,
// and uses Claude to parse it into PoolSession entries.
func scrapePoolSchedule(browser *rod.Browser, apiKey string, pool *models.Pool) error {
	if apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY required for pool schedule scraping")
	}

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

	// Canva needs extra time to hydrate the schedule tables.
	time.Sleep(canvaRenderWait)

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
	if strings.TrimSpace(pageText) == "" {
		return fmt.Errorf("canva page rendered empty text")
	}

	return parsePoolScheduleWithLLM(ctx, apiKey, pool, pageText)
}

func parsePoolScheduleWithLLM(ctx context.Context, apiKey string, pool *models.Pool, pageText string) error {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	prompt := fmt.Sprintf(`You are parsing a Denver public swimming pool schedule from the text of a Canva schedule document.

Pool: %s

Page text:
%s

Extract every swim session block. Return a JSON object:
{"sessions": [...]}

Each session object must have:
- "type": one of "open_swim", "lap_swim", "family_swim", "adult_swim", "quiet_swim", "aqua_fitness", "swim_lessons", "swim_team"
- "days": array of lowercase 3-letter abbreviations: "mon", "tue", "wed", "thu", "fri", "sat", "sun"
- "open": 24-hour time string "HH:MM"
- "close": 24-hour time string "HH:MM"

Rules:
- Consolidate rows with identical type and times across multiple days into one entry.
- Convert 12-hour times (e.g. "2:00 PM") to 24-hour format.
- If the schedule is not present or unreadable, return {"uncertain": true}.
- Return only valid JSON, no other text.`,
		pool.Name, pageText)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(llmModel),
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return fmt.Errorf("LLM request: %w", err)
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

	var result scrapedSessions
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		return fmt.Errorf("parse LLM response %q: %w", responseText, err)
	}
	if result.Uncertain {
		return fmt.Errorf("LLM could not parse schedule from page text")
	}

	pool.Sessions = result.Sessions
	return nil
}

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
