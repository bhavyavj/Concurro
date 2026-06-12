package job

import (
	"testing"
)

func TestJobIsTerminal(t *testing.T) {
	tests := []struct {
		status   string
		expected bool
	}{
		{StatusPending, false},
		{StatusRunning, false},
		{StatusCompleted, true},
		{StatusFailed, true},
		{StatusCancelled, true},
	}

	for _, tt := range tests {
		j := &Job{Status: tt.status}
		if got := j.IsTerminal(); got != tt.expected {
			t.Errorf("IsTerminal(%s) = %v, want %v", tt.status, got, tt.expected)
		}
	}
}

func TestJobProgress(t *testing.T) {
	j := &Job{
		Items:   []string{"a", "b", "c"},
		Results: []ItemResult{{}, {}},
	}
	done, total := j.Progress()
	if done != 2 || total != 3 {
		t.Errorf("Progress() = (%d, %d), want (2, 3)", done, total)
	}
}
