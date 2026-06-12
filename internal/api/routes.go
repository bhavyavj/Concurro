package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/bhavyavj/Concurro/internal/api/handlers"
	"github.com/bhavyavj/Concurro/internal/store"
	"github.com/bhavyavj/Concurro/internal/worker"
)

func NewRouter(st store.Store, pool *worker.Pool, logger *slog.Logger) http.Handler {
	r := chi.NewRouter()

	// Built-in chi middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second)) // per-request timeout

	jobs := handlers.NewJobsHandler(st, pool, logger)

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Post("/jobs", jobs.SubmitJob)
		r.Get("/jobs", jobs.ListJobs)
		r.Get("/jobs/{id}", jobs.GetJob)
		r.Post("/jobs/{id}/cancel", jobs.CancelJob)
		r.Get("/stats", jobs.Stats)
	})

	// Health & readiness
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	})
	r.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("# HELP concurro_active_workers Current active workers\n"))
	})

	// Dashboard UI (self-contained for easy demo)
	r.Get("/", serveDashboard)

	return r
}
