# my-denver-card-free

**Repo location:** ~/code/my-denver-card-free

## Project purpose

Aggregates free-entry information for myDenverCard holders across Denver cultural venues (Denver Art Museum, Denver Museum of Nature & Science, Denver Zoo, etc.). Displays which months each venue offers free admission, whether adults are included, schedule restrictions, and notes.

## Stack

- **Go 1.25:** static site generator and local dev server
- **[templ](https://templ.guide):** type-safe HTML templating (generates `*_templ.go` files from `*.templ` sources)
- **[htmx](https://htmx.org):** loaded from CDN, available for future interactions
- **[rod](https://go-rod.github.io):** headless browser automation for the scraper (stub, TODO)
- **Vanilla JS:** client-side filtering (month strip, filter tabs, hero chips) embedded in the generated HTML
- **Fonts:** Bebas Neue (display/labels/tabs) + Outfit (body), loaded from Google Fonts

## How to run locally

```bash
brew install go
go install github.com/a-h/templ/cmd/templ@latest
go mod tidy
make generate && make run
# visit http://localhost:8080
```

Run the scraper stub:

```bash
make scrape
```

Generate the static site to `docs/index.html`:

```bash
make generate-site
```

## Data flow

```plaintext
GitHub Actions (weekly cron)
  -> cmd/scraper (rod headless browser -- TODO)
  -> data/venues.json
  -> cmd/generate (static site generator)
  -> docs/index.html  (committed to repo, served by GitHub Pages)
```

## Key directories

| Path | Purpose |
|------|---------|
| `cmd/server/` | Local dev HTTP server (not deployed) |
| `cmd/scraper/` | Scraper entrypoint (rod logic TODO) |
| `cmd/generate/` | Static site generator: renders templ to `docs/index.html` |
| `internal/models/` | Shared `Venue` struct |
| `templates/` | templ source files (run `make generate` after editing) |
| `data/venues.json` | Venue data: source of truth, updated by scraper |
| `docs/` | Generated static site output (committed by CI, served by GitHub Pages) |
| `.github/workflows/` | Weekly scrape + static gen + issue-on-failure CI |

## Venue data model

Key fields in `data/venues.json` and `internal/models/venue.go`:

| Field | Type | Notes |
|-------|------|-------|
| `program` | `"mydenvercard"` or `"general"` | Whether benefit is card-specific or free for all kids |
| `featured` | bool | Shows venue in the premium tier (larger card, appears first) |
| `free_months` | string array | Month names when free admission applies |
| `free_schedule` | `"daily"`, `"weekends"`, `"weekends_and_breaks"` | Day/period restriction within free months |
| `adults_included` | int | 0 = kids only, 1 = one adult, 2 = two adults |
| `temporarily_closed` | bool | Venue is temporarily closed |

School break approximation (used by JS "Free Now" filter, update annually):
- Winter: Dec 20 - Jan 5
- Spring: Mar 15 - Mar 29
- Summer: Jun 1 - Aug 31

## UI layout

The page is a single-page static site with no routing:

1. **Header:** full-bleed Bear Blue gradient with the Big Blue Bear PNG, large italic gold "Free." headline, and a subtitle. Background has subtle SVG mountain silhouette + city skyline shapes.
2. **Hero section:** JS-populated chips showing venues free right now (blue = card, gold = general). Below a divider, a "Free next month" row of gray chips. Clicking a chip scrolls to and highlights the venue row.
3. **Filter bar:** tabs (All / Free Now / Summer / Year-Round) + a scrollable month strip. Clicking a month filters to venues free that month.
4. **Venue list:** featured venues first (gold top border, wider band), then non-featured, then closed. Each card has a colored left band (blue = card, gold = general, gray = closed), venue name, notes, and a status column (adults included badge + months label).
5. **Toast:** fixed bottom-right, dismissible. "No card yet? Get your card" link to the myDenverCard signup page.
6. **Footer:** update cadence note, issue report link, personal site credit.

Color palette: Bear Blue `#002868`, Gold `#d97706`, Sky Blue `#1a56db`, Red `#bf0a30`, off-white `#edf1fb`. No em dashes anywhere in user-visible text.

## Templ workflow

Always run `make generate` (or `templ generate`) after editing any `*.templ` file. The generated `*_templ.go` files are committed to the repo so the server builds without needing templ installed at deploy time.

## Deployment

Static site served by **GitHub Pages** from the `docs/` folder on `main`. The weekly GitHub Actions workflow regenerates `docs/index.html` and commits it automatically.
