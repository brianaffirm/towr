package dispatch

import (
	"strings"
	"testing"
)

func TestDetectPaneState(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  PaneState
	}{
		{"idle with prompt", "some output\n❯ \n", PaneIdle},
		{"working", "some output\nThinking...\n", PaneWorking},
		{"empty", "\n\n\n", PaneEmpty},
		{"idle after response", "I created the file.\n\n❯\n", PaneIdle},
		{"prompt with suggestion", "❯ Try something\n", PaneIdle},
		{"completely empty", "", PaneEmpty},
		{"working with trailing blanks", "Processing files...\n\n\n", PaneWorking},
		{"trust dialog is not idle", " ❯ 1. Yes, I trust this folder\n   2. No, exit\n\n Enter to confirm · Esc to cancel\n", PaneWorking},
		{"menu selection not idle", "❯ 1. Allow once\n", PaneWorking},
		{"real claude UI with status bar", "────────\n❯ \n────────\n  ? for shortcuts              Update available!\n\n\n\n", PaneIdle},
		{"permission dialog active", "❯ Create a file\n\n Write(hello.txt)\n Do you want to create hello.txt?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend\n", PaneWorking},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectPaneState(tt.input)
			if got != tt.want {
				t.Errorf("DetectPaneState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractLastResponse(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantContains string
		wantEmpty bool
	}{
		{
			name:         "two prompts with response",
			input:        "❯ first prompt\nfirst response\n❯ second prompt\nsecond response line 1\nsecond response line 2\n❯\n",
			wantContains: "second response",
		},
		{
			name:         "single prompt",
			input:        "Welcome to Claude\nSome init text\n❯\n",
			wantContains: "Welcome to Claude",
		},
		{
			name:         "no prompts",
			input:        "just some text\nno prompts here\n",
			wantContains: "just some text",
		},
		{
			name:      "adjacent prompts",
			input:     "❯ first\n❯ second\n",
			wantEmpty: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractLastResponse(tt.input)
			if tt.wantEmpty {
				if got != "" {
					t.Errorf("expected empty, got %q", got)
				}
				return
			}
			if !strings.Contains(got, tt.wantContains) {
				t.Errorf("expected %q to contain %q", got, tt.wantContains)
			}
		})
	}
}
