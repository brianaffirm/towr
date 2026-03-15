package tui

import (
	_ "embed"
	"strings"
)

//go:embed logo.txt
var logoRaw string

// logoMaxLines is the number of sampled lines to show — just the head of the
// tower, which is narrow enough to center properly in the control pane.
const logoMaxLines = 9

// scaleLogo returns a compact, centered version of the logo's head that fits
// within maxW columns. Takes every 2nd row from the first ~18 source lines.
func scaleLogo(maxW int) string {
	lines := strings.Split(strings.TrimRight(logoRaw, "\n"), "\n")

	// Find the first non-empty line to skip blank top margin.
	first := 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	lines = lines[first:]

	// Sample every 2nd line, stopping after logoMaxLines.
	var sampled []string
	maxContent := 0
	for i, line := range lines {
		if i%2 != 0 {
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
		if len(sampled) >= logoMaxLines {
			break
		}
	}

	// Center the block within maxW.
	pad := (maxW - maxContent) / 2
	if pad < 0 {
		pad = 0
	}
	padding := strings.Repeat(" ", pad)
	var out []string
	for _, line := range sampled {
		out = append(out, padding+line)
	}
	return strings.Join(out, "\n")
}
