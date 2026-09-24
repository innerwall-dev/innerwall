# Thin dispatcher. Every target is a 1–3-line wrapper over a real tool;
# anything with logic lives in scripts/.

.PHONY: build build-noconsole test test-netns test-console lint openapi proto sqlc console ui dev seed drift tools migrate

build:
	CGO_ENABLED=0 go build -trimpath -o bin/ ./cmd/...

build-noconsole:
	CGO_ENABLED=0 go build -trimpath -tags noconsole -o bin/ ./cmd/...

test:
	go test -race ./...
	go test -tags noconsole ./ui/

test-console:
	npm --prefix ui run test

test-netns:
	scripts/test-netns.sh

lint:
	golangci-lint run ./...
	buf lint
	npm --prefix ui run lint

openapi:
	scripts/check-openapi.sh

proto:
	buf generate

sqlc:
	sqlc generate

console:
	npm --prefix ui ci --no-audit --no-fund
	npm --prefix ui run build

ui: console

dev:
	docker compose up --build

seed:
	go run -tags dev ./cmd/innerwall dev seed

migrate:
	go run ./cmd/innerwall migrate

drift:
	scripts/check-drift.sh

tools:
	scripts/install-tools.sh
