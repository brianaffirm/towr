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
