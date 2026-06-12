package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/bhavyavj/Concurro/internal/job"
)

// Processor is the interface for processing a single work item.
// Implementations are responsible for respecting context cancellation and timeouts.
type Processor interface {
	// Process executes work for one item and returns the result.
	// It must be safe for concurrent use.
	Process(ctx context.Context, item string) (job.ItemResult, error)
}

// Registry maps job types to their processor implementations.
type Registry struct {
	processors map[string]Processor
}

func NewRegistry() *Registry {
	r := &Registry{
		processors: make(map[string]Processor),
	}
	// Register built-in processors here.
	// These represent real-world use cases the platform can handle.
	r.Register("url_batch", NewURLProcessor())
	r.Register("log_analyze", NewLogProcessor())
	return r
}

func (r *Registry) Register(jobType string, p Processor) {
	r.processors[jobType] = p
}

func (r *Registry) Get(jobType string) (Processor, error) {
	p, ok := r.processors[jobType]
	if !ok {
		return nil, errors.New("unknown job type: " + jobType)
	}
	return p, nil
}

// URLProcessor fetches URLs concurrently and extracts useful metadata.
type URLProcessor struct {
	client *http.Client
	titleRe *regexp.Regexp
}

func NewURLProcessor() *URLProcessor {
	return &URLProcessor{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
		titleRe: regexp.MustCompile(`(?i)<title>(.*?)</title>`),
	}
}

func (p *URLProcessor) Process(ctx context.Context, item string) (job.ItemResult, error) {
	result := job.ItemResult{
		URL:         item,
		ProcessedAt: time.Now().UTC(),
	}

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item, nil)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	req.Header.Set("User-Agent", "Concurro/1.0 (+https://github.com/bhavyavj/Concurro)")

	resp, err := p.client.Do(req)
	latency := time.Since(start)
	result.LatencyMs = latency.Milliseconds()

	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	defer resp.Body.Close()

	result.StatusCode = resp.StatusCode

	// Limit body read to 1MB to protect memory on huge responses
	limited := io.LimitReader(resp.Body, 1<<20)
	body, err := io.ReadAll(limited)
	if err != nil {
		result.Error = "failed to read body: " + err.Error()
		return result, err
	}

	result.ContentLength = int64(len(body))

	// Compute hash of body (first 1MB)
	sum := sha256.Sum256(body)
	result.Hash = hex.EncodeToString(sum[:])[:16] // first 16 chars for brevity

	// Try to extract title (very basic, no full HTML parser to avoid deps)
	if resp.StatusCode == 200 && strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		if m := p.titleRe.FindSubmatch(body); len(m) == 2 {
			title := strings.TrimSpace(string(m[1]))
			// Truncate long titles
			if len(title) > 120 {
				title = title[:117] + "..."
			}
			result.Title = title
		}
	}

	// Treat non-2xx as soft error but still record the result
	if resp.StatusCode >= 400 {
		result.Error = http.StatusText(resp.StatusCode)
	}

	return result, nil
}

// LogProcessor demonstrates concurrent log analysis — a very common real-world use case
// (think tailing/processing application logs, audit logs, or server logs at scale).
type LogProcessor struct{}

func NewLogProcessor() *LogProcessor {
	return &LogProcessor{}
}

func (p *LogProcessor) Process(ctx context.Context, item string) (job.ItemResult, error) {
	result := job.ItemResult{
		URL:         item, // reuse "URL" field as the log line for simplicity
		ProcessedAt: time.Now().UTC(),
	}

	// Simulate some processing work (real version would do regex, parsing, etc.)
	start := time.Now()

	// Very simple analysis (production version would be much richer)
	upper := strings.ToUpper(item)
	level := "INFO"
	if strings.Contains(upper, "ERROR") || strings.Contains(upper, "FATAL") || strings.Contains(upper, "CRITICAL") {
		level = "ERROR"
		result.Error = "error detected in log line"
	} else if strings.Contains(upper, "WARN") || strings.Contains(upper, "WARNING") {
		level = "WARN"
	}

	result.Title = level
	result.LatencyMs = time.Since(start).Milliseconds()

	// "Content length" can represent line length or bytes processed
	result.ContentLength = int64(len(item))

	// Hash can be used for a simple signature of the line
	sum := sha256.Sum256([]byte(item))
	result.Hash = hex.EncodeToString(sum[:])[:12]

	// Respect cancellation (important for long log files)
	select {
	case <-ctx.Done():
		result.Error = "cancelled during analysis"
		return result, ctx.Err()
	default:
	}

	return result, nil
}
