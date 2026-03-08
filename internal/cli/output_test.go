package cli

import (
	"os"
	"testing"
)

func TestFormatWorktreeStatus(t *testing.T) {
	// Suppress colors for predictable output.
	os.Setenv("NO_COLOR", "1")
	defer os.Unsetenv("NO_COLOR")

	tests := []struct {
		name      string
		staged    int
		unstaged  int
		untracked int
		want      string
	}{
		{"clean", 0, 0, 0, "clean"},
		{"unstaged only", 0, 3, 0, "~3"},
		{"staged only", 2, 0, 0, "+2"},
		{"untracked only", 0, 0, 5, "?5"},
		{"unstaged + staged", 1, 3, 0, "~3 +1"},
		{"all three", 1, 2, 3, "~2 +1 ?3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatWorktreeStatus(tt.staged, tt.unstaged, tt.untracked)
			if got != tt.want {
				t.Errorf("FormatWorktreeStatus(%d, %d, %d) = %q, want %q",
					tt.staged, tt.unstaged, tt.untracked, got, tt.want)
			}
		})
	}
}

func TestFormatMergeStatus(t *testing.T) {
	os.Setenv("NO_COLOR", "1")
	defer os.Unsetenv("NO_COLOR")

	if got := FormatMergeStatus(true); got != "merged" {
		t.Errorf("FormatMergeStatus(true) = %q, want %q", got, "merged")
	}
	if got := FormatMergeStatus(false); got != "" {
		t.Errorf("FormatMergeStatus(false) = %q, want %q", got, "")
	}
}

func TestFormatDiff(t *testing.T) {
	os.Setenv("NO_COLOR", "1")
	defer os.Unsetenv("NO_COLOR")

	if got := FormatDiff(0, 0); got != "-" {
		t.Errorf("FormatDiff(0, 0) = %q, want %q", got, "-")
	}
	if got := FormatDiff(10, 3); got != "+10/-3" {
		t.Errorf("FormatDiff(10, 3) = %q, want %q", got, "+10/-3")
	}
}
