package interruption

import (
	"path/filepath"
	"strings"
)

// PolicyRule defines a single permission rule that matches file path patterns.
type PolicyRule struct {
	Action   string   `json:"action"`   // "allow", "deny", "queue"
	Patterns []string `json:"patterns"` // glob patterns (e.g. "src/**", "db/**")
	Label    string   `json:"label"`    // human-readable rule name
}

// PolicyResult is the outcome of evaluating a blocker against the active policy.
type PolicyResult struct {
	Action      string `json:"action"`      // "allow", "deny", "queue"
	Rule        string `json:"rule"`        // which rule matched
	Enforcement string `json:"enforcement"` // "hard" or "soft"
}

// Preset is a named collection of policy rules and configuration.
type Preset struct {
	Name          string       `json:"name"`
	AutoApprove   []string     `json:"auto_approve"`
	AutoDeny      []string     `json:"auto_deny"`
	Queue         []string     `json:"queue"`
	AlwaysDeny    []string     `json:"always_deny"`
	AutoLand      bool         `json:"auto_land"`
	MaxDiffLines  int          `json:"max_diff_lines"`
	Timeout       string       `json:"timeout"`
	TimeoutAction string       `json:"timeout_action"`
}

// PolicyEngine evaluates blockers against the active set of rules.
type PolicyEngine struct {
	activePreset string
	presets      map[string]*Preset
}

// NewPolicyEngine creates a PolicyEngine pre-loaded with all built-in presets.
func NewPolicyEngine() *PolicyEngine {
	pe := &PolicyEngine{
		presets: make(map[string]*Preset),
	}
	for _, p := range BuiltinPresets() {
		pe.presets[p.Name] = p
	}
	pe.activePreset = "interactive" // default: everything manual
	return pe
}

// LoadPreset registers a preset by name. If it already exists, it is replaced.
func (pe *PolicyEngine) LoadPreset(preset *Preset) {
	pe.presets[preset.Name] = preset
}

// SetActive switches the active preset. Returns an error if the name is unknown.
func (pe *PolicyEngine) SetActive(name string) error {
	if _, ok := pe.presets[name]; !ok {
		return &UnknownPresetError{Name: name}
	}
	pe.activePreset = name
	return nil
}

// GetActive returns the name of the currently active preset.
func (pe *PolicyEngine) GetActive() string {
	return pe.activePreset
}

// GetPreset returns the preset with the given name, or nil.
func (pe *PolicyEngine) GetPreset(name string) *Preset {
	return pe.presets[name]
}

// Evaluate checks a blocker against the active preset's rules.
// Returns nil if no rule matches (caller should fall through to queue).
func (pe *PolicyEngine) Evaluate(blocker Blocker) (*PolicyResult, error) {
	preset, ok := pe.presets[pe.activePreset]
	if !ok {
		return nil, &UnknownPresetError{Name: pe.activePreset}
	}

	// Check always_deny first (command-level blocks).
	if matchesCommand(blocker.AgentRequest, preset.AlwaysDeny) {
		return &PolicyResult{
			Action:      "deny",
			Rule:        "always_deny: " + blocker.AgentRequest,
			Enforcement: "hard",
		}, nil
	}

	// Check file-path rules in order: auto_deny → queue → auto_approve.
	for _, f := range blocker.FilesAtStake {
		if matchesAnyPattern(f, preset.AutoDeny) {
			return &PolicyResult{
				Action:      "deny",
				Rule:        "auto_deny matched: " + f,
				Enforcement: "hard",
			}, nil
		}
	}

	for _, f := range blocker.FilesAtStake {
		if matchesAnyPattern(f, preset.Queue) {
			return &PolicyResult{
				Action:      "queue",
				Rule:        "queue matched: " + f,
				Enforcement: "soft",
			}, nil
		}
	}

	for _, f := range blocker.FilesAtStake {
		if matchesAnyPattern(f, preset.AutoApprove) {
			return &PolicyResult{
				Action:      "allow",
				Rule:        "auto_approve matched: " + f,
				Enforcement: "soft",
			}, nil
		}
	}

	// No rule matched — return nil to signal fallthrough.
	return nil, nil
}

// matchesAnyPattern checks whether path matches any of the given glob patterns.
// Supports filepath.Match patterns. For "**" (double-star) patterns, we also
// check each path segment prefix so that "src/**" matches "src/foo/bar.go".
func matchesAnyPattern(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchGlob(path, pattern) {
			return true
		}
	}
	return false
}

// matchGlob matches a single path against a glob pattern. It handles the
// common "dir/**" convention by checking if the path starts with the prefix.
func matchGlob(path, pattern string) bool {
	// Handle double-star: treat "dir/**" as prefix match.
	if strings.Contains(pattern, "**") {
		prefix := strings.SplitN(pattern, "**", 2)[0]
		// Normalize: "src/**" → prefix "src/"
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	// Try standard filepath.Match.
	matched, err := filepath.Match(pattern, path)
	if err != nil {
		return false
	}
	return matched
}

// matchesCommand checks if the agent request contains any of the denied commands.
func matchesCommand(request string, denied []string) bool {
	for _, cmd := range denied {
		if strings.Contains(request, cmd) {
			return true
		}
	}
	return false
}

// UnknownPresetError is returned when an unknown preset name is used.
type UnknownPresetError struct {
	Name string
}

func (e *UnknownPresetError) Error() string {
	return "unknown preset: " + e.Name
}
