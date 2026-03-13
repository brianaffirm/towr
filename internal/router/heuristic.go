package router

import (
	"regexp"
	"strings"
)

var archKeywords = []string{"architect", "refactor", "migration"}
var fileRefPattern = regexp.MustCompile(`\b[\w-]+/[\w./-]+\.\w+\b`)

func heuristic(prompt string) Decision {
	words := len(strings.Fields(prompt))
	files := countFileReferences(prompt)
	hasArch := matchesAny(prompt, archKeywords)

	switch {
	case words < 100 && files <= 1 && !hasArch:
		return Decision{Model: "haiku", Tier: 0, Reason: "heuristic:simple", CanEscalate: true}
	case files <= 3 && !hasArch:
		return Decision{Model: "sonnet", Tier: 1, Reason: "heuristic:standard", CanEscalate: true}
	default:
		return Decision{Model: "opus", Tier: 2, Reason: "heuristic:complex", CanEscalate: false}
	}
}

func countFileReferences(prompt string) int {
	return len(fileRefPattern.FindAllString(prompt, -1))
}

func matchesAny(prompt string, keywords []string) bool {
	lower := strings.ToLower(prompt)
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
