package git

import "testing"

func TestBuildPRURL(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		base    string
		branch  string
		wantURL string
		wantOK  bool
	}{
		{
			name:    "https remote",
			remote:  "https://github.com/user/repo.git",
			base:    "main",
			branch:  "amux/auth",
			wantURL: "https://github.com/user/repo/compare/main...amux/auth?expand=1",
			wantOK:  true,
		},
		{
			name:    "https remote no .git",
			remote:  "https://github.com/user/repo",
			base:    "main",
			branch:  "amux/auth",
			wantURL: "https://github.com/user/repo/compare/main...amux/auth?expand=1",
			wantOK:  true,
		},
		{
			name:    "ssh remote",
			remote:  "git@github.com:user/repo.git",
			base:    "develop",
			branch:  "amux/fix",
			wantURL: "https://github.com/user/repo/compare/develop...amux/fix?expand=1",
			wantOK:  true,
		},
		{
			name:   "non-github remote",
			remote: "https://gitlab.com/user/repo.git",
			base:   "main",
			branch: "amux/auth",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, ok := BuildPRURL(tt.remote, tt.base, tt.branch)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
		})
	}
}

func TestParseGitHubRemote(t *testing.T) {
	tests := []struct {
		url       string
		wantOwner string
		wantRepo  string
		wantOK    bool
	}{
		{"git@github.com:org/project.git", "org", "project", true},
		{"https://github.com/org/project.git", "org", "project", true},
		{"https://github.com/org/project", "org", "project", true},
		{"git@gitlab.com:org/project.git", "", "", false},
		{"https://bitbucket.org/org/project.git", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			owner, repo, ok := parseGitHubRemote(tt.url)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok {
				if owner != tt.wantOwner {
					t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
				}
				if repo != tt.wantRepo {
					t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
				}
			}
		})
	}
}
