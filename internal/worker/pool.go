package worker

// Package worker implements a bounded concurrent worker pool.
//
// A fixed number of goroutines pull [WorkItem] values from a shared channel,
// process each item via the registered [processor.Processor], and write results
// back to the [store.Store]. The pool supports per-job cancellation and drains
// in-flight work gracefully on shutdown.

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bhavyavj/Concurro/internal/job"
	"github.com/bhavyavj/Concurro/internal/processor"
	"github.com/bhavyavj/Concurro/internal/store"
)

// WorkItem represents one unit of work for the pool.
type WorkItem struct {
	JobID string
	Item  string // e.g. a URL
}

// Pool manages a fixed number of workers that process items concurrently.
type Pool struct {
	numWorkers int
	workCh     chan WorkItem

	// jobCtxs allows per-job cancellation without affecting the whole pool
	jobCtxs map[string]context.CancelFunc
	mu      sync.Mutex

	activeWorkers int64 // atomic

	procReg *processor.Registry
	st      store.Store
	logger  *slog.Logger

	wg     sync.WaitGroup
	cancel context.CancelFunc // global shutdown
}

// NewPool creates a new worker pool.
// numWorkers controls the maximum concurrency across all jobs.
func NewPool(numWorkers int, reg *processor.Registry, st store.Store, logger *slog.Logger) *Pool {
	if numWorkers <= 0 {
		numWorkers = 4
	}
	return &Pool{
		numWorkers: numWorkers,
		workCh:     make(chan WorkItem, 2048), // decent buffer so submitters don't block easily
		jobCtxs:    make(map[string]context.CancelFunc),
		procReg:    reg,
		st:         st,
		logger:     logger,
	}
}

// Start launches the worker goroutines. It blocks until the provided context is cancelled.
func (p *Pool) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel

	p.logger.Info("starting worker pool", "workers", p.numWorkers)

	for i := 0; i < p.numWorkers; i++ {
		p.wg.Add(1)
		go p.worker(i, ctx)
	}

	// Block until shutdown signal
	<-ctx.Done()
	p.logger.Info("worker pool shutting down, waiting for workers to finish...")

	// Close channel so workers exit cleanly
	close(p.workCh)

	p.wg.Wait()
	p.logger.Info("worker pool stopped")
}

// Shutdown initiates graceful shutdown. It stops accepting new work and waits for in-flight items.
func (p *Pool) Shutdown() {
	if p.cancel != nil {
		p.cancel()
	}
}

// Enqueue adds an item to the processing queue for a specific job.
// The caller should have already created the job in the store.
func (p *Pool) Enqueue(jobID, item string) {
	// Create a cancellable context for this job if not exists
	p.mu.Lock()
	if _, exists := p.jobCtxs[jobID]; !exists {
		jctx, jcancel := context.WithCancel(context.Background())
		p.jobCtxs[jobID] = jcancel
		_ = jctx // we store only the cancel
	}
	p.mu.Unlock()

	p.workCh <- WorkItem{JobID: jobID, Item: item}
}

// CancelJob stops any remaining work for the given job.
func (p *Pool) CancelJob(jobID string) {
	p.mu.Lock()
	if cancel, ok := p.jobCtxs[jobID]; ok {
		cancel()
		delete(p.jobCtxs, jobID)
	}
	p.mu.Unlock()

	p.logger.Info("job cancelled in pool", "job_id", jobID)
}

// ActiveWorkers returns the current number of workers processing items.
func (p *Pool) ActiveWorkers() int {
	return int(atomic.LoadInt64(&p.activeWorkers))
}

// QueueLen returns approximate number of items waiting in the queue.
func (p *Pool) QueueLen() int {
	return len(p.workCh)
}

func (p *Pool) worker(id int, globalCtx context.Context) {
	defer p.wg.Done()
	p.logger.Debug("worker started", "worker_id", id)

	for item := range p.workCh {
		// Respect global shutdown
		select {
		case <-globalCtx.Done():
			return
		default:
		}

		atomic.AddInt64(&p.activeWorkers, 1)

		start := time.Now()

		// Get job-scoped context (if cancelled, this item will be skipped or error)
		p.mu.Lock()
		jcancel, hasJobCtx := p.jobCtxs[item.JobID]
		p.mu.Unlock()

		procCtx := globalCtx
		if hasJobCtx {
			// We can't easily get the ctx from cancel func, so we use a background + check status below
			procCtx = globalCtx // fallback; we will check DB status for cancellation
		}
		_ = jcancel // only used for explicit cancel path

		// Fetch current job status to respect cancellation
		j, err := p.st.GetJob(globalCtx, item.JobID)
		if err != nil {
			p.logger.Error("failed to load job for work item", "job_id", item.JobID, "error", err)
			atomic.AddInt64(&p.activeWorkers, -1)
			continue
		}
		if j.Status == job.StatusCancelled {
			atomic.AddInt64(&p.activeWorkers, -1)
			continue
		}

		proc, err := p.procReg.Get(j.Type)
		if err != nil {
			p.logger.Error("no processor for job type", "type", j.Type, "error", err)
			_ = p.recordResult(globalCtx, item.JobID, job.ItemResult{
				URL:         item.Item,
				Error:       "unknown processor: " + j.Type,
				ProcessedAt: time.Now().UTC(),
			})
			atomic.AddInt64(&p.activeWorkers, -1)
			continue
		}

		res, procErr := proc.Process(procCtx, item.Item)
		if procErr != nil && res.Error == "" {
			res.Error = procErr.Error()
		}

		if err := p.recordResult(globalCtx, item.JobID, res); err != nil {
			p.logger.Error("failed to record result", "job_id", item.JobID, "item", item.Item, "error", err)
		}

		duration := time.Since(start)
		p.logger.Debug("item processed",
			"worker_id", id,
			"job_id", item.JobID,
			"item", item.Item,
			"status", res.StatusCode,
			"latency_ms", res.LatencyMs,
			"took", duration,
		)

		atomic.AddInt64(&p.activeWorkers, -1)

		// After recording, check if job is complete
		p.checkAndCompleteJob(globalCtx, item.JobID)
	}

	p.logger.Debug("worker exited", "worker_id", id)
}

// recordResult appends a result and updates job counters.
func (p *Pool) recordResult(ctx context.Context, jobID string, res job.ItemResult) error {
	if err := p.st.AppendResult(ctx, jobID, res); err != nil {
		return err
	}
	return nil
}

// checkAndCompleteJob counts results vs total items and marks terminal status if needed.
func (p *Pool) checkAndCompleteJob(ctx context.Context, jobID string) {
	j, err := p.st.GetJob(ctx, jobID)
	if err != nil {
		p.logger.Error("check complete: failed to load job", "job_id", jobID, "error", err)
		return
	}
	if j.IsTerminal() {
		return
	}

	done, total := j.Progress()
	if done >= total && total > 0 {
		now := time.Now().UTC()
		status := job.StatusCompleted

		// Simple heuristic: if more than half failed, mark failed (can be improved)
		if j.FailureCount > j.SuccessCount {
			status = job.StatusFailed
		}

		updates := map[string]any{
			"status":       status,
			"completed_at": &now,
		}
		if err := p.st.UpdateJob(ctx, jobID, updates); err != nil {
			p.logger.Error("failed to mark job complete", "job_id", jobID, "error", err)
			return
		}
		p.logger.Info("job completed", "job_id", jobID, "status", status, "total", total)

		// Clean up per-job cancel func
		p.mu.Lock()
		if cancel, ok := p.jobCtxs[jobID]; ok {
			cancel()
			delete(p.jobCtxs, jobID)
		}
		p.mu.Unlock()
	}
}
