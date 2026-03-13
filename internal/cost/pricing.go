package cost

const Version = "2026-03"

type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

var Pricing = map[string]ModelPricing{
	"opus":   {15.00, 75.00},
	"sonnet": {3.00, 15.00},
	"haiku":  {0.25, 1.25},
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
