package cost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/router"
)

func TestCalculate(t *testing.T) {
	tests := []struct {
		model string
		usage TokenUsage
		want  float64
	}{
		{"opus", TokenUsage{InputTokens: 10000, OutputTokens: 30000}, 2.40},
		{"sonnet", TokenUsage{InputTokens: 10000, OutputTokens: 30000}, 0.48},
		{"haiku", TokenUsage{InputTokens: 10000, OutputTokens: 30000}, 0.04},
		{"opus", TokenUsage{InputTokens: 0, OutputTokens: 0}, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := Calculate(tt.model, tt.usage)
			if diff := got - tt.want; diff > 0.01 || diff < -0.01 {
				t.Errorf("Calculate(%q, %+v) = %.4f, want %.4f", tt.model, tt.usage, got, tt.want)
			}
		})
	}
}

func TestCalculate_UnknownModel(t *testing.T) {
	got := Calculate("gpt-4", TokenUsage{InputTokens: 1000, OutputTokens: 1000})
	if got != 0 {
		t.Errorf("unknown model should return 0, got %f", got)
	}
}

func TestParseClaudeTokens(t *testing.T) {
	dir := t.TempDir()
	old := dispatch.GetProjectsDirOverride()
	dispatch.SetProjectsDirOverride(dir)
	t.Cleanup(func() { dispatch.SetProjectsDirOverride(old) })

	worktreePath := "/Users/test/.towr/worktrees/towr/costtest"
	encoded := dispatch.ClaudeProjectDir(worktreePath)
	projDir := filepath.Join(dir, encoded)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("result entry with usage", func(t *testing.T) {
		jsonlFile := filepath.Join(projDir, "session.jsonl")
		content := "{\"type\":\"user\",\"timestamp\":\"2026-03-13T00:00:00Z\"}\n{\"type\":\"result\",\"result\":{\"usage\":{\"input_tokens\":12450,\"output_tokens\":38200}}}\n"
		if err := os.WriteFile(jsonlFile, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		usage, err := ParseClaudeTokens(worktreePath)
		if err != nil {
			t.Fatalf("ParseClaudeTokens: %v", err)
		}
		if usage.InputTokens != 12450 {
			t.Errorf("input = %d, want 12450", usage.InputTokens)
		}
		if usage.OutputTokens != 38200 {
			t.Errorf("output = %d, want 38200", usage.OutputTokens)
		}
		if usage.Source != "jsonl-parsed" {
			t.Errorf("source = %q, want jsonl-parsed", usage.Source)
		}
	})

	t.Run("no result entry returns unavailable", func(t *testing.T) {
		// Separate worktree path to avoid mtime race
		wt2 := "/Users/test/.towr/worktrees/towr/costtest2"
		enc2 := dispatch.ClaudeProjectDir(wt2)
		pd2 := filepath.Join(dir, enc2)
		if err := os.MkdirAll(pd2, 0o755); err != nil {
			t.Fatal(err)
		}
		jf := filepath.Join(pd2, "session.jsonl")
		content := "{\"type\":\"user\"}\n{\"type\":\"last-prompt\",\"lastPrompt\":\"hello\"}\n"
		if err := os.WriteFile(jf, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}

		usage, err := ParseClaudeTokens(wt2)
		if err != nil {
			t.Fatalf("ParseClaudeTokens: %v", err)
		}
		if usage.Source != "unavailable" {
			t.Errorf("source = %q, want unavailable", usage.Source)
		}
	})
}

func TestEstimateTokens(t *testing.T) {
	usage := EstimateTokens("Write a simple function that adds two numbers")
	if usage.InputTokens == 0 {
		t.Error("estimated input should be > 0")
	}
	if usage.Source != "estimated" {
		t.Errorf("source = %q, want estimated", usage.Source)
	}
}

func TestFormatPreRun(t *testing.T) {
	items := []PreRunItem{
		{TaskID: "auth", Decision: router.Decision{Model: "opus", Reason: "policy:infrastructure/**"}, EstCost: 2.80},
		{TaskID: "api", Decision: router.Decision{Model: "sonnet", Reason: "heuristic:standard"}, EstCost: 0.12},
	}
	out := FormatPreRun("my-sprint", items)
	if !strings.Contains(out, "my-sprint") {
		t.Error("should contain plan name")
	}
	if !strings.Contains(out, "opus") {
		t.Error("should contain model")
	}
	if !strings.Contains(out, "Savings:") {
		t.Error("should contain savings line")
	}
}

func TestFormatPostRun(t *testing.T) {
	items := []PostRunItem{
		{TaskID: "auth", Model: "opus", Usage: TokenUsage{InputTokens: 12450, OutputTokens: 38200, Source: "jsonl-parsed"}, ActualCost: 3.80, OpusCost: 3.80},
		{TaskID: "api", Model: "sonnet", Usage: TokenUsage{InputTokens: 8200, OutputTokens: 21100, Source: "jsonl-parsed"}, ActualCost: 0.34, OpusCost: 3.32},
	}
	out := FormatPostRun(items, 3, 45*time.Second)
	if !strings.Contains(out, "Total:") {
		t.Error("should contain total")
	}
	if !strings.Contains(out, "Saved:") {
		t.Error("should contain saved")
	}
	if !strings.Contains(out, "45s") {
		t.Error("should contain duration")
	}
}

func TestFmtTokens(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{500, "500"},
		{1000, "1,000"},
		{12450, "12,450"},
		{0, "0"},
	}
	for _, tt := range tests {
		got := fmtTokens(tt.input)
		if got != tt.want {
			t.Errorf("fmtTokens(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{2*time.Minute + 30*time.Second, "2m30s"},
		{12*time.Minute + 34*time.Second, "12m34s"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
