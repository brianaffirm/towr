package agent

import "sort"

// registry maps agent names to their implementations.
var registry = map[string]Agent{
	"claude-code": &ClaudeCode{},
}

// Get returns the agent by name. Returns Default() if name is empty or not found.
func Get(name string) Agent {
	if name == "" {
		return Default()
	}
	if a, ok := registry[name]; ok {
		return a
	}
	return Default()
}

// GetWithOpts returns an agent with optional model and full-auto overrides.
// Constructs a new instance (not the registered singleton) so fields
// are set without shared-state mutation.
func GetWithOpts(model, agentName string, fullAuto bool) Agent {
	switch agentName {
	case "codex":
		return &CodexAgent{ModelFlag: model, FullAuto: fullAuto}
	case "cursor":
		return &CursorAgent{ModelFlag: model, FullAuto: fullAuto}
	default: // claude-code or empty
		if model != "" || fullAuto {
			return &ClaudeCode{ModelFlag: model, FullAuto: fullAuto}
		}
		return Default()
	}
}

// GetWithModel returns an agent with an optional model override.
// Convenience wrapper around GetWithOpts with fullAuto=false.
func GetWithModel(model, agentName string) Agent {
	return GetWithOpts(model, agentName, false)
}

// Register adds an agent to the registry.
func Register(name string, a Agent) {
	registry[name] = a
}

// List returns all registered agent names in sorted order.
func List() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
