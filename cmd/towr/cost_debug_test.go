package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/store"
)

func TestDebugCostEvents(t *testing.T) {
	dbPath := filepath.Join(config.TowrHome(), "global-state.db")
	s := store.NewSQLiteStore()
	if err := s.Init(dbPath); err != nil {
		t.Fatalf("open: %v", err)
	}

	// Query all events
	all, err := s.QueryEvents(store.EventQuery{})
	if err != nil {
		t.Fatalf("query all: %v", err)
	}
	t.Logf("Total events: %d", len(all))

	// Query cost events without repo filter
	costAll, err := s.QueryEvents(store.EventQuery{Kind: store.EventTaskCost})
	if err != nil {
		t.Fatalf("query cost: %v", err)
	}
	t.Logf("Cost events (no repo filter): %d", len(costAll))

	// Query cost events with repo filter
	costRepo, err := s.QueryEvents(store.EventQuery{Kind: store.EventTaskCost, RepoRoot: "/Users/brian.ho/w/towr"})
	if err != nil {
		t.Fatalf("query cost+repo: %v", err)
	}
	t.Logf("Cost events (with repo): %d", len(costRepo))

	for _, e := range costAll {
		fmt.Printf("  id=%s kind=%s repo=%q ws=%s model=%v\n", e.ID, e.Kind, e.RepoRoot, e.WorkspaceID, e.Data["model"])
	}
}
