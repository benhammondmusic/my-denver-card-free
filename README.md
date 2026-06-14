# My Denver Card Free

**[Live site](https://benhammondmusic.github.io/my-denver-card-free/)**

A fast, parent-friendly reference for Denver families: which cultural venues, pools, and rec centers are free right now with a myDenverCard, which are coming up next month, and what the card actually covers (adults included, schedule restrictions, seasonal windows).

## Features

- Hero section showing venues free today as clickable chips; group chips for Pools and Rec Centers
- "Coming up next month" preview for planning ahead
- Filter tabs: All / Free Now / This Weekend / Summer / Year-Round / Pools / Playground / EV Charger
- Real-time pool status badges: Open now / Opens at HH:MM / No open swim today / Opens Jun 8 (pre-season)
- Outdoor pool season gating: pools show as pre-season (blue badge) before their season_start date
- Venue modal with pool schedule, amenities, map, address, and phone
- Map panel showing venue locations (syncs with filter)
- Amenity chips on venue rows and filter tabs for Playground and Free EV charging
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
- **[rod](https://go-rod.github.io):** headless Chromium for the scraper; used to load Canva embeds and extract pool schedule tables
- **Vanilla JS:** client-side filtering, pool status calculation, modal, map, hero chips
- **[Leaflet](https://leafletjs.com):** interactive map via OpenStreetMap tiles
- **Fonts:** Bebas Neue (display) + Outfit (body) via Google Fonts

## Key directories

| Path | Purpose |
|------|---------|
| `cmd/server/` | Local dev HTTP server (not deployed) |
| `cmd/scraper/` | Scraper: rod headless browser + Claude Haiku LLM fallback for venue metadata; Canva embed table parsing for pool schedules |
| `cmd/generate/` | Renders templ to `docs/index.html` |
| `internal/models/` | Shared `Venue` and `Pool` structs |
| `templates/` | templ source files |
| `data/venues.json` | Venue data: source of truth, updated by scraper |
| `docs/` | Generated static site (committed by CI, served by GitHub Pages) |
| `.github/workflows/` | Weekly scrape + static gen + issue-on-failure CI |

## Venue data model

Key fields in `data/venues.json` and `internal/models/venue.go`:

| Field | Type | Notes |
|-------|------|-------|
| `name` | string | Display name |
| `program` | `"mydenvercard"` or `"general"` | Card-specific benefit vs. free for all Denver kids |
| `featured` | bool | Shows in the premier tier (larger card, appears first) |
| `category` | `""`, `"pool"`, `"rec_center"`, `"museum"` | Controls list grouping and filter behavior |
| `free_months` | string[] | Month names when free admission applies |
| `free_schedule` | `"daily"`, `"weekends"`, `"weekends_and_breaks"` | Day restriction within free months |
| `adults_included` | int | 0 = kids only, 1 = one adult, 2 = two adults |
| `temporarily_closed` | bool | Venue is temporarily closed |
| `indoor` | bool | Outdoor pools have season gating; indoor venues are year-round |
| `reservation_required` | bool | Shown as a badge in the modal |
| `activities` | string[] | Amenities shown as tags in the modal (rec centers) |
| `playground` | bool | Venue has an adjacent playground; enables Playground filter |
| `ev_charger` | bool | Venue has free EV charging; enables EV Charger filter |
| `pools` | Pool[] | Pool schedule data (see below) |
| `lat`, `lng` | float64 | Used for the map marker |
| `address`, `phone` | string | Shown in modal |

### Pool struct

Each `Pool` within `pools[]` has:

| Field | Type | Notes |
|-------|------|-------|
| `name` | string | Pool name (e.g., "Leisure Pool") |
| `family_friendly` | bool | Whether this pool allows family/open swim |
| `features` | string[] | e.g., "water slide", "lazy river" |
| `notes` | string | Schedule caveats shown in modal (e.g., mid-day break, early closure days) |
| `manual_sessions` | bool | If true, scraper skips overwriting `sessions` — use when hand-corrected data is more accurate than the Canva scrape |
| `season_start` | `"YYYY-MM-DD"` | Outdoor pools only; JS uses this to gate free-now status |
| `season_end` | `"YYYY-MM-DD"` | Outdoor pools only |
| `canva_url` | string | Source Canva embed used by scraper to extract sessions |
| `sessions` | PoolSession[] | Each session: days[], open/close times, family_friendly |

The scraper detects season dates from the Denver.gov pools page body text and writes them automatically.

### Protecting hand-corrected pool data

The weekly scraper overwrites `sessions` for any pool with a `canva_url`. If you manually correct session times (e.g., because the scraper picked up overlapping swim types), set `"manual_sessions": true` on that pool entry. The scraper will log a skip and leave sessions untouched while still updating `season_start`/`season_end`.

Fields the scraper **never** touches: `notes`, `features`, `manual_sessions`.
Fields the scraper **always** overwrites: `season_start`, `season_end` (from Denver.gov page), `sessions` (from Canva embed, unless `manual_sessions: true`).

## UI layout

1. **Header:** full-bleed Bear Blue gradient, Big Blue Bear PNG, large gold "Free." headline
2. **Hero section:** JS-populated chips for venues free right now (excluding pools/rec centers); group chip for Pools (links to Pools tab) and Rec Centers (links to rec section). "Coming up next month" chips below.
3. **Filter bar:** tabs (All / Free Now / This Weekend / Summer / Year-Round / Pools / Playground / EV Charger)
4. **Map panel:** Leaflet map shown below filter bar on the Pools tab (and when scrolling to a venue)
5. **Venue list:** sub-headings for Art & History, Pools and Rec Centers, and More (shown on All tab). Featured venues first (gold top border). Left band color: blue = card, gold = general, gray = closed. Amenity chips (Playground, Free EV) shown on venue rows.
6. **Pool status badge:** set by JS on page load using session times and season dates. States: Open now (green), Opens at HH:MM (yellow), No open swim today (gray), Opens Jun 8 (blue, pre-season), Season ended (gray).
7. **Venue modal:** bottom sheet with pool schedule, amenities, address/phone, map, and action buttons.
8. **Footer:** update cadence note + issue report link

Color palette: Bear Blue `#002868`, Gold `#d97706`, Sky Blue `#1a56db`, Red `#bf0a30`.

School break approximation used by "Free Now" JS filter (update annually for DPS calendar):
- Winter: Dec 20 - Jan 5
- Spring: Mar 15 - Mar 29
- Summer: Jun 1 - Aug 31

## Deployment

GitHub Pages serves `docs/` from `main`. The weekly CI workflow runs the scraper, regenerates `docs/index.html`, and commits automatically.

## Contributing

Venue data lives in `data/venues.json`. If something is wrong or missing, [open an issue](https://github.com/benhammondmusic/my-denver-card-free/issues).
