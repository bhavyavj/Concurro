# Concurro

**High-concurrency job processing platform** built in Go — designed as a portfolio project to demonstrate production-grade backend engineering skills.

Concurro lets you submit batch jobs (e.g. bulk URL analysis) that are processed by a configurable pool of concurrent workers. It exposes both a clean REST API and a powerful CLI, plus a simple live web dashboard to visualize the parallelism in action.

## Why this project?

This project was built specifically to showcase skills valued by remote backend Go roles:

- **Deep concurrency mastery**: Custom bounded worker pool using channels, context, WaitGroups, cancellation, and graceful shutdown.
- **Clean architecture**: Clear separation (handlers → service/queue → workers → store), interface-based design, testable components.
- **Real production patterns**: Structured logging (`slog`), context everywhere, DB transactions where appropriate, health endpoints, configurable via env/flags.
- **CLI + HTTP service**: Dual interface using Cobra + Chi — common in real Go systems (internal tools + public API).
- **Persistence + observability**: SQLite (CGO-free), job history, progress tracking.
- **Easy to demo**: Docker one-command run + live web UI that shows workers processing items in parallel.
- **Well-documented**: Architecture decisions, concurrency patterns, and how to extend it are explained.

## Key Features

- Submit batch jobs via CLI or web UI (one item per line, e.g. URLs)
- Configurable worker pool (default 8, tunable per job or globally)
- Real concurrent processing with proper cancellation support
- Job status tracking (pending → running → completed/failed/cancelled)
- Per-item results with rich metadata
- Live updating dashboard (polling + clean UI)
- Full CRUD for jobs via REST API
- Graceful shutdown (drains in-flight work)
- SQLite persistence (single binary, no external DB needed)
- Docker + docker-compose support
- Structured logs and basic metrics endpoint

## Tech Stack

- **Language**: Go 1.22+
- **Router**: `github.com/go-chi/chi/v5`
- **CLI**: `github.com/spf13/cobra`
- **Database**: `modernc.org/sqlite` (pure Go, CGO-free)
- **Web UI**: Go `html/template` + vanilla JS + minimal CSS
- **Concurrency**: Custom worker pool (no external job queue library)
- **Observability**: `log/slog` + `/healthz`, `/readyz`, `/metrics`

## Quick Start (Two Terminals Recommended)

### 1. Start the server (Terminal 1)

The easiest way is using Make:

```bash
cd ~/Projects/concurro

# Recommended: run in background with logs + easy stop
make serve-bg

# Or run in foreground (blocks your terminal)
make serve
```

Useful commands:
- `make stop` — cleanly stop the background server (or kill anything on port 8080)
- `make restart` — stop + start again
- `make status` — check if it's running
- `make logs` — follow the server output
- `make help` — see all available targets

The server also starts the worker pool automatically.

Open your browser to **http://localhost:8080** — you'll see a live dashboard with explanations of the real problems the architecture addresses.

### What you will actually observe (the important part)

1. Pick a job type (Bulk URL Health Checker or Log Analyzer) in the form.
2. Submit a batch (use the demo button or paste your own items).
3. Immediately the **ACTIVE WORKERS** counter jumps (e.g. to 8). This means 8 items are being processed **at the exact same time**.
4. The **QUEUE DEPTH** shows work waiting in the Go channel.
5. Click a job row to watch per-item results appear (real HTTP calls for URLs, or log line parsing for the analyzer).
6. The work is real and concurrent — this is the exact pattern used for high-throughput background work in production.

This is the core value: showing you can safely and correctly parallelize work in Go instead of slow sequential loops.

### 2. Use the CLI (Terminal 2)

```bash
cd ~/Projects/concurro

# Submit a batch job (the server must be running)
go run ./cmd/cli job submit https://golang.org https://github.com https://example.com

# See recent jobs (nicely formatted table)
go run ./cmd/cli job list

# Get full details + per-item results for a specific job (copy ID from list or UI)
go run ./cmd/cli job get <paste-job-id-here>

# Cancel a running job if needed
go run ./cmd/cli job cancel <job-id>
```

The CLI gives friendly errors if the server isn't running and suggests the exact command to start it.

### With Docker (great for clean demos)

```bash
docker compose up --build
```

Then use the same CLI commands (or the web UI at http://localhost:8080). The container runs everything needed.

### Using the pre-built binaries

After `make build` (or the build step in Docker):

```bash
./bin/concurro serve          # server + workers
./bin/concurro-cli job list   # CLI (talks to localhost:8080 by default)
```

## Real-Life Problems This Architecture Solves

Concurro is not "just another URL checker". It is a **small but realistic implementation of a concurrent job processing platform**.

The same design (reliable job submission + bounded worker pool + queue + result tracking + graceful lifecycle) powers real systems at companies of all sizes:

### 1. Bulk URL / Website Health Checking & Monitoring (current `url_batch` type)
- SEO agencies and dev teams regularly need to check thousands of links for broken pages, slow responses, or missing titles.
- Uptime/monitoring tools do the same at scale for customer sites.
- Instead of a slow `for` loop, you fan out the work to many goroutines while keeping memory and connections under control (the bounded pool).

### 2. Log Analysis & Parsing at Scale (current `log_analyze` type)
- Applications and servers produce huge volumes of logs.
- Teams need to quickly scan for ERRORs, count occurrences, extract patterns, or alert on anomalies.
- Processing lines sequentially on one CPU core is too slow when you have gigabytes of logs. Workers process chunks concurrently.

### 3. Other common real problems the platform can power (easy to extend)
- Processing uploaded CSVs or data files in parallel (ETL jobs)
- Sending bulk emails / notifications / push messages with rate limits and retries
- Generating reports or thumbnails for many items
- Running background tasks triggered by users (e.g. "export my data", "reindex my documents")
- Web crawling or content aggregation with politeness controls

The key hard parts this project demonstrates correctly:
- Controlled concurrency (never overwhelm the system or external services)
- Context cancellation + per-job cancellation
- Graceful shutdown (no lost work on deploy/restart)
- Job persistence and observability
- Clean separation between "what work to do" (Processor) and "how to schedule it safely" (Pool)

This is exactly the kind of code senior Go backend/platform engineers write.

## Architecture Overview

```
cmd/api          → entrypoint for the server (starts HTTP + embedded workers)
cmd/cli          → CLI client (talks to the API by default)

internal/
  config/        → configuration (env + defaults)
  job/           → Job and ItemResult models + status constants
  processor/     → pluggable processors (url_batch, future: log_analyze, etc.)
  store/         → Store interface + SQLite implementation
  worker/        → WorkerPool — the heart of concurrency
  api/
    handlers/    → HTTP handlers (jobs, health)
    middleware/  → logging, recovery, etc.
    routes.go    → chi router setup

web/templates/   → dashboard UI (layout + pages)

The worker pool is started inside the API server. Workers pull work items from a channel and execute them using the configured Processor. Results are streamed back to the job store.

**In simple terms**: You throw a list of work at it. Go's concurrency (goroutines + channels) lets many workers do the work at the same time instead of one after another. The dashboard makes the parallelism visible.
```

### The Worker Pool (most important part for interviews)

The pool is deliberately custom-built (not using a library) to demonstrate:

- Bounded concurrency using a worker goroutine pool + work channel
- Context-based cancellation at job and global level
- Graceful drain on shutdown (`SIGINT` / `SIGTERM`)
- Progress reporting back to the job store
- Decoupling of "what to process" (Processor interface) from "how to schedule"

See `internal/worker/pool.go` for the implementation.

## Submitting a Demo Job

1. In the UI, paste 20-50 URLs (one per line) into the form and submit.
2. Watch the stats update and the results table fill as workers process items concurrently.
3. You can submit multiple batches to see the shared pool in action.
4. Use the CLI in another terminal to submit/cancel/list while the UI is open.

Example URLs for testing:

```
https://golang.org
https://github.com
https://httpbin.org/status/200
https://example.com
...
```

## Extending It

Adding a new job type (e.g. `csv_transform` or `log_analyze`) is straightforward:

1. Implement `processor.Processor` interface
2. Register it in the processor registry
3. Update job submission to accept the new type + payload

The architecture was intentionally designed to be extensible without touching the worker pool or queue logic.

## Makefile Targets

- `make serve` — run the server locally
- `make docker-up` — build and run via Docker Compose
- `make test` — run all tests with race detector
- `make build` — build both API and CLI binaries

## Future Enhancements (ideas)

- Server-Sent Events (SSE) for true real-time updates instead of polling
- File watcher component (submit jobs on new files in a watched dir)
- Multiple processor types (log analysis, image resize via stdlib, etc.)
- Prometheus metrics export
- JWT auth + multi-tenancy (for extra "production" feel)
- Retry with exponential backoff per item
- Job result export (CSV/JSON)

## License

MIT

---

Built as a portfolio project to demonstrate strong Go backend and concurrency skills for remote opportunities.
