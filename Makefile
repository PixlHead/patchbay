export GOCACHE := $(CURDIR)/.cache/go-build
export npm_config_cache := $(CURDIR)/.cache/npm
export PLAYWRIGHT_BROWSERS_PATH ?= $(CURDIR)/.cache/playwright

.PHONY: install build test e2e fmt api web up down

install:
	npm --prefix web ci

build:
	npm --prefix web run build
	mkdir -p bin
	go build -o bin/patchbay ./cmd/server

test:
	go test -race ./...
	npm --prefix web run typecheck

e2e: build
	npm --prefix web run test:e2e

fmt:
	gofmt -w cmd internal
	npm --prefix web run format

api:
	go run ./cmd/server

web:
	npm --prefix web run dev

up:
	docker compose up --build --wait
	docker ps

down:
	docker compose down --remove-orphans
	docker ps
