package repository

import "testing"

func TestClampLimit(t *testing.T) {
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{"zero returns default", 0, defaultPageLimit},
		{"negative returns default", -1, defaultPageLimit},
		{"exceeds max returns default", maxPageLimit + 1, defaultPageLimit},
		{"valid small value", 5, 5},
		{"valid at max", maxPageLimit, maxPageLimit},
		{"valid at one", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampLimit(tt.input)
			if got != tt.want {
				t.Errorf("clampLimit(%d) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
