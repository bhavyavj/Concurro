.PHONY: build run serve serve-bg stop restart status logs cli test clean docker-build docker-up docker-down

APP_NAME := concurro
BINARY := $(APP_NAME)

GO := go
GOFLAGS := -ldflags="-s -w"

# Background server support
PID_FILE := .concurro.pid
LOG_FILE := .concurro.log
PORT := 8080
ADDR := :$(PORT)

build:
	$(GO) build -o bin/$(BINARY) ./cmd/api
	$(GO) build -o bin/$(BINARY)-cli ./cmd/cli

run: build
	./bin/$(BINARY)

serve:
	$(GO) run ./cmd/api serve

# Run server in background (recommended for development)
# Usage: make serve-bg
# Then: make stop   or   make logs
serve-bg:
	@$(MAKE) build > /dev/null 2>&1
	@if [ -f $(PID_FILE) ] && ps -p $$(cat $(PID_FILE)) > /dev/null 2>&1; then \
		echo "⚠️  Concurro is already running (PID: $$(cat $(PID_FILE)))"; \
		echo "   Use 'make stop' to stop it first, or 'make restart'"; \
	else \
		echo "🚀 Starting Concurro in background on $(ADDR)..."; \
		CONCURRO_ADDR=$(ADDR) nohup ./bin/$(BINARY) serve > $(LOG_FILE) 2>&1 & \
		echo $$! > $(PID_FILE); \
		sleep 1.2; \
		if ps -p $$(cat $(PID_FILE)) > /dev/null 2>&1; then \
			echo "✅ Server started successfully (PID: $$(cat $(PID_FILE)))"; \
			echo "   Dashboard: http://localhost:$(PORT)"; \
			echo "   Logs:      make logs   (or tail -f $(LOG_FILE))"; \
			echo "   Stop:      make stop"; \
			echo "   Restart:   make restart"; \
		else \
			echo "❌ Failed to start server. Check logs:"; \
			cat $(LOG_FILE) | tail -20; \
			rm -f $(PID_FILE); \
		fi \
	fi

# Stop the background server
stop:
	@STOPPED=0; \
	if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		if ps -p $$PID > /dev/null 2>&1; then \
			kill $$PID 2>/dev/null || true; \
			echo "✅ Stopped Concurro (PID: $$PID)"; \
			STOPPED=1; \
		fi; \
		rm -f $(PID_FILE); \
	fi; \
	if [ $$STOPPED -eq 0 ]; then \
		echo "No PID file. Killing anything on port $(PORT)..."; \
		PIDS=$$(lsof -ti :$(PORT) 2>/dev/null || true); \
		if [ -n "$$PIDS" ]; then \
			kill -9 $$PIDS 2>/dev/null || true; \
			echo "✅ Killed process(es) on port $(PORT)"; \
		else \
			echo "Nothing was running on port $(PORT)"; \
		fi; \
	fi

# Restart the background server
restart: stop serve-bg

# Show if server is running
status:
	@if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		if ps -p $$PID > /dev/null 2>&1; then \
			echo "✅ Concurro is running (PID: $$PID, port $(PORT))"; \
			echo "   http://localhost:$(PORT)"; \
		else \
			echo "❌ PID file exists but process is dead. Cleaning up..."; \
			rm -f $(PID_FILE); \
		fi \
	else \
		echo "❌ Concurro is not running (no PID file)"; \
		PIDS=$$(lsof -ti :$(PORT) 2>/dev/null || true); \
		if [ -n "$$PIDS" ]; then \
			echo "   (However something is using port $(PORT))"; \
		fi \
	fi

# Tail the server logs (only works with serve-bg)
logs:
	@if [ -f $(LOG_FILE) ]; then \
		tail -f $(LOG_FILE); \
	else \
		echo "No log file yet. Start the server with 'make serve-bg' first."; \
	fi

cli:
	$(GO) run ./cmd/cli --help

test:
	$(GO) test ./... -v -count=1 -race

test-short:
	$(GO) test ./... -short -race

clean:
	rm -rf bin/ *.db *.db-shm *.db-wal $(PID_FILE) $(LOG_FILE) .concurro.log

docker-build:
	docker build -t $(APP_NAME):latest .

docker-up:
	docker compose up --build

docker-down:
	docker compose down

# Development helpers
dev: serve

# Show available make targets
help:
	@echo "Concurro Makefile targets:"
	@echo ""
	@echo "  make serve       - Run server in foreground (default)"
	@echo "  make serve-bg    - Run server in background (recommended)"
	@echo "  make stop        - Stop background server (or kill port 8080)"
	@echo "  make restart     - Stop + start in background"
	@echo "  make status      - Check if server is running"
	@echo "  make logs        - Tail server logs (serve-bg only)"
	@echo "  make build       - Build binaries"
	@echo "  make test        - Run tests with race detector"
	@echo "  make clean       - Remove binaries, db files, pid/log"
	@echo "  make docker-up   - Run with Docker Compose"
	@echo ""
	@echo "After 'make serve-bg', open http://localhost:8080"

lint:
	$(GO) vet ./...
	staticcheck ./... || true
