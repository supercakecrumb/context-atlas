.PHONY: build test dev api web web-dev typecheck lint contracts contracts-check check up down logs

build:
	go build ./...

test:
	go test ./...

dev:
	docker compose up -d db
	@$(MAKE) api & api_pid=$$!; \
	  npm --prefix web run dev & web_pid=$$!; \
	  trap 'kill $$api_pid $$web_pid 2>/dev/null || true' INT TERM EXIT; \
	  wait

api:
	go run ./cmd/context-atlas

web:
	npm --prefix web ci
	npm --prefix web run build

web-dev:
	npm --prefix web run dev

typecheck:
	npm --prefix web run typecheck

lint:
	golangci-lint run ./...

contracts:
	TZ=UTC go run ./cmd/context-atlas openapi > api/openapi.json
	TZ=UTC npm --prefix web run generate:api

contracts-check: contracts
	git diff --exit-code -- api/openapi.json web/src/api/generated

check:
	./scripts/pre-commit.sh

up:
	docker compose up --build --wait

down:
	docker compose down

logs:
	docker compose logs -f
