.PHONY: dev build test lint migrate-up migrate-down migrate-down-all migrate-version bootstrap scheduler scheduler-once worker worker-once frontend frontend-test e2e local-install local-start local-stop local-restart local-status local-test

dev:
	docker compose -f docker-compose.yml up -d

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

scheduler:
	go run ./cmd/scheduler

worker:
	go run ./cmd/worker

worker-once:
	go run ./cmd/worker --once

frontend:
	cd web && npm run build

frontend-test:
	cd web && npm run test

e2e:
	cd web && npm run e2e

local-install:
	bash ./install.sh --local

local-start:
	bash ./install.sh --local --start

local-stop:
	bash ./install.sh --local --stop

local-restart:
	bash ./install.sh --local --restart

local-status:
	bash ./install.sh --local --status

local-test:
	bash tests/local_runtime_test.sh
