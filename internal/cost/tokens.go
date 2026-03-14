package cost

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"

	"github.com/brianaffirm/towr/internal/dispatch"
)

type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	Source       string // "jsonl-parsed", "estimated", "unavailable"
}

type resultEntry struct {
	Type   string `json:"type"`
	Result *struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"result"`
}

func ParseClaudeTokens(worktreePath string) (TokenUsage, error) {
	jsonlPath, err := dispatch.FindLatestJSONL(worktreePath)
	if err != nil {
		return TokenUsage{Source: "unavailable"}, nil
	}

	f, err := os.Open(jsonlPath)
	if err != nil {
		return TokenUsage{Source: "unavailable"}, nil
	}
	defer f.Close()

	var totalIn, totalOut int
	found := false

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"result"`) {
			continue
		}
		var entry resultEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "result" && entry.Result != nil && entry.Result.Usage != nil {
			totalIn += entry.Result.Usage.InputTokens
			totalOut += entry.Result.Usage.OutputTokens
			found = true
		}
	}

	if !found {
		return TokenUsage{Source: "unavailable"}, nil
	}
	return TokenUsage{
		InputTokens:  totalIn,
		OutputTokens: totalOut,
		Source:       "jsonl-parsed",
	}, nil
}

// codexTokenCountEvent represents a Codex JSONL token_count event.
type codexTokenCountEvent struct {
	Type    string `json:"type"`
	Payload struct {
		Type string `json:"type"`
		Info struct {
			TotalTokenUsage struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"total_token_usage"`
		} `json:"info"`
	} `json:"payload"`
}

// ParseCodexTokens reads token usage from a Codex session JSONL file.
// Matches the session by worktree path (via cwd in session_meta).
// Takes the last token_count event (Codex totals are cumulative).
func ParseCodexTokens(worktreePath string) (TokenUsage, error) {
	sessionPath, err := dispatch.FindCodexSession(worktreePath)
	if err != nil {
		return TokenUsage{Source: "unavailable"}, nil
	}

	f, err := os.Open(sessionPath)
	if err != nil {
		return TokenUsage{Source: "unavailable"}, nil
	}
	defer f.Close()

	var lastIn, lastOut int
	found := false

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"token_count"`) {
			continue
		}
		var entry codexTokenCountEvent
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Payload.Type == "token_count" {
			lastIn = entry.Payload.Info.TotalTokenUsage.InputTokens
			lastOut = entry.Payload.Info.TotalTokenUsage.OutputTokens
			found = true
		}
	}

	if !found {
		return TokenUsage{Source: "unavailable"}, nil
	}
	return TokenUsage{
		InputTokens:  lastIn,
		OutputTokens: lastOut,
		Source:       "codex-jsonl",
	}, nil
}

func EstimateTokens(prompt string) TokenUsage {
	words := len(strings.Fields(prompt))
	inputTokens := int(float64(words) * 1.3)
	outputTokens := inputTokens * 3
	return TokenUsage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Source:       "estimated",
	}
}

func DefaultEstimate() TokenUsage {
	return TokenUsage{
		InputTokens:  10000,
		OutputTokens: 30000,
		Source:       "estimated",
	}
}
