GO ?= go

.PHONY: dev-infra dev build test race vet fmt-check migrate-up migrate-down migrate-status

dev-infra:
	docker compose up -d mysql redis

dev:
	$(GO) run ./cmd/api

build:
	mkdir -p bin
	$(GO) build -o bin/openservicenow-api ./cmd/api
	$(GO) build -o bin/openservicenow-migrate ./cmd/migrate

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

fmt-check:
	@test -z "$$($(GO)fmt -l .)" || (echo "gofmt required"; $(GO)fmt -l .; exit 1)

migrate-up:
	$(GO) run ./cmd/migrate up

migrate-down:
	$(GO) run ./cmd/migrate down

migrate-status:
	$(GO) run ./cmd/migrate status
