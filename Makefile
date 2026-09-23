.PHONY: dev build test lint migrate-up migrate-down migrate-down-all migrate-version bootstrap scheduler-once frontend frontend-test e2e

dev:
	docker compose up -d

build:
	go build ./...

test:
	go test ./...

lint:
	test -z "$$(gofmt -l .)"
	go vet ./...

migrate-up:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down

migrate-down-all:
	go run ./cmd/migrate down-all

migrate-version:
	go run ./cmd/migrate version

bootstrap:
	go run ./cmd/bootstrap

scheduler-once:
	go run ./cmd/scheduler --once

frontend:
	cd web && npm run build

frontend-test:
	cd web && npm run test

e2e:
	cd web && npm run e2e
