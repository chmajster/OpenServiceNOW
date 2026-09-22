.PHONY: dev build test lint migrate-up migrate-down

dev:
	docker compose up -d

build:
	go build ./...

test:
	go test ./...

lint:
	gofmt -w $$(find . -name '*.go' -type f) && go vet ./...

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down
