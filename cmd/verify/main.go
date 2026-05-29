// One-time schedule verification tool.
// Fetches each indoor pool's Canva embed, takes a screenshot, and saves it
// alongside the current JSON sessions so they can be compared visually.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/benhammondmusic/my-denver-card-free/internal/models"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

const (
	poolsPageURL    = "https://www.denvergov.org/Government/Agencies-Departments-Offices/Agencies-Departments-Offices-Directory/Parks-Recreation/Recreation-Centers-Pools/Swimming-Pools"
	renderWait      = 4 * time.Second
	betweenWait     = 2 * time.Second
	screenshotDir   = "screenshots"
)

var (
	reCanvaIframe = regexp.MustCompile(`<iframe[^>]+src="(https://www\.canva\.com/design/[^"]+)"`)
	reDocID       = regexp.MustCompile(`/design/([A-Za-z0-9_-]+)/`)
)

func main() {
	raw, err := os.ReadFile("data/venues.json")
	if err != nil {
		log.Fatalf("read venues.json: %v", err)
	}
	var venues []models.Venue
	if err := json.Unmarshal(raw, &venues); err != nil {
		log.Fatalf("unmarshal: %v", err)
	}

	// Build map: docID -> (venue name, pool, current sessions)
	type poolEntry struct {
		venueName string
		pool      *models.Pool
	}
	poolMap := make(map[string]poolEntry)
	for i := range venues {
		v := &venues[i]
		for j := range v.Pools {
			p := &v.Pools[j]
			if p.CanvaURL == "" {
				continue
			}
			m := reDocID.FindStringSubmatch(p.CanvaURL)
			if m == nil {
				continue
			}
			poolMap[m[1]] = poolEntry{v.Name, p}
		}
	}

	// Fetch Denver.gov to get embed URLs with auth tokens.
	embedURLs, err := fetchEmbedURLs(poolsPageURL)
	if err != nil {
		log.Fatalf("fetch embed URLs: %v", err)
	}
	log.Printf("found %d Canva embed URLs", len(embedURLs))

	if err := os.MkdirAll(screenshotDir, 0755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	l := launcher.New().Headless(true).MustLaunch()
	browser := rod.New().ControlURL(l).MustConnect()
	defer browser.MustClose()

	first := true
	for docID, embedURL := range embedURLs {
		entry, ok := poolMap[docID]
		if !ok {
			continue
		}
		if !first {
			time.Sleep(betweenWait)
		}
		first = false

		slug := strings.ReplaceAll(strings.ToLower(entry.venueName), " ", "-")
		slug = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(slug, "")
		screenshotPath := filepath.Join(screenshotDir, slug+".png")
		sessionsPath := filepath.Join(screenshotDir, slug+".json")

		log.Printf("screenshotting %s ...", entry.venueName)
		if err := screenshot(browser, embedURL, screenshotPath); err != nil {
			log.Printf("  FAIL: %v", err)
			continue
		}

		// Write current JSON sessions alongside screenshot for comparison.
		sb, _ := json.MarshalIndent(entry.pool.Sessions, "", "  ")
		os.WriteFile(sessionsPath, sb, 0644)

		log.Printf("  saved %s", screenshotPath)
	}
	log.Println("done")
}

func screenshot(browser *rod.Browser, embedURL, path string) error {
	page, err := browser.Page(proto.TargetCreateTarget{URL: embedURL})
	if err != nil {
		return fmt.Errorf("open page: %w", err)
	}
	defer page.MustClose()

	if err := page.WaitLoad(); err != nil {
		return fmt.Errorf("wait load: %w", err)
	}
	time.Sleep(renderWait)

	img, err := page.Screenshot(false, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
	if err != nil {
		return fmt.Errorf("screenshot: %w", err)
	}
	return os.WriteFile(path, img, 0644)
}

func fetchEmbedURLs(pageURL string) (map[string]string, error) {
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; my-denver-card-verifier)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	matches := reCanvaIframe.FindAllStringSubmatch(string(body), -1)
	out := make(map[string]string)
	for _, m := range matches {
		src := m[1]
		dm := reDocID.FindStringSubmatch(src)
		if dm != nil {
			out[dm[1]] = src
		}
	}
	return out, nil
}
