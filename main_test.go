package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/rubrical-works/gh-pmu/internal/api"
)

func TestExitCode(t *testing.T) {
	partial := &api.PartialSuccessError{Resource: "issue #42", Steps: []string{"add to project"}, Err: errors.New("boom")}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"success", nil, 0},
		{"total failure", errors.New("failed to create issue"), 1},
		{"partial success", partial, 6},
		{"wrapped partial success", fmt.Errorf("create: %w", partial), 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exitCode(tt.err); got != tt.want {
				t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}
