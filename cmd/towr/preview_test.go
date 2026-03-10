package main

import (
	"testing"
)

func TestBuildPreviewHeader(t *testing.T) {
	tests := []struct {
		name   string
		wsID   string
		isDiff bool
		args   []string
		want   string
	}{
		{
			name:   "file preview",
			wsID:   "auth",
			isDiff: false,
			args:   []string{"src/handler.go"},
			want:   "── auth │ src/handler.go ",
		},
		{
			name:   "workspace diff",
			wsID:   "billing",
			isDiff: true,
			args:   nil,
			want:   "── billing │ workspace diff ",
		},
		{
			name:   "file diff",
			wsID:   "api",
			isDiff: true,
			args:   []string{"main.go"},
			want:   "── api │ main.go ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildPreviewHeader(tt.wsID, tt.isDiff, tt.args)
			// Check that it starts with expected prefix.
			if len(result) < len(tt.want) || result[:len(tt.want)] != tt.want {
				t.Errorf("buildPreviewHeader(%q, %v, %v) = %q, want prefix %q", tt.wsID, tt.isDiff, tt.args, result, tt.want)
			}
			// Check it's padded with ─ characters.
			suffix := result[len(tt.want):]
			for _, r := range suffix {
				if r != '─' {
					t.Errorf("expected padding char '─', got %q in suffix %q", r, suffix)
					break
				}
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "'simple'"},
		{"with spaces", "'with spaces'"},
		{"it's", "'it'\\''s'"},
		{"", "''"},
	}

	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
