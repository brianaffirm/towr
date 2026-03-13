package cost

const Version = "2026-03"

type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// Pricing reflects current model pricing as of March 2026.
//
// Claude (Anthropic) — https://docs.anthropic.com/en/docs/about-claude/models
//   Opus 4.6:   $5/M input,    $25/M output
//   Sonnet 4.6: $3/M input,    $15/M output
//   Haiku 4.5:  $1/M input,     $5/M output
//
// Codex (OpenAI) — https://developers.openai.com/codex/pricing/
//   codex-mini:    $0.25/M input,  $2/M output
//   gpt-5.3-codex: $1.75/M input, $14/M output
//   gpt-5.4:       $2.50/M input, $15/M output
//
// Cursor — https://cursor.com/docs/models (includes ~20% Cursor markup)
//   cursor-auto:   $1.25/M input,  $6/M output
//   cursor-sonnet: $3.60/M input, $18/M output
var Pricing = map[string]ModelPricing{
	"opus":          {5.00, 25.00},
	"sonnet":        {3.00, 15.00},
	"haiku":         {1.00, 5.00},
	"codex-mini":    {0.25, 2.00},
	"gpt-5.3-codex": {1.75, 14.00},
	"gpt-5.4":       {2.50, 15.00},
	"cursor-auto":   {1.25, 6.00},
	"cursor-sonnet": {3.60, 18.00},
}

func Calculate(model string, usage TokenUsage) float64 {
	p, ok := Pricing[model]
	if !ok {
		return 0
	}
	inputCost := float64(usage.InputTokens) / 1_000_000 * p.InputPerMillion
	outputCost := float64(usage.OutputTokens) / 1_000_000 * p.OutputPerMillion
	return inputCost + outputCost
}
