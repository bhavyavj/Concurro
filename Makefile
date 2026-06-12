.PHONY: build run serve cli test clean docker-build docker-up docker-down

APP_NAME := concurro
BINARY := $(APP_NAME)

GO := go
GOFLAGS := -ldflags="-s -w"

build:
	$(GO) build -o bin/$(BINARY) ./cmd/api
	$(GO) build -o bin/$(BINARY)-cli ./cmd/cli

run: build
	./bin/$(BINARY)

serve:
	$(GO) run ./cmd/api serve

cli:
	$(GO) run ./cmd/cli --help

test:
	$(GO) test ./... -v -count=1 -race

test-short:
	$(GO) test ./... -short -race

clean:
	rm -rf bin/ *.db *.db-shm *.db-wal

docker-build:
	docker build -t $(APP_NAME):latest .

docker-up:
	docker compose up --build

docker-down:
	docker compose down

# Development helpers
dev:
	$(GO) run ./cmd/api serve

lint:
	$(GO) vet ./...
	staticcheck ./... || true
