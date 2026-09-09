# Thin dispatcher. Every target is a 1–3-line wrapper over a real tool;
# anything with logic lives in scripts/.

.PHONY: build test lint proto sqlc ui dev drift tools

build:
	CGO_ENABLED=0 go build -trimpath -o bin/ ./cmd/...

test:
	go test -race ./...

lint:
	golangci-lint run ./...
	buf lint
	npm --prefix ui run lint

proto:
	buf generate

sqlc:
	sqlc generate

ui:
	npm --prefix ui ci --no-audit --no-fund
	npm --prefix ui run build

dev:
	docker compose up --build

drift:
	scripts/check-drift.sh

tools:
	scripts/install-tools.sh
