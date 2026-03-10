package workspace

import "testing"

func TestWorkspaceStatus_IsValid(t *testing.T) {
	tests := []struct {
		status WorkspaceStatus
		valid  bool
	}{
		{StatusCreating, true},
		{StatusReady, true},
		{StatusRunning, true},
		{StatusPaused, true},
		{StatusIdle, true},
		{StatusValidating, true},
		{StatusLanding, true},
		{StatusLanded, true},
		{StatusArchived, true},
		{StatusBlocked, true},
		{StatusOrphaned, true},
		{"INVALID", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.status.IsValid(); got != tt.valid {
			t.Errorf("WorkspaceStatus(%q).IsValid() = %v, want %v", tt.status, got, tt.valid)
		}
	}
}

func TestBranchName(t *testing.T) {
	if got := BranchName("feat-auth"); got != "towr/feat-auth" {
		t.Errorf("BranchName(feat-auth) = %q, want towr/feat-auth", got)
	}
}
