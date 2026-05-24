# My Denver Card Free

**[mydenvercardfree.benhammondmusic.tech](https://benhammondmusic.github.io/my-denver-card-free/)**

A fast, parent-friendly reference for Denver families: which cultural venues are free right now with a myDenverCard, which ones are coming up next month, and what the card actually covers (adults included, schedule restrictions, seasonal windows).

## Features

- Hero section showing venues free today as clickable chips
- "Free next month" preview so you can plan ahead
- Filter by Free Now, Summer, or Year-Round
- Month strip to browse any month
- Data covers both myDenverCard-specific benefits and venues free for all Denver kids regardless of card
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

## Tech stack

- Go 1.25 static site generator + local dev server
- [templ](https://templ.guide) type-safe HTML templates
- Vanilla JS client-side filtering (no build step)
- Deployed to GitHub Pages from `docs/`

## Contributing

Venue data lives in `data/venues.json`. If something is wrong or missing, [open an issue](https://github.com/benhammondmusic/my-denver-card-free/issues).
