package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/bhavyavj/Concurro/internal/job"
	"github.com/bhavyavj/Concurro/internal/store"
	"github.com/bhavyavj/Concurro/internal/worker"
)

type JobsHandler struct {
	store store.Store
	pool  *worker.Pool
	log   *slog.Logger
}

func NewJobsHandler(st store.Store, pool *worker.Pool, logger *slog.Logger) *JobsHandler {
	return &JobsHandler{
		store: st,
		pool:  pool,
		log:   logger,
	}
}

// SubmitJob accepts a batch of items (one per line) and enqueues them.
func (h *JobsHandler) SubmitJob(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type        string   `json:"type"`
		Items       []string `json:"items"`
		ItemsRaw    string   `json:"items_raw"` // newline separated from forms
		WorkerCount int      `json:"worker_count"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// Try form data as fallback
		if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" || r.FormValue("items_raw") != "" {
			req.ItemsRaw = r.FormValue("items_raw")
			req.Type = "url_batch"
		} else {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
	}

	if req.Type == "" {
		req.Type = "url_batch"
	}

	// Support both JSON array and raw newline text
	items := req.Items
	if len(items) == 0 && req.ItemsRaw != "" {
		for _, line := range strings.Split(req.ItemsRaw, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				items = append(items, line)
			}
		}
	}

	if len(items) == 0 {
		http.Error(w, "no items provided", http.StatusBadRequest)
		return
	}
	if len(items) > 500 {
		http.Error(w, "too many items (max 500 for demo)", http.StatusBadRequest)
		return
	}

	wc := req.WorkerCount
	if wc <= 0 {
		wc = 0 // pool default will be used
	}

	j := &job.Job{
		ID:          uuid.NewString(),
		Type:        req.Type,
		Status:      job.StatusRunning,
		Items:       items,
		WorkerCount: wc,
	}

	// Use background context with timeout for DB to avoid "context deadline exceeded" when workers are writing.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := h.store.CreateJob(ctx, j); err != nil {
		h.log.Error("create job failed", "error", err)
		http.Error(w, "failed to create job", http.StatusInternalServerError)
		return
	}

	// Enqueue every item into the worker pool
	for _, it := range items {
		h.pool.Enqueue(j.ID, it)
	}

	h.log.Info("job submitted", "job_id", j.ID, "type", j.Type, "items", len(items))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"job_id": j.ID,
		"status": j.Status,
		"total":  len(items),
	})
}

// ListJobs returns recent jobs (lightweight).
func (h *JobsHandler) ListJobs(w http.ResponseWriter, r *http.Request) {
	// Use background context with timeout for DB list to avoid deadline under worker contention.
	listCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	jobs, err := h.store.ListJobs(listCtx, 100)
	if err != nil {
		http.Error(w, "failed to list jobs", http.StatusInternalServerError)
		return
	}

	// Enrich with progress
	type summary struct {
		*job.Job
		Done     int `json:"done"`
		Progress int `json:"progress"` // 0-100
	}
	var out []summary
	for _, j := range jobs {
		done := j.ResultCount
		total := j.TotalItems
		prog := 0
		if total > 0 {
			prog = done * 100 / total
		}
		out = append(out, summary{Job: j, Done: done, Progress: prog})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// GetJob returns full details including all results.
func (h *JobsHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	j, err := h.store.GetJob(r.Context(), id)
	if err != nil {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	done, total := j.Progress()
	resp := map[string]any{
		"job":      j,
		"done":     done,
		"total":    total,
		"progress": 0,
	}
	if total > 0 {
		resp["progress"] = done * 100 / total
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// CancelJob cancels a running job.
func (h *JobsHandler) CancelJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.store.CancelJob(r.Context(), id); err != nil {
		http.Error(w, "failed to cancel", http.StatusInternalServerError)
		return
	}
	h.pool.CancelJob(id)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
}

// Stats returns simple live stats from the pool + DB.
func (h *JobsHandler) Stats(w http.ResponseWriter, r *http.Request) {
	type stats struct {
		ActiveWorkers int `json:"active_workers"`
		QueueDepth    int `json:"queue_depth"`
	}

	s := stats{
		ActiveWorkers: h.pool.ActiveWorkers(),
		QueueDepth:    h.pool.QueueLen(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}
