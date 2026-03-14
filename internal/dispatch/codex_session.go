package dispatch

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// codexSessionsDir is the base directory for Codex session JSONL files.
// Overridable in tests.
var codexSessionsDir = ""

// SetCodexSessionsDirOverride allows test packages to override the Codex sessions directory.
func SetCodexSessionsDirOverride(dir string) { codexSessionsDir = dir }

// GetCodexSessionsDirOverride returns the current override (empty = use default).
func GetCodexSessionsDirOverride() string { return codexSessionsDir }

func getCodexSessionsDir() string {
	if codexSessionsDir != "" {
		return codexSessionsDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}

// codexSessionMeta is the first-line metadata of a Codex JSONL session file.
type codexSessionMeta struct {
	Type    string `json:"type"`
	Payload struct {
		Cwd string `json:"cwd"`
	} `json:"payload"`
}

// FindCodexSession finds the Codex session JSONL file for the given worktree path.
// Matches by reading the session_meta first line and comparing payload.cwd.
func FindCodexSession(worktreePath string) (string, error) {
	base := getCodexSessionsDir()
	if base == "" {
		return "", fmt.Errorf("cannot determine Codex sessions directory")
	}

	// filepath.Glob doesn't recurse with **. Walk the directory.
	var matches []string
	_ = filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && filepath.Ext(path) == ".jsonl" {
			matches = append(matches, path)
		}
		return nil
	})

	if len(matches) == 0 {
		return "", fmt.Errorf("no Codex session files found in %s", base)
	}

	// Sort by modification time (newest first).
	sort.Slice(matches, func(i, j int) bool {
		si, _ := os.Stat(matches[i])
		sj, _ := os.Stat(matches[j])
		if si == nil || sj == nil {
			return false
		}
		return si.ModTime().After(sj.ModTime())
	})

	// Match by cwd in session_meta.
	for _, m := range matches {
		cwd, err := readCodexCwd(m)
		if err != nil {
			continue
		}
		if cwd == worktreePath {
			return m, nil
		}
	}

	return "", fmt.Errorf("no Codex session found for worktree %s", worktreePath)
}

// readCodexCwd reads the first line of a Codex JSONL file and extracts the cwd.
func readCodexCwd(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	if !scanner.Scan() {
		return "", fmt.Errorf("empty file")
	}

	var meta codexSessionMeta
	if err := json.Unmarshal([]byte(scanner.Text()), &meta); err != nil {
		return "", err
	}
	if meta.Type != "session_meta" {
		return "", fmt.Errorf("first line is not session_meta")
	}
	return meta.Payload.Cwd, nil
}
