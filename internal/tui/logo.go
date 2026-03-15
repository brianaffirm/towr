package tui

import (
	_ "embed"
	"strings"
)

//go:embed logo.txt
var logoRaw string

// scaleLogo returns a scaled-down, centered version of the logo that fits
// within maxW columns. Samples every 3rd row to reduce height to ~1/3.
func scaleLogo(maxW int) string {
	lines := strings.Split(strings.TrimRight(logoRaw, "\n"), "\n")

	// Find the first and last non-empty lines so we skip blank margins.
	first, last := 0, len(lines)-1
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	for last > first && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	lines = lines[first : last+1]

	// First pass: collect sampled lines and find max content width.
	var sampled []string
	maxContent := 0
	for i, line := range lines {
		if i%3 != 0 {
			continue
		}
		stripped := strings.TrimSpace(line)
		runes := []rune(stripped)
		if len(runes) > maxW {
			stripped = string(runes[:maxW])
			runes = []rune(stripped)
		}
		sampled = append(sampled, stripped)
		if len(runes) > maxContent {
			maxContent = len(runes)
		}
	}

	// Second pass: center each line within maxW.
	var out []string
	for _, line := range sampled {
		pad := (maxW - maxContent) / 2
		if pad < 0 {
			pad = 0
		}
		out = append(out, strings.Repeat(" ", pad)+line)
	}
	return strings.Join(out, "\n")
}
