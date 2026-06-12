package job

import "time"

// Status constants for jobs.
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// ItemResult represents the processing result for a single item in a batch job.
type ItemResult struct {
	URL           string    `json:"url"`
	StatusCode    int       `json:"status_code"`
	LatencyMs     int64     `json:"latency_ms"`
	Title         string    `json:"title,omitempty"`
	ContentLength int64     `json:"content_length"`
	Hash          string    `json:"hash,omitempty"`
	Error         string    `json:"error,omitempty"`
	ProcessedAt   time.Time `json:"processed_at"`
}

// Job represents a batch processing job.
type Job struct {
	ID           string        `json:"id"`
	Type         string        `json:"type"` // "url_batch" for now
	Status       string        `json:"status"`
	Items        []string      `json:"items"` // list of work items (e.g. URLs)
	Results      []ItemResult  `json:"results,omitempty"`
	WorkerCount  int           `json:"worker_count"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
	TotalItems   int           `json:"total_items"`
	SuccessCount int           `json:"success_count"`
	FailureCount int           `json:"failure_count"`
}

// IsTerminal returns true if the job is in a final state.
func (j *Job) IsTerminal() bool {
	return j.Status == StatusCompleted || j.Status == StatusFailed || j.Status == StatusCancelled
}

// Progress returns completed items / total.
func (j *Job) Progress() (done, total int) {
	return len(j.Results), len(j.Items)
}
