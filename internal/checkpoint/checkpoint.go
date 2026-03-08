// Package checkpoint manages context preservation across agent pauses and
// respawns, ensuring no work context is lost.
package checkpoint

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"text/template"

	"github.com/brianho/amux/internal/store"
	"github.com/brianho/amux/internal/workspace"
)

// Manager handles creating, storing, and rendering checkpoints.
type Manager struct {
	store store.Store
}

// NewManager creates a new checkpoint Manager.
func NewManager(s store.Store) *Manager {
	return &Manager{store: s}
}

// CreateImplicit builds an implicit checkpoint from the workspace's current
// state and the git state on disk. It captures diff summary, commit log, and
// blocker reason without requiring agent cooperation.
func (m *Manager) CreateImplicit(ws *workspace.Workspace) (*workspace.Checkpoint, error) {
	cp := &workspace.Checkpoint{
		BlockerReason: ws.Error,
	}

	// Capture git diff --stat
	diffStat, err := gitCommand(ws.WorktreePath, "diff", "--stat", ws.BaseBranch+"...HEAD")
	if err == nil {
		cp.DiffSummary = strings.TrimSpace(diffStat)
	}

	// Capture git log --oneline
	logOutput, err := gitCommand(ws.WorktreePath, "log", "--oneline", ws.BaseBranch+"..HEAD")
	if err == nil {
		lines := strings.Split(strings.TrimSpace(logOutput), "\n")
		for _, l := range lines {
			if l != "" {
				cp.CommitsOnBranch = append(cp.CommitsOnBranch, l)
			}
		}
	}

	// Capture modified files from diff --name-only
	nameOnly, err := gitCommand(ws.WorktreePath, "diff", "--name-only", ws.BaseBranch+"...HEAD")
	if err == nil {
		for _, f := range strings.Split(strings.TrimSpace(nameOnly), "\n") {
			if f != "" {
				cp.FilesModified = append(cp.FilesModified, f)
			}
		}
	}

	return cp, nil
}

// StoreExplicit persists an agent-written explicit checkpoint into the
// workspace record via the store. The explicit fields (ProgressSummary,
// RemainingWork, KeyDecisions, OpenQuestions) are merged onto the existing
// checkpoint if one exists.
func (m *Manager) StoreExplicit(repoRoot, workspaceID string, explicit workspace.Checkpoint) error {
	ws, err := m.store.GetWorkspace(repoRoot, workspaceID)
	if err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}

	// Merge explicit fields onto existing checkpoint data.
	existing := workspace.Checkpoint{}
	if ws.Checkpoint != nil {
		if err := json.Unmarshal(ws.Checkpoint, &existing); err != nil {
			// If we can't parse existing, start fresh.
			existing = workspace.Checkpoint{}
		}
	}

	if explicit.ProgressSummary != "" {
		existing.ProgressSummary = explicit.ProgressSummary
	}
	if explicit.RemainingWork != "" {
		existing.RemainingWork = explicit.RemainingWork
	}
	if len(explicit.KeyDecisions) > 0 {
		existing.KeyDecisions = explicit.KeyDecisions
	}
	if len(explicit.OpenQuestions) > 0 {
		existing.OpenQuestions = explicit.OpenQuestions
	}

	data, err := json.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	ws.Checkpoint = data

	return m.store.SaveWorkspace(ws)
}

// respawnTemplate is the Markdown template injected as an agent's opening context.
var respawnTemplate = template.Must(template.New("respawn").Parse(`You are continuing work on a task that was paused.

## Original Task
{{ .Task }}

## What Was Already Done
{{ if .DiffSummary }}### Diff Summary
` + "```" + `
{{ .DiffSummary }}
` + "```" + `
{{ end }}{{ if .Commits }}### Commits
{{ range .Commits }}- {{ . }}
{{ end }}{{ end }}
## Why It Paused
{{ .BlockerReason }}

## Human's Decision
{{ .Resolution }}

{{ if .AgentNotes }}## Previous Agent's Notes
{{ .AgentNotes }}
{{ end }}## Instructions
Continue from where the previous session left off.
Do NOT redo work already committed on this branch.
`))

// respawnData holds the values passed into the respawn prompt template.
type respawnData struct {
	Task          string
	DiffSummary   string
	Commits       []string
	BlockerReason string
	Resolution    string
	AgentNotes    string
}

// BuildRespawnPrompt renders a Markdown prompt that gives a new agent full
// context about what happened before the pause.
func (m *Manager) BuildRespawnPrompt(ws *workspace.Workspace, resolution string) (string, error) {
	cp := workspace.Checkpoint{}
	if ws.Checkpoint != nil {
		cpBytes, err := json.Marshal(ws.Checkpoint)
		if err == nil {
			_ = json.Unmarshal(cpBytes, &cp)
		}
	}

	agentNotes := buildAgentNotes(cp)

	task := ws.Source.Value
	if task == "" {
		task = ws.ID
	}

	data := respawnData{
		Task:          task,
		DiffSummary:   cp.DiffSummary,
		Commits:       cp.CommitsOnBranch,
		BlockerReason: cp.BlockerReason,
		Resolution:    resolution,
		AgentNotes:    agentNotes,
	}

	var buf strings.Builder
	if err := respawnTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render respawn template: %w", err)
	}
	return buf.String(), nil
}

// buildAgentNotes formats the explicit checkpoint fields into a readable block.
func buildAgentNotes(cp workspace.Checkpoint) string {
	var parts []string
	if cp.ProgressSummary != "" {
		parts = append(parts, "**Progress:** "+cp.ProgressSummary)
	}
	if cp.RemainingWork != "" {
		parts = append(parts, "**Remaining:** "+cp.RemainingWork)
	}
	if len(cp.KeyDecisions) > 0 {
		parts = append(parts, "**Key decisions:**")
		for _, d := range cp.KeyDecisions {
			parts = append(parts, "- "+d)
		}
	}
	if len(cp.OpenQuestions) > 0 {
		parts = append(parts, "**Open questions:**")
		for _, q := range cp.OpenQuestions {
			parts = append(parts, "- "+q)
		}
	}
	return strings.Join(parts, "\n")
}

// gitCommand runs a git command in the given directory and returns stdout.
// Exported for testing via variable replacement.
var gitCommand = func(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
