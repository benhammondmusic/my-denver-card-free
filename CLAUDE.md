# my-denver-card-free

**Repo location:** ~/code/my-denver-card-free

## Project purpose

Aggregates free-entry information for myDenverCard holders across Denver cultural venues (Denver Art Museum, Denver Museum of Nature & Science, Denver Zoo, etc.). Displays which months each venue offers free or reduced admission, whether adults are included, and any caveats.

## Stack

- **Go 1.22** — server, scraper, and static site generator
- **[templ](https://templ.guide)** — type-safe HTML templating (generates `*_templ.go` files from `*.templ` sources)
- **[htmx](https://htmx.org)** — hypermedia-driven frontend interactions (loaded from CDN)
- **[rod](https://go-rod.github.io)** — headless browser automation for the scraper

## How to run

```bash
brew install go
go install github.com/a-h/templ/cmd/templ@latest
go mod tidy          # generates go.sum on first checkout
make generate && make run
# visit http://localhost:8080
```

Run the scraper stub:
```bash
make scrape
```

Generate the static site to `public/index.html`:
```bash
make generate-site
```

## Data flow

```
GitHub Actions (weekly cron)
  → cmd/scraper (rod headless browser)
  → data/venues.json
  → cmd/generate (static site generator)
  → public/index.html
       and
  → cmd/server (http.ServeMux)
  → internal/handlers (reads JSON, passes to templ)
  → templates/ (layout + index templ components)
  → htmx frontend (month filter buttons)
```

## Key directories

| Path | Purpose |
|------|---------|
| `cmd/server/` | HTTP server entrypoint |
| `cmd/scraper/` | Scraper entrypoint (rod logic TODO) |
| `cmd/generate/` | Static site generator — renders templ to `public/index.html` |
| `internal/models/` | Shared `Venue` struct |
| `internal/handlers/` | HTTP handlers |
| `templates/` | templ source files (run `make generate` after editing) |
| `data/venues.json` | Venue data — source of truth, updated by scraper |
| `public/` | Generated static site output (committed by CI) |
| `.github/workflows/` | Weekly scrape + static gen + issue-on-failure CI |

## Templ workflow

Always run `make generate` (or `templ generate`) after editing any `*.templ` file. The generated `*_templ.go` files are committed to the repo so the server builds without needing templ installed at deploy time.

## Deployment

Deployed on [Fly.io](https://fly.io) (`fly.toml`). Primary region: `den` (Denver). The Docker multi-stage build compiles the binary and copies it into a distroless image.
