package interruption

// BuiltinPresets returns the four built-in policy presets.
func BuiltinPresets() []*Preset {
	return []*Preset{
		OvernightPreset(),
		ConservativePreset(),
		AggressivePreset(),
		InteractivePreset(),
	}
}

// OvernightPreset auto-approves safe paths, queues risky ones, denies dangerous ops.
// Designed for unattended runs where the human will triage in the morning.
func OvernightPreset() *Preset {
	return &Preset{
		Name:        "overnight",
		AutoApprove: []string{"src/**", "tests/**", "docs/**"},
		AutoDeny:    []string{"infrastructure/**", ".github/**"},
		Queue:       []string{"db/**", "config/**"},
		AlwaysDeny:  []string{"rm -rf", "git push --force"},
		AutoLand:    true,
		MaxDiffLines: 1000,
		Timeout:      "6h",
		TimeoutAction: "block",
	}
}

// ConservativePreset queues everything for human review. Nothing is auto-decided.
func ConservativePreset() *Preset {
	return &Preset{
		Name:          "conservative",
		AutoApprove:   nil,
		AutoDeny:      nil,
		Queue:         []string{"**"},
		AlwaysDeny:    []string{"rm -rf", "git push --force"},
		AutoLand:      false,
		MaxDiffLines:  500,
		Timeout:       "12h",
		TimeoutAction: "block",
	}
}

// AggressivePreset auto-approves almost everything. Only always_deny rules block.
// Use when speed matters more than caution.
func AggressivePreset() *Preset {
	return &Preset{
		Name:          "aggressive",
		AutoApprove:   []string{"**"},
		AutoDeny:      nil,
		Queue:         nil,
		AlwaysDeny:    []string{"rm -rf", "git push --force"},
		AutoLand:      true,
		MaxDiffLines:  5000,
		Timeout:       "1h",
		TimeoutAction: "skip",
	}
}

// InteractivePreset makes no automatic decisions. Every blocker is queued.
// This is the default preset — the human is always in the loop.
func InteractivePreset() *Preset {
	return &Preset{
		Name:          "interactive",
		AutoApprove:   nil,
		AutoDeny:      nil,
		Queue:         nil,
		AlwaysDeny:    nil,
		AutoLand:      false,
		MaxDiffLines:  0,
		Timeout:       "0",
		TimeoutAction: "block",
	}
}
