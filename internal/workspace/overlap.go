package workspace

import (
	"sort"
	"strings"

	"github.com/brianho/amux/internal/git"
	"github.com/brianho/amux/internal/store"
)

// OverlapPair describes two workspaces editing the same files.
type OverlapPair struct {
	WorkspaceA string   `json:"workspace_a"`
	WorkspaceB string   `json:"workspace_b"`
	Files      []string `json:"files"`
}

// ModifiedFiles returns files changed in a workspace vs its base branch.
func ModifiedFiles(repoRoot, baseBranch, branch string) []string {
	if repoRoot == "" || baseBranch == "" || branch == "" {
		return nil
	}
	out, err := git.RunGit(repoRoot, "diff", "--name-only", baseBranch+"..."+branch)
	if err != nil || out == "" {
		return nil
	}
	var files []string
	for _, f := range strings.Split(strings.TrimSpace(out), "\n") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files
}

// DetectOverlaps finds workspaces editing the same files.
func DetectOverlaps(workspaces []*store.Workspace) []OverlapPair {
	// Build map: workspace → modified files.
	filesByWS := make(map[string]map[string]bool)
	for _, ws := range workspaces {
		if ws.RepoRoot == "" || ws.Status == "LANDED" || ws.Status == "ARCHIVED" {
			continue
		}
		files := ModifiedFiles(ws.RepoRoot, ws.BaseBranch, ws.Branch)
		if len(files) == 0 {
			continue
		}
		set := make(map[string]bool, len(files))
		for _, f := range files {
			set[f] = true
		}
		filesByWS[ws.ID] = set
	}

	// Compare all pairs.
	ids := make([]string, 0, len(filesByWS))
	for id := range filesByWS {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var overlaps []OverlapPair
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			var shared []string
			for f := range filesByWS[ids[i]] {
				if filesByWS[ids[j]][f] {
					shared = append(shared, f)
				}
			}
			if len(shared) > 0 {
				sort.Strings(shared)
				overlaps = append(overlaps, OverlapPair{ids[i], ids[j], shared})
			}
		}
	}
	return overlaps
}

// OverlapCount returns the number of files this workspace overlaps with any other.
func OverlapCount(wsID string, overlaps []OverlapPair) int {
	seen := make(map[string]bool)
	for _, o := range overlaps {
		if o.WorkspaceA == wsID || o.WorkspaceB == wsID {
			for _, f := range o.Files {
				seen[f] = true
			}
		}
	}
	return len(seen)
}
