.PHONY: dev build test lint migrate-up migrate-down bootstrap scheduler-once

dev:
	docker compose up -d

build:
	go build ./...

test:
	go test ./...

lint:
	test -z "$(gofmt -l .)"
	go vet ./...

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

bootstrap:
	go run ./cmd/bootstrap

scheduler-once:
	go run ./cmd/scheduler --once
