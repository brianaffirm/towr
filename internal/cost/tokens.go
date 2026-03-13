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
