package main

import (
	"testing"
)

func TestIsInsideDir(t *testing.T) {
	tests := []struct {
		name   string
		child  string
		parent string
		want   bool
	}{
		{
			name:   "exact match",
			child:  "/home/user/project/wt",
			parent: "/home/user/project/wt",
			want:   true,
		},
		{
			name:   "child is subdirectory",
			child:  "/home/user/project/wt/src",
			parent: "/home/user/project/wt",
			want:   true,
		},
		{
			name:   "child is deeply nested",
			child:  "/home/user/project/wt/src/pkg/main.go",
			parent: "/home/user/project/wt",
			want:   true,
		},
		{
			name:   "child is outside parent",
			child:  "/home/user/other",
			parent: "/home/user/project/wt",
			want:   false,
		},
		{
			name:   "child is sibling with shared prefix",
			child:  "/home/user/project/wt-other",
			parent: "/home/user/project/wt",
			want:   false,
		},
		{
			name:   "parent is subdirectory of child",
			child:  "/home/user/project",
			parent: "/home/user/project/wt",
			want:   false,
		},
		{
			name:   "completely different paths",
			child:  "/tmp/foo",
			parent: "/home/user/project/wt",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isInsideDir(tt.child, tt.parent)
			if got != tt.want {
				t.Errorf("isInsideDir(%q, %q) = %v, want %v", tt.child, tt.parent, got, tt.want)
			}
		})
	}
}
