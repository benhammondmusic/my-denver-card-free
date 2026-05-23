.PHONY: generate build run scrape generate-site

generate:
	templ generate

build: generate
	go build -o bin/server ./cmd/server/main.go

run: generate
	go run ./cmd/server/main.go

scrape: generate
	go run ./cmd/scraper/main.go

generate-site: generate
	go run ./cmd/generate/main.go
