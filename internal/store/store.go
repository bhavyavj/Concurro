package store

import (
	"context"

	"github.com/bhavyavj/Concurro/internal/job"
)

// Store defines persistence operations for jobs and results.
// It is the boundary between the worker pool / API and the database.
type Store interface {
	// CreateJob inserts a new job and returns it (with generated ID if needed).
	CreateJob(ctx context.Context, j *job.Job) error

	// GetJob retrieves a job by ID, including its results.
	GetJob(ctx context.Context, id string) (*job.Job, error)

	// ListJobs returns recent jobs (most recent first). Limit 0 means default.
	ListJobs(ctx context.Context, limit int) ([]*job.Job, error)

	// UpdateJob applies partial updates (status, completed_at, etc.).
	UpdateJob(ctx context.Context, id string, updates map[string]any) error

	// AppendResult adds one item result to a job and updates counters on the job.
	AppendResult(ctx context.Context, jobID string, result job.ItemResult) error

	// CancelJob marks a job as cancelled.
	CancelJob(ctx context.Context, id string) error

	// Close releases any resources (DB connection).
	Close() error
}
