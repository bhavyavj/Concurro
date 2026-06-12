package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	apiURL string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "concurro",
		Short: "Concurro CLI - interact with the concurrent job platform",
	}

	rootCmd.PersistentFlags().StringVar(&apiURL, "api", "http://localhost:8080", "Base URL of the Concurro API server")

	// job command group
	jobCmd := &cobra.Command{
		Use:   "job",
		Short: "Manage jobs",
	}

	submitCmd := &cobra.Command{
		Use:   "submit [items...]",
		Short: "Submit a new batch job (one item per arg or via --file)",
		RunE:  runSubmit,
	}
	submitCmd.Flags().StringP("file", "f", "", "Read items (one per line) from a file")
	submitCmd.Flags().IntP("workers", "w", 0, "Override number of workers for this job")
	submitCmd.Flags().StringP("type", "t", "url_batch", "Job type: url_batch (website health) or log_analyze (parse logs for errors)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List recent jobs",
		RunE:  runList,
	}

	getCmd := &cobra.Command{
		Use:   "get <job-id>",
		Short: "Get details and results for a job",
		Args:  cobra.ExactArgs(1),
		RunE:  runGet,
	}

	cancelCmd := &cobra.Command{
		Use:   "cancel <job-id>",
		Short: "Cancel a running job",
		Args:  cobra.ExactArgs(1),
		RunE:  runCancel,
	}

	jobCmd.AddCommand(submitCmd, listCmd, getCmd, cancelCmd)
	rootCmd.AddCommand(jobCmd)

	// serve is just a hint (actual server is in the api binary)
	rootCmd.AddCommand(&cobra.Command{
		Use:   "serve",
		Short: "Start the Concurro server (alias for the main binary)",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Use the main 'concurro' binary or 'go run ./cmd/api serve' to start the server.")
		},
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runSubmit(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString("file")
	wc, _ := cmd.Flags().GetInt("workers")
	jobType, _ := cmd.Flags().GetString("type")

	var items []string

	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				items = append(items, line)
			}
		}
	} else {
		items = args
	}

	if len(items) == 0 {
		return fmt.Errorf("no items provided (use args or --file)")
	}

	body := map[string]any{
		"type":  jobType,
		"items": items,
	}
	if wc > 0 {
		body["worker_count"] = wc
	}

	b, _ := json.Marshal(body)

	resp, err := http.Post(apiURL+"/api/jobs", "application/json", bytes.NewReader(b))
	if err != nil {
		return niceConnError(err)
	}
	defer resp.Body.Close()

	var result struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
		Total  int    `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// fallback to raw if decode fails
		data, _ := io.ReadAll(resp.Body)
		fmt.Printf("Submitted job. Raw response: %s\n", string(data))
		return nil
	}

	fmt.Printf("✓ Job submitted successfully! (type: %s)\n", jobType)
	fmt.Printf("  Job ID : %s\n", result.JobID)
	fmt.Printf("  Status : %s\n", result.Status)
	fmt.Printf("  Items  : %d\n\n", result.Total)
	fmt.Printf("Next steps:\n")
	fmt.Printf("  go run ./cmd/cli job list\n")
	fmt.Printf("  go run ./cmd/cli job get %s\n", result.JobID)
	return nil
}

func runList(cmd *cobra.Command, args []string) error {
	resp, err := http.Get(apiURL + "/api/jobs")
	if err != nil {
		return niceConnError(err)
	}
	defer resp.Body.Close()

	var jobs []struct {
		ID         string    `json:"id"`
		Status     string    `json:"status"`
		Progress   int       `json:"progress"`
		TotalItems int       `json:"total_items"`
		CreatedAt  time.Time `json:"created_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&jobs); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(jobs) == 0 {
		fmt.Println("No jobs found.")
		return nil
	}

	fmt.Printf("%-12s  %-10s  %-8s  %-6s  %s\n", "ID (short)", "STATUS", "PROGRESS", "ITEMS", "SUBMITTED")
	fmt.Printf("%s\n", strings.Repeat("-", 70))
	for _, j := range jobs {
		shortID := j.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		fmt.Printf("%-12s  %-10s  %3d%%      %-6d  %s\n",
			shortID,
			j.Status,
			j.Progress,
			j.TotalItems,
			j.CreatedAt.Format("15:04:05"),
		)
	}
	fmt.Println()
	fmt.Println("Tip: Use `go run ./cmd/cli job get <full-id>` for details (copy full ID from the UI or API).")
	return nil
}

func runGet(cmd *cobra.Command, args []string) error {
	id := args[0]
	resp, err := http.Get(apiURL + "/api/jobs/" + id)
	if err != nil {
		return niceConnError(err)
	}
	defer resp.Body.Close()

	var data struct {
		Job struct {
			ID           string    `json:"id"`
			Type         string    `json:"type"`
			Status       string    `json:"status"`
			WorkerCount  int       `json:"worker_count"`
			CreatedAt    time.Time `json:"created_at"`
			CompletedAt  *time.Time `json:"completed_at"`
			TotalItems   int       `json:"total_items"`
			SuccessCount int       `json:"success_count"`
			FailureCount int       `json:"failure_count"`
			Results      []struct {
				URL         string `json:"url"`
				StatusCode  int    `json:"status_code"`
				LatencyMs   int64  `json:"latency_ms"`
				Title       string `json:"title"`
				Error       string `json:"error"`
			} `json:"results"`
		} `json:"job"`
		Done     int `json:"done"`
		Total    int `json:"total"`
		Progress int `json:"progress"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("failed to parse job: %w", err)
	}

	j := data.Job
	fmt.Printf("Job %s\n", j.ID)
	fmt.Printf("  Type      : %s\n", j.Type)
	fmt.Printf("  Status    : %s\n", j.Status)
	fmt.Printf("  Workers   : %d\n", j.WorkerCount)
	fmt.Printf("  Progress  : %d/%d (%d%%)\n", data.Done, data.Total, data.Progress)
	fmt.Printf("  Success   : %d   Failures: %d\n", j.SuccessCount, j.FailureCount)
	fmt.Printf("  Created   : %s\n", j.CreatedAt.Format(time.RFC1123))
	if j.CompletedAt != nil {
		fmt.Printf("  Completed : %s\n", j.CompletedAt.Format(time.RFC1123))
	}

	if len(j.Results) > 0 {
		fmt.Println("\nResults:")
		fmt.Printf("  %-40s  %6s  %8s  %s\n", "URL", "STATUS", "LATENCY", "TITLE / ERROR")
		fmt.Printf("  %s\n", strings.Repeat("-", 90))
		for _, r := range j.Results {
			titleOrErr := r.Title
			if r.Error != "" {
				titleOrErr = r.Error
			}
			if len(titleOrErr) > 35 {
				titleOrErr = titleOrErr[:32] + "..."
			}
			fmt.Printf("  %-40s  %6d  %6dms  %s\n",
				truncate(r.URL, 40),
				r.StatusCode,
				r.LatencyMs,
				titleOrErr,
			)
		}
	} else {
		fmt.Println("\n(No results yet — workers may still be processing.)")
	}
	return nil
}

func runCancel(cmd *cobra.Command, args []string) error {
	id := args[0]
	resp, err := http.Post(apiURL+"/api/jobs/"+id+"/cancel", "application/json", nil)
	if err != nil {
		return niceConnError(err)
	}
	defer resp.Body.Close()
	fmt.Printf("Cancel requested for job %s\n", id)
	return nil
}

// niceConnError gives friendly advice when the API is unreachable.
func niceConnError(err error) error {
	if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") {
		return fmt.Errorf("cannot reach Concurro server at %s\n\nIs the server running?\n\nStart it in another terminal with:\n  cd ~/Projects/concurro && go run ./cmd/api serve\n\nThen try your command again.", apiURL)
	}
	return err
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}
