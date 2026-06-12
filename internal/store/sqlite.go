package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	_ "modernc.org/sqlite"

	"github.com/bhavyavj/Concurro/internal/job"
)

const schema = `
PRAGMA foreign_keys = ON;
PRAGMA journal_mode = WAL;

CREATE TABLE IF NOT EXISTS jobs (
	id            TEXT PRIMARY KEY,
	type          TEXT NOT NULL,
	status        TEXT NOT NULL,
	items_json    TEXT NOT NULL,
	worker_count  INTEGER NOT NULL DEFAULT 4,
	created_at    DATETIME NOT NULL,
	updated_at    DATETIME NOT NULL,
	completed_at  DATETIME
);

CREATE TABLE IF NOT EXISTS job_results (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	job_id         TEXT NOT NULL,
	url            TEXT NOT NULL,
	status_code    INTEGER,
	latency_ms     INTEGER,
	title          TEXT,
	content_length INTEGER,
	hash           TEXT,
	error          TEXT,
	processed_at   DATETIME NOT NULL,
	FOREIGN KEY(job_id) REFERENCES jobs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_job_results_job_id ON job_results(job_id);
`

type SQLiteStore struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewSQLiteStore opens (or creates) the SQLite database and ensures schema.
func NewSQLiteStore(dbPath string, logger *slog.Logger) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1) // Safe for WAL mode with pure Go driver in single process
	db.SetMaxIdleConns(1)

	if err := db.PingContext(context.Background()); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	s := &SQLiteStore{db: db, logger: logger}
	return s, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) CreateJob(ctx context.Context, j *job.Job) error {
	itemsJSON, err := json.Marshal(j.Items)
	if err != nil {
		return fmt.Errorf("marshal items: %w", err)
	}

	now := time.Now().UTC()
	j.CreatedAt = now
	j.UpdatedAt = now

	if j.Status == "" {
		j.Status = job.StatusPending
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO jobs (id, type, status, items_json, worker_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, j.ID, j.Type, j.Status, string(itemsJSON), j.WorkerCount, j.CreatedAt, j.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetJob(ctx context.Context, id string) (*job.Job, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, type, status, items_json, worker_count, created_at, updated_at, completed_at
		FROM jobs WHERE id = ?
	`, id)

	var j job.Job
	var itemsJSON string
	var completedAt sql.NullTime

	err := row.Scan(
		&j.ID, &j.Type, &j.Status, &itemsJSON, &j.WorkerCount,
		&j.CreatedAt, &j.UpdatedAt, &completedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("job not found: %s", id)
		}
		return nil, fmt.Errorf("scan job: %w", err)
	}

	if err := json.Unmarshal([]byte(itemsJSON), &j.Items); err != nil {
		return nil, fmt.Errorf("unmarshal items: %w", err)
	}
	j.TotalItems = len(j.Items)

	if completedAt.Valid {
		j.CompletedAt = &completedAt.Time
	}

	// Load results
	results, success, failure, err := s.loadResults(ctx, id)
	if err != nil {
		return nil, err
	}
	j.Results = results
	j.SuccessCount = success
	j.FailureCount = failure

	return &j, nil
}

func (s *SQLiteStore) loadResults(ctx context.Context, jobID string) ([]job.ItemResult, int, int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT url, status_code, latency_ms, title, content_length, hash, error, processed_at
		FROM job_results
		WHERE job_id = ?
		ORDER BY processed_at ASC
	`, jobID)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("query results: %w", err)
	}
	defer rows.Close()

	var results []job.ItemResult
	var success, failure int

	for rows.Next() {
		var r job.ItemResult
		var procAt time.Time
		if err := rows.Scan(
			&r.URL, &r.StatusCode, &r.LatencyMs, &r.Title,
			&r.ContentLength, &r.Hash, &r.Error, &procAt,
		); err != nil {
			return nil, 0, 0, fmt.Errorf("scan result: %w", err)
		}
		r.ProcessedAt = procAt

		results = append(results, r)
		if r.Error != "" || r.StatusCode >= 400 {
			failure++
		} else {
			success++
		}
	}
	return results, success, failure, nil
}

func (s *SQLiteStore) ListJobs(ctx context.Context, limit int) ([]*job.Job, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, status, items_json, worker_count, created_at, updated_at, completed_at
		FROM jobs
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list jobs query: %w", err)
	}
	defer rows.Close()

	var jobs []*job.Job
	for rows.Next() {
		var j job.Job
		var itemsJSON string
		var completedAt sql.NullTime

		if err := rows.Scan(
			&j.ID, &j.Type, &j.Status, &itemsJSON, &j.WorkerCount,
			&j.CreatedAt, &j.UpdatedAt, &completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan list job: %w", err)
		}

		_ = json.Unmarshal([]byte(itemsJSON), &j.Items)
		j.TotalItems = len(j.Items)
		if completedAt.Valid {
			j.CompletedAt = &completedAt.Time
		}

		// For list we load lightweight results count only (avoid heavy load)
		var resCount int
		s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM job_results WHERE job_id = ?`, j.ID).Scan(&resCount)
		j.Results = make([]job.ItemResult, 0, resCount) // just to set length for progress

		jobs = append(jobs, &j)
	}
	return jobs, nil
}

func (s *SQLiteStore) UpdateJob(ctx context.Context, id string, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}

	// Very small whitelist to prevent SQL injection via keys
	allowed := map[string]bool{
		"status": true, "completed_at": true, "worker_count": true,
	}

	setClauses := []string{}
	args := []any{}

	for k, v := range updates {
		if !allowed[k] {
			continue
		}
		setClauses = append(setClauses, k+" = ?")
		args = append(args, v)
	}
	if len(setClauses) == 0 {
		return nil
	}

	args = append(args, time.Now().UTC(), id)
	query := fmt.Sprintf(`UPDATE jobs SET %s, updated_at = ? WHERE id = ?`, join(setClauses, ", "))

	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update job: %w", err)
	}
	return nil
}

func (s *SQLiteStore) AppendResult(ctx context.Context, jobID string, result job.ItemResult) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO job_results (job_id, url, status_code, latency_ms, title, content_length, hash, error, processed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, jobID, result.URL, result.StatusCode, result.LatencyMs, result.Title,
		result.ContentLength, result.Hash, result.Error, result.ProcessedAt)
	if err != nil {
		return fmt.Errorf("insert result: %w", err)
	}

	// Update job updated_at
	_, err = tx.ExecContext(ctx, `UPDATE jobs SET updated_at = ? WHERE id = ?`, time.Now().UTC(), jobID)
	if err != nil {
		return fmt.Errorf("update job timestamp: %w", err)
	}

	return tx.Commit()
}

func (s *SQLiteStore) CancelJob(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE jobs SET status = ?, completed_at = ?, updated_at = ?
		WHERE id = ?
	`, job.StatusCancelled, now, now, id)
	if err != nil {
		return fmt.Errorf("cancel job: %w", err)
	}
	return nil
}

// join is a tiny helper (stdlib strings.Join would require import).
func join(elems []string, sep string) string {
	if len(elems) == 0 {
		return ""
	}
	res := elems[0]
	for _, e := range elems[1:] {
		res += sep + e
	}
	return res
}
