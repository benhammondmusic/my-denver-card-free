TEMPL  := $(shell go env GOPATH)/bin/templ
AIR    := $(shell go env GOPATH)/bin/air

.PHONY: generate build run dev scrape generate-site

generate:
	$(TEMPL) generate

build: generate
	go build -o bin/server ./cmd/server/main.go

run: generate
	go run ./cmd/server/main.go

# Hot-reload dev server: templ proxy on :7331 -> Go server on :8080
# Edit any .templ or .go file and the browser refreshes automatically.
# Open http://localhost:7331
dev:
	@echo "Dev server starting at http://localhost:7331"
	@trap 'kill 0' INT TERM; \
	$(TEMPL) generate --watch --proxy="http://localhost:8080" --open-browser=false 2>&1 | sed 's/^/[templ] /' & \
	$(AIR); \
	wait

scrape: generate
	go run ./cmd/scraper/main.go

generate-site: generate
	go run ./cmd/generate/main.go
	@echo "→ docs/index.html"
