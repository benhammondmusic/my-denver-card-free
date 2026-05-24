# My Denver Card Free

**[Live site](https://benhammondmusic.github.io/my-denver-card-free/)**

A fast, parent-friendly reference for Denver families: which cultural venues are free right now with a myDenverCard, which are coming up next month, and what the card actually covers (adults included, schedule restrictions, seasonal windows).

## Features

- Hero section showing venues free today as clickable chips
- "Free next month" preview so you can plan ahead
- Filter by Free Now, Summer, or Year-Round
- Month strip to browse any month
- Covers both myDenverCard-specific benefits and venues free for all Denver kids regardless of card
- Auto-updated weekly via GitHub Actions

## Running locally

```bash
brew install go
go install github.com/a-h/templ/cmd/templ@latest
go mod tidy
make generate && make run
# visit http://localhost:8080
```

Regenerate the static site:

```bash
make generate-site
```

## Stack

- **Go 1.25:** static site generator + local dev server
- **[templ](https://templ.guide):** type-safe HTML templates (run `make generate` after editing any `*.templ` file)
- **[htmx](https://htmx.org):** loaded from CDN, available for future interactions
- **[rod](https://go-rod.github.io):** headless browser for the scraper (stub, TODO)
- **Vanilla JS:** client-side filtering embedded in the generated HTML
- **Fonts:** Bebas Neue (display) + Outfit (body) via Google Fonts

## Key directories

| Path | Purpose |
|------|---------|
| `cmd/server/` | Local dev HTTP server (not deployed) |
| `cmd/scraper/` | Scraper entrypoint (rod logic TODO) |
| `cmd/generate/` | Renders templ to `docs/index.html` |
| `internal/models/` | Shared `Venue` struct |
| `templates/` | templ source files |
| `data/venues.json` | Venue data: source of truth, updated by scraper |
| `docs/` | Generated static site (committed by CI, served by GitHub Pages) |
| `.github/workflows/` | Weekly scrape + static gen + issue-on-failure CI |

## Venue data model

Key fields in `data/venues.json` and `internal/models/venue.go`:

| Field | Type | Notes |
|-------|------|-------|
| `program` | `"mydenvercard"` or `"general"` | Card-specific benefit vs. free for all Denver kids |
| `featured` | bool | Shows in the premium tier (larger card, appears first) |
| `free_months` | string array | Month names when free admission applies |
| `free_schedule` | `"daily"`, `"weekends"`, `"weekends_and_breaks"` | Day restriction within free months |
| `adults_included` | int | 0 = kids only, 1 = one adult, 2 = two adults |
| `temporarily_closed` | bool | Venue is temporarily closed |

School break approximation used by the "Free Now" JS filter (update annually for DPS calendar):
- Winter: Dec 20 - Jan 5
- Spring: Mar 15 - Mar 29
- Summer: Jun 1 - Aug 31

## UI layout

1. **Header:** full-bleed Bear Blue gradient, Big Blue Bear PNG, large gold "Free." headline
2. **Hero section:** JS-populated chips for venues free right now; "Free next month" chips below. Clicking scrolls to and highlights that venue row.
3. **Filter bar:** tabs (All / Free Now / Summer / Year-Round) + scrollable month strip
4. **Venue list:** featured venues first (gold top border), then non-featured, then closed. Left band color: blue = card, gold = general, gray = closed.
5. **Toast:** fixed bottom-right, dismissible signup link for families without a card yet
6. **Footer:** update cadence note + issue report link

Color palette: Bear Blue `#002868`, Gold `#d97706`, Sky Blue `#1a56db`, Red `#bf0a30`.

## Deployment

GitHub Pages serves `docs/` from `main`. The weekly CI workflow runs the scraper, regenerates `docs/index.html`, and commits automatically.

## Contributing

Venue data lives in `data/venues.json`. If something is wrong or missing, [open an issue](https://github.com/benhammondmusic/my-denver-card-free/issues).
