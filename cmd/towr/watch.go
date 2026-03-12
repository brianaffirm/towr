package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/brianaffirm/towr/internal/agent"
	"github.com/brianaffirm/towr/internal/config"
	"github.com/brianaffirm/towr/internal/dispatch"
	"github.com/brianaffirm/towr/internal/store"
	"github.com/brianaffirm/towr/internal/terminal"
	"github.com/brianaffirm/towr/internal/workspace"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

func newWatchCmd(initApp func() (*appContext, error), jsonFlag *bool) *cobra.Command {
	var (
		intervalFlag    time.Duration
		autoApproveFlag bool
		allFlag         bool
		reactFlag       bool
	)

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Monitor all workspaces and react to state changes",
		Long:  "Continuously poll all active workspaces, detect state transitions (idle, blocked, completed), and react automatically. Replaces the manual towr wait + towr send --approve loop.",
		RunE: func(cmd *cobra.Command, args []string) error {
			app, appErr := initApp()

			if appErr != nil || allFlag {
				// All-repos mode: no single app context needed.
				return runWatchAllRepos(intervalFlag, autoApproveFlag, reactFlag, jsonFlag)
			}

			return runWatch(app, intervalFlag, autoApproveFlag, reactFlag, jsonFlag)
		},
	}

	cmd.Flags().DurationVar(&intervalFlag, "interval", 10*time.Second, "poll interval (e.g. 5s, 30s)")
	cmd.Flags().BoolVar(&autoApproveFlag, "auto-approve", false, "automatically approve permission dialogs")
	cmd.Flags().BoolVar(&allFlag, "all", false, "monitor workspaces across all repos")
	cmd.Flags().BoolVar(&reactFlag, "react", false, "monitor PRs and auto-react to CI failures and review feedback")

	return cmd
}

// watchState tracks per-workspace monitoring state.
type watchState struct {
	prevState   dispatch.PaneState
	sawWorking  bool
	dispatchID  string
	idleSince   time.Time // when workspace first entered idle (for stale-idle warning)
	warnedIdle  bool      // whether we already warned about prolonged idle
	finalStatus string    // for exit summary: "completed", "working", "blocked", etc.
}

// prState tracks per-PR monitoring state to avoid re-triggering reactions.
type prState struct {
	number         int
	lastReview     string // "APPROVED", "CHANGES_REQUESTED", ""
	lastCI         string // "SUCCESS", "FAILURE", "PENDING"
	reactedReview  bool   // already reacted to this review state
	reactedCI      bool   // already reacted to this CI state
	workspaceID    string // mapped from branch name
	ciRetries      int    // count of CI fix re-dispatches
	reviewRetries  int    // count of review fix re-dispatches
	lastCommentCount int  // track comment count to detect new comments
	commentRetries int    // count of comment reply re-dispatches
}

const maxReactRetries = 10

// towrReplySignature is prepended to all towr-generated PR comments
// so humans can distinguish them from manual comments, and watch can
// filter them out to avoid infinite reaction loops.
const towrReplySignature = "🤖 *Reply from towr*"

// repoStoreCache caches open stores per repo root for all-repos mode.
type repoStoreCache struct {
	mu     sync.Mutex
	stores map[string]*store.SQLiteStore
	terms  map[string]terminal.Backend
}

func newRepoStoreCache() *repoStoreCache {
	return &repoStoreCache{
		stores: make(map[string]*store.SQLiteStore),
		terms:  make(map[string]terminal.Backend),
	}
}

func (c *repoStoreCache) getStore(repoRoot string) (*store.SQLiteStore, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if s, ok := c.stores[repoRoot]; ok {
		return s, nil
	}

	var dbPath string
	if repoRoot == "" {
		dbPath = filepath.Join(config.TowrHome(), "global-state.db")
	} else {
		repoState := config.RepoStatePath(repoRoot)
		dbPath = filepath.Join(repoState, "state.db")
	}

	s := store.NewSQLiteStore()
	if err := s.Init(dbPath); err != nil {
		return nil, fmt.Errorf("open store for %s: %w", repoRoot, err)
	}
	c.stores[repoRoot] = s
	return s, nil
}

func (c *repoStoreCache) getTerm(_ string) terminal.Backend {
	c.mu.Lock()
	defer c.mu.Unlock()

	// All repos share the same tmux backend.
	if t, ok := c.terms["default"]; ok {
		return t
	}

	var term terminal.Backend
	if _, err := exec.LookPath("tmux"); err != nil {
		term = terminal.NewHeadlessBackend()
	} else {
		term = terminal.NewTmuxBackend("towr")
	}
	c.terms["default"] = term
	return term
}

func (c *repoStoreCache) closeAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.stores {
		_ = s.Close()
	}
}

// ---------- Single-repo watch (existing behavior + react) ----------

func runWatch(app *appContext, interval time.Duration, autoApprove, react bool, jsonFlag *bool) error {
	if react {
		if err := checkGHCLI(); err != nil {
			return err
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	states := make(map[string]*watchState)
	prStates := make(map[int]*prState)

	now := time.Now()
	workspaces := countActiveWorkspaces(app)
	approveStr := "off"
	if autoApprove {
		approveStr = "on"
	}

	if *jsonFlag {
		emitJSON(map[string]interface{}{
			"time":         formatTime(now),
			"event":        "started",
			"workspaces":   workspaces,
			"interval":     interval.String(),
			"auto_approve": autoApprove,
			"react":        react,
		})
	} else {
		reactStr := ""
		if react {
			reactStr = ", react: on"
		}
		fmt.Printf("[%s] Watching %d workspaces (poll: %s, auto-approve: %s%s)\n",
			formatTime(now), workspaces, interval, approveStr, reactStr)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// PR polling uses a longer interval (3x workspace polling).
	var prTicker *time.Ticker
	if react {
		prInterval := interval * 3
		if prInterval < 30*time.Second {
			prInterval = 30 * time.Second
		}
		prTicker = time.NewTicker(prInterval)
		defer prTicker.Stop()
	}

	for {
		if prTicker != nil {
			select {
			case <-sigCh:
				printSummary(app, states, jsonFlag)
				return nil
			case <-ticker.C:
				pollWorkspaces(app, states, autoApprove, jsonFlag)
			case <-prTicker.C:
				pollPRsSingleRepo(app, prStates, states, jsonFlag)
			}
		} else {
			select {
			case <-sigCh:
				printSummary(app, states, jsonFlag)
				return nil
			case <-ticker.C:
				pollWorkspaces(app, states, autoApprove, jsonFlag)
			}
		}
	}
}

// ---------- All-repos watch ----------

func runWatchAllRepos(interval time.Duration, autoApprove, react bool, jsonFlag *bool) error {
	if react {
		if err := checkGHCLI(); err != nil {
			return err
		}
	}

	if err := config.EnsureTowrDirs(); err != nil {
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	cache := newRepoStoreCache()
	defer cache.closeAll()

	states := make(map[string]*watchState)
	prStates := make(map[int]*prState)

	// Count active workspaces across all repos.
	reposDir := filepath.Join(config.TowrHome(), "repos")
	allWS, _ := store.ListAllWorkspaces(reposDir)
	activeCount := 0
	for _, ws := range allWS {
		status := workspace.WorkspaceStatus(ws.Status)
		if status == workspace.StatusRunning || status == workspace.StatusIdle {
			activeCount++
		}
	}

	now := time.Now()
	approveStr := "off"
	if autoApprove {
		approveStr = "on"
	}

	if *jsonFlag {
		emitJSON(map[string]interface{}{
			"time":         formatTime(now),
			"event":        "started",
			"mode":         "all-repos",
			"workspaces":   activeCount,
			"interval":     interval.String(),
			"auto_approve": autoApprove,
			"react":        react,
		})
	} else {
		reactStr := ""
		if react {
			reactStr = ", react: on"
		}
		fmt.Printf("[%s] Watching %d workspaces across all repos (poll: %s, auto-approve: %s%s)\n",
			formatTime(now), activeCount, interval, approveStr, reactStr)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var prTicker *time.Ticker
	if react {
		prInterval := interval * 3
		if prInterval < 30*time.Second {
			prInterval = 30 * time.Second
		}
		prTicker = time.NewTicker(prInterval)
		defer prTicker.Stop()
	}

	for {
		if prTicker != nil {
			select {
			case <-sigCh:
				printSummaryAllRepos(states, jsonFlag)
				return nil
			case <-ticker.C:
				pollWorkspacesAllRepos(cache, states, autoApprove, jsonFlag)
			case <-prTicker.C:
				pollPRsAllRepos(cache, prStates, states, jsonFlag)
			}
		} else {
			select {
			case <-sigCh:
				printSummaryAllRepos(states, jsonFlag)
				return nil
			case <-ticker.C:
				pollWorkspacesAllRepos(cache, states, autoApprove, jsonFlag)
			}
		}
	}
}

func pollWorkspacesAllRepos(cache *repoStoreCache, states map[string]*watchState, autoApprove bool, jsonFlag *bool) {
	reposDir := filepath.Join(config.TowrHome(), "repos")
	allWS, err := store.ListAllWorkspaces(reposDir)
	if err != nil {
		return
	}

	activeCount := 0
	for _, ws := range allWS {
		status := workspace.WorkspaceStatus(ws.Status)
		if status != workspace.StatusRunning && status != workspace.StatusIdle {
			continue
		}

		s, err := cache.getStore(ws.RepoRoot)
		if err != nil {
			continue
		}
		term := cache.getTerm(ws.RepoRoot)

		latestDisp, err := s.LatestDispatch(ws.RepoRoot, ws.ID)
		if err != nil || latestDisp == nil {
			continue
		}
		dispID, _ := latestDisp.Data["dispatch_id"].(string)
		if dispID == "" {
			continue
		}

		latestEvt, err := s.LatestTaskEvent(ws.RepoRoot, ws.ID, dispID)
		if err != nil {
			continue
		}
		if latestEvt != nil && (latestEvt.Kind == store.EventTaskCompleted || latestEvt.Kind == store.EventTaskFailed) {
			if st, ok := states[ws.ID]; ok && st.finalStatus == "" {
				st.finalStatus = "completed"
			}
			continue
		}

		activeCount++

		if _, ok := states[ws.ID]; !ok {
			states[ws.ID] = &watchState{dispatchID: dispID}
		}
		st := states[ws.ID]
		st.dispatchID = dispID

		// Build a temporary appContext for this workspace.
		tmpApp := &appContext{
			repoRoot: ws.RepoRoot,
			store:    s,
			term:     term,
		}

		var currentState dispatch.PaneState
		var jsonlSummary string
		usedJSONL := false

		if ws.WorktreePath != "" {
			jState, jSummary, jErr := dispatch.DetectClaudeActivity(ws.WorktreePath)
			if jErr == nil && jState != dispatch.PaneEmpty {
				// JSONL gave a definitive answer (working, idle, blocked).
				currentState = jState
				jsonlSummary = jSummary
				usedJSONL = true
			}
			// If JSONL returned PaneEmpty, it's inconclusive — fall through to capture-pane.
			if jErr == nil && jState == dispatch.PaneEmpty {
				jsonlSummary = jSummary // keep the summary even if state is inconclusive
			}
		}

		// Look up the agent for this workspace to use correct dialog/idle patterns.
		ag := agent.Get(ws.AgentRuntime)

		captured, captErr := term.CapturePane(ws.ID, 200)
		if captErr == nil {
			lastActivity := term.PaneLastActivity(ws.ID)
			capState := dispatch.DetectPaneStateWithPatterns(captured, ag.DialogIndicators(), ag.IdlePattern(), lastActivity, 15*time.Second)
			if capState == dispatch.PaneBlocked {
				currentState = dispatch.PaneBlocked
			}
			if !usedJSONL {
				currentState = capState
			}
		} else if !usedJSONL {
			alive, aliveErr := term.IsPaneAlive(ws.ID)
			if aliveErr != nil || !alive {
				handleTransition(tmpApp, ws, st, dispatch.PaneEmpty, "", captured, autoApprove, jsonFlag)
			}
			continue
		}

		if currentState == dispatch.PaneWorking || currentState == dispatch.PaneBlocked {
			st.sawWorking = true
		}

		// Always handle blocked state (new dialog may appear even if state didn't change).
		if currentState == dispatch.PaneBlocked || currentState != st.prevState {
			handleTransition(tmpApp, ws, st, currentState, jsonlSummary, captured, autoApprove, jsonFlag)
			st.prevState = currentState
		}

		if currentState == dispatch.PaneIdle && st.sawWorking {
			if st.idleSince.IsZero() {
				st.idleSince = time.Now()
			} else if time.Since(st.idleSince) > 5*time.Minute && !st.warnedIdle {
				st.warnedIdle = true
				now := time.Now()
				if *jsonFlag {
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"workspace": ws.ID,
						"event":     "idle_warning",
						"duration":  time.Since(st.idleSince).String(),
					})
				} else {
					fmt.Printf("[%s] \u23f3 %s: idle for >5min\n", formatTime(now), ws.ID)
				}
			}
		} else {
			st.idleSince = time.Time{}
			st.warnedIdle = false
		}
	}

	if activeCount == 0 && len(states) > 0 {
		anyActive := false
		for _, st := range states {
			if st.sawWorking {
				anyActive = true
				break
			}
		}
		if anyActive {
			now := time.Now()
			if *jsonFlag {
				emitJSON(map[string]interface{}{
					"time":  formatTime(now),
					"event": "all_idle",
				})
			} else {
				fmt.Printf("[%s] All workspaces idle. Watching for new dispatches...\n", formatTime(now))
			}
		}
	}
}

func printSummaryAllRepos(states map[string]*watchState, jsonFlag *bool) {
	if len(states) == 0 {
		return
	}

	if *jsonFlag {
		summaries := make([]map[string]interface{}, 0, len(states))
		for wsID, st := range states {
			status := st.finalStatus
			if status == "" {
				status = "unknown"
			}
			summaries = append(summaries, map[string]interface{}{
				"workspace":   wsID,
				"dispatch_id": st.dispatchID,
				"status":      status,
			})
		}
		emitJSON(map[string]interface{}{
			"event":      "stopped",
			"time":       formatTime(time.Now()),
			"workspaces": summaries,
		})
		return
	}

	fmt.Println("\nStopped watching. Summary:")
	for wsID, st := range states {
		icon := "\u25b6"
		status := "still working"
		switch st.finalStatus {
		case "completed":
			icon = "\u2713"
			status = "completed"
		case "blocked":
			icon = "\u26a0"
			status = "blocked"
		case "exited":
			icon = "\u26a0"
			status = "exited"
		}
		dispID := st.dispatchID
		if dispID == "" {
			dispID = "---"
		}
		fmt.Printf("  %s: %s %s %s\n", wsID, dispID, icon, status)
	}
}

// ---------- Original single-repo polling (unchanged) ----------

func pollWorkspaces(app *appContext, states map[string]*watchState, autoApprove bool, jsonFlag *bool) {
	allWS, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
	if err != nil {
		return
	}

	activeCount := 0
	for _, ws := range allWS {
		status := workspace.WorkspaceStatus(ws.Status)
		if status != workspace.StatusRunning && status != workspace.StatusIdle {
			continue
		}

		latestDisp, err := app.store.LatestDispatch(app.repoRoot, ws.ID)
		if err != nil || latestDisp == nil {
			continue
		}
		dispID, _ := latestDisp.Data["dispatch_id"].(string)
		if dispID == "" {
			continue
		}

		latestEvt, err := app.store.LatestTaskEvent(app.repoRoot, ws.ID, dispID)
		if err != nil {
			continue
		}
		if latestEvt != nil && (latestEvt.Kind == store.EventTaskCompleted || latestEvt.Kind == store.EventTaskFailed) {
			if st, ok := states[ws.ID]; ok && st.finalStatus == "" {
				st.finalStatus = "completed"
			}
			continue
		}

		activeCount++

		if _, ok := states[ws.ID]; !ok {
			states[ws.ID] = &watchState{dispatchID: dispID}
		}
		st := states[ws.ID]
		st.dispatchID = dispID

		var currentState dispatch.PaneState
		var jsonlSummary string
		usedJSONL := false

		if ws.WorktreePath != "" {
			jState, jSummary, jErr := dispatch.DetectClaudeActivity(ws.WorktreePath)
			if jErr == nil && jState != dispatch.PaneEmpty {
				currentState = jState
				jsonlSummary = jSummary
				usedJSONL = true
			}
			if jErr == nil && jState == dispatch.PaneEmpty {
				jsonlSummary = jSummary
			}
		}

		ag := agent.Get(ws.AgentRuntime)

		captured, captErr := app.term.CapturePane(ws.ID, 200)
		if captErr == nil {
			lastActivity := app.term.PaneLastActivity(ws.ID)
			capState := dispatch.DetectPaneStateWithPatterns(captured, ag.DialogIndicators(), ag.IdlePattern(), lastActivity, 15*time.Second)
			if capState == dispatch.PaneBlocked {
				currentState = dispatch.PaneBlocked
			}
			if !usedJSONL {
				currentState = capState
			}
		} else if !usedJSONL {
			alive, aliveErr := app.term.IsPaneAlive(ws.ID)
			if aliveErr != nil || !alive {
				handleTransition(app, ws, st, dispatch.PaneEmpty, "", captured, autoApprove, jsonFlag)
			}
			continue
		}

		if currentState == dispatch.PaneWorking || currentState == dispatch.PaneBlocked {
			st.sawWorking = true
		}

		if currentState == dispatch.PaneBlocked || currentState != st.prevState {
			handleTransition(app, ws, st, currentState, jsonlSummary, captured, autoApprove, jsonFlag)
			st.prevState = currentState
		}

		if currentState == dispatch.PaneIdle && st.sawWorking {
			if st.idleSince.IsZero() {
				st.idleSince = time.Now()
			} else if time.Since(st.idleSince) > 5*time.Minute && !st.warnedIdle {
				st.warnedIdle = true
				now := time.Now()
				if *jsonFlag {
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"workspace": ws.ID,
						"event":     "idle_warning",
						"duration":  time.Since(st.idleSince).String(),
					})
				} else {
					fmt.Printf("[%s] \u23f3 %s: idle for >5min\n", formatTime(now), ws.ID)
				}
			}
		} else {
			st.idleSince = time.Time{}
			st.warnedIdle = false
		}
	}

	if activeCount == 0 && len(states) > 0 {
		anyActive := false
		for _, st := range states {
			if st.sawWorking {
				anyActive = true
				break
			}
		}
		if anyActive {
			now := time.Now()
			if *jsonFlag {
				emitJSON(map[string]interface{}{
					"time":  formatTime(now),
					"event": "all_idle",
				})
			} else {
				fmt.Printf("[%s] All workspaces idle. Watching for new dispatches...\n", formatTime(now))
			}
		}
	}
}

func handleTransition(app *appContext, ws *store.Workspace, st *watchState, newState dispatch.PaneState, jsonlSummary, captured string, autoApprove bool, jsonFlag *bool) {
	now := time.Now()

	switch {
	case st.prevState == dispatch.PaneWorking && newState == dispatch.PaneIdle && st.sawWorking:
		summary := jsonlSummary
		if summary == "" && captured != "" {
			summary = truncate(dispatch.ExtractLastResponse(captured), 200)
		}

		commsDir, _ := dispatch.EnsureCommsDir(ws.ID)
		if commsDir != "" {
			response := summary
			if captured != "" {
				response = dispatch.ExtractLastResponse(captured)
			}
			_ = os.WriteFile(commsDir+"/result.txt", []byte(response), 0o644)
		}

		_ = app.store.EmitEvent(store.Event{
			ID:          uuid.New().String(),
			Kind:        store.EventTaskCompleted,
			WorkspaceID: ws.ID,
			RepoRoot:    app.repoRoot,
			Timestamp:   now.UTC(),
			Data: map[string]interface{}{
				"dispatch_id": st.dispatchID,
				"summary":     summary,
				"mode":        "interactive",
				"source":      "watch",
			},
		})

		ws.Status = string(workspace.StatusIdle)
		ws.UpdatedAt = now.UTC().Format(time.RFC3339)
		_ = app.store.SaveWorkspace(ws)

		st.finalStatus = "completed"

		if *jsonFlag {
			emitJSON(map[string]interface{}{
				"time":        formatTime(now),
				"workspace":   ws.ID,
				"dispatch_id": st.dispatchID,
				"event":       "completed",
				"summary":     summary,
			})
		} else {
			fmt.Printf("[%s] \u2713 %s %s: completed \u2014 %q\n", formatTime(now), ws.ID, st.dispatchID, truncate(summary, 80))
		}

	case newState == dispatch.PaneBlocked:
		dialogCtx := "permission dialog active"
		if captured != "" {
			dialogCtx = dispatch.ExtractDialogContext(captured)
		}

		if autoApprove {
			// Pick the right approval key based on the dialog type.
			// Cursor uses 'y' for shell approval, 'a' for trust. Claude uses Enter.
			approveKey := "Enter"
			if strings.Contains(captured, "Run this command?") || strings.Contains(captured, "Run (once)") {
				approveKey = "y" // Cursor shell approval
			} else if strings.Contains(captured, "Trust this workspace") {
				approveKey = "a" // Cursor trust dialog
			}
			if err := app.term.SendKeys(ws.ID, approveKey); err == nil {
				st.finalStatus = "working"
				if *jsonFlag {
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"workspace": ws.ID,
						"event":     "blocked",
						"dialog":    dialogCtx,
					})
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"workspace": ws.ID,
						"event":     "approved",
					})
				} else {
					fmt.Printf("[%s] \u26a0 %s: permission dialog \u2014 %q\n", formatTime(now), ws.ID, dialogCtx)
					fmt.Printf("[%s] \u2713 %s: auto-approved\n", formatTime(now), ws.ID)
				}
				// Rapid re-check: after approving, Claude often hits another dialog
				// within seconds. Poll quickly to catch consecutive dialogs.
				wsAgent := agent.Get(ws.AgentRuntime)
				for retry := 0; retry < 5; retry++ {
					time.Sleep(3 * time.Second)
					recapture, recapErr := app.term.CapturePane(ws.ID, 200)
					if recapErr != nil {
						break
					}
					reActivity := app.term.PaneLastActivity(ws.ID)
					reState := dispatch.DetectPaneStateWithPatterns(recapture, wsAgent.DialogIndicators(), wsAgent.IdlePattern(), reActivity, 5*time.Second)
					if reState != dispatch.PaneBlocked {
						break
					}
					// Pick approval key for this dialog too
					reKey := "Enter"
					if strings.Contains(recapture, "Run this command?") || strings.Contains(recapture, "Run (once)") {
						reKey = "y"
					} else if strings.Contains(recapture, "Trust this workspace") {
						reKey = "a"
					}
					_ = app.term.SendKeys(ws.ID, reKey)
					reDialog := dispatch.ExtractDialogContext(recapture)
					if !*jsonFlag {
						fmt.Printf("[%s] \u2713 %s: auto-approved \u2014 %q\n", formatTime(time.Now()), ws.ID, reDialog)
					}
				}
			}
		} else {
			st.finalStatus = "blocked"
			if *jsonFlag {
				emitJSON(map[string]interface{}{
					"time":      formatTime(now),
					"workspace": ws.ID,
					"event":     "blocked",
					"dialog":    dialogCtx,
				})
			} else {
				fmt.Printf("[%s] \u26a0 %s: permission dialog \u2014 %q\n", formatTime(now), ws.ID, dialogCtx)
			}
		}

	case newState == dispatch.PaneEmpty:
		st.finalStatus = "exited"
		if *jsonFlag {
			emitJSON(map[string]interface{}{
				"time":      formatTime(now),
				"workspace": ws.ID,
				"event":     "exited",
			})
		} else {
			fmt.Printf("[%s] \u26a0 %s: Claude exited\n", formatTime(now), ws.ID)
		}

	case newState == dispatch.PaneWorking:
		if *jsonFlag {
			emitJSON(map[string]interface{}{
				"time":      formatTime(now),
				"workspace": ws.ID,
				"event":     "transition",
				"from":      string(st.prevState),
				"to":        "working",
			})
		} else {
			fmt.Printf("[%s] \u25b6 %s: working\n", formatTime(now), ws.ID)
		}
		st.finalStatus = "working"
	}
}

func printSummary(app *appContext, states map[string]*watchState, jsonFlag *bool) {
	if len(states) == 0 {
		return
	}

	if *jsonFlag {
		summaries := make([]map[string]interface{}, 0, len(states))
		for wsID, st := range states {
			status := st.finalStatus
			if status == "" {
				status = "unknown"
			}
			summaries = append(summaries, map[string]interface{}{
				"workspace":   wsID,
				"dispatch_id": st.dispatchID,
				"status":      status,
			})
		}
		emitJSON(map[string]interface{}{
			"event":      "stopped",
			"time":       formatTime(time.Now()),
			"workspaces": summaries,
		})
		return
	}

	fmt.Println("\nStopped watching. Summary:")
	for wsID, st := range states {
		icon := "\u25b6"
		status := "still working"
		switch st.finalStatus {
		case "completed":
			icon = "\u2713"
			status = "completed"
		case "blocked":
			icon = "\u26a0"
			status = "blocked"
		case "exited":
			icon = "\u26a0"
			status = "exited"
		}
		dispID := st.dispatchID
		if dispID == "" {
			dispID = "---"
		}
		fmt.Printf("  %s: %s %s %s\n", wsID, dispID, icon, status)
	}
}

func countActiveWorkspaces(app *appContext) int {
	allWS, err := app.store.ListWorkspaces(app.repoRoot, store.ListFilter{})
	if err != nil {
		return 0
	}
	count := 0
	for _, ws := range allWS {
		status := workspace.WorkspaceStatus(ws.Status)
		if status == workspace.StatusRunning || status == workspace.StatusIdle {
			count++
		}
	}
	return count
}

func formatTime(t time.Time) string {
	return t.Format("15:04:05")
}

func emitJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Println(string(data))
}

// ---------- PR monitoring (--react) ----------

// ghPR represents a GitHub PR as returned by `gh pr list --json`.
type ghPR struct {
	Number         int           `json:"number"`
	HeadRefName    string        `json:"headRefName"`
	ReviewDecision string        `json:"reviewDecision"`
	URL            string        `json:"url"`
	StatusChecks   []ghCheckRun  `json:"statusCheckRollup"`
	Comments       []ghComment   `json:"comments"`
}

type ghComment struct {
	Author    ghAuthor `json:"author"`
	Body      string   `json:"body"`
	CreatedAt string   `json:"createdAt"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

type ghCheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// checkGHCLI verifies that the gh CLI is installed.
func checkGHCLI() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("--react requires the GitHub CLI (gh). Install it: https://cli.github.com")
	}
	return nil
}

// fetchOpenPRs calls `gh pr list` and returns open PRs.
func fetchOpenPRs() ([]ghPR, error) {
	cmd := exec.Command("gh", "pr", "list",
		"--json", "number,headRefName,reviewDecision,statusCheckRollup,url,comments",
		"--state", "open", "--limit", "50")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}

	var prs []ghPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse gh pr list: %w", err)
	}
	return prs, nil
}

// prCIStatus summarizes CI status from statusCheckRollup.
// Returns "SUCCESS", "FAILURE", or "PENDING".
func prCIStatus(checks []ghCheckRun) string {
	if len(checks) == 0 {
		return "PENDING"
	}

	hasFailure := false
	allComplete := true
	for _, c := range checks {
		if c.Status != "COMPLETED" {
			allComplete = false
			continue
		}
		if c.Conclusion == "FAILURE" || c.Conclusion == "ERROR" || c.Conclusion == "TIMED_OUT" || c.Conclusion == "CANCELLED" {
			hasFailure = true
		}
	}

	if hasFailure {
		return "FAILURE"
	}
	if !allComplete {
		return "PENDING"
	}
	return "SUCCESS"
}

// branchToWorkspaceID extracts workspace ID from a towr/* branch name.
// e.g., "towr/auth" -> "auth"
func branchToWorkspaceID(branch string) string {
	if !strings.HasPrefix(branch, "towr/") {
		return ""
	}
	return strings.TrimPrefix(branch, "towr/")
}

// pollPRsSingleRepo polls PRs and reacts in single-repo mode.
func pollPRsSingleRepo(app *appContext, prStates map[int]*prState, wsStates map[string]*watchState, jsonFlag *bool) {
	prs, err := fetchOpenPRs()
	if err != nil {
		return
	}

	for _, pr := range prs {
		wsID := branchToWorkspaceID(pr.HeadRefName)
		if wsID == "" {
			continue
		}

		ciStatus := prCIStatus(pr.StatusChecks)

		st, ok := prStates[pr.Number]
		if !ok {
			st = &prState{
				number:      pr.Number,
				workspaceID: wsID,
			}
			prStates[pr.Number] = st
		}

		// Detect state changes and reset reaction flags.
		if ciStatus != st.lastCI {
			st.reactedCI = false
			if ciStatus != "FAILURE" {
				// New push or CI recovered — reset retry count.
				st.ciRetries = 0
			}
			st.lastCI = ciStatus
		}
		if pr.ReviewDecision != st.lastReview {
			st.reactedReview = false
			if pr.ReviewDecision != "CHANGES_REQUESTED" {
				st.reviewRetries = 0
			}
			st.lastReview = pr.ReviewDecision
		}

		now := time.Now()

		// React: CI failed.
		if ciStatus == "FAILURE" && !st.reactedCI && st.ciRetries < maxReactRetries {
			st.reactedCI = true
			st.ciRetries++
			prompt := fmt.Sprintf("CI checks failed on PR #%d (%s). Run the failing tests, read the error output, and fix the code. Then push your fixes.", pr.Number, pr.URL)
			if *jsonFlag {
				emitJSON(map[string]interface{}{
					"time":      formatTime(now),
					"event":     "pr_ci_failed",
					"pr":        pr.Number,
					"branch":    pr.HeadRefName,
					"workspace": wsID,
					"retry":     st.ciRetries,
				})
			} else {
				fmt.Printf("[%s] \u2717 PR #%d (%s): CI failed \u2014 dispatching fix (%d/%d)\n",
					formatTime(now), pr.Number, pr.HeadRefName, st.ciRetries, maxReactRetries)
			}
			dispatchReaction(wsID, prompt, jsonFlag)
		}

		// React: changes requested.
		if pr.ReviewDecision == "CHANGES_REQUESTED" && !st.reactedReview && st.reviewRetries < maxReactRetries {
			st.reactedReview = true
			st.reviewRetries++
			prompt := fmt.Sprintf("Code review on PR #%d has requested changes. Read the review comments with 'gh pr view %d --comments' and address each issue. Then push your fixes.", pr.Number, pr.Number)
			if *jsonFlag {
				emitJSON(map[string]interface{}{
					"time":      formatTime(now),
					"event":     "pr_changes_requested",
					"pr":        pr.Number,
					"branch":    pr.HeadRefName,
					"workspace": wsID,
					"retry":     st.reviewRetries,
				})
			} else {
				fmt.Printf("[%s] \U0001f4ac PR #%d (%s): changes requested \u2014 dispatching fix (%d/%d)\n",
					formatTime(now), pr.Number, pr.HeadRefName, st.reviewRetries, maxReactRetries)
			}
			dispatchReaction(wsID, prompt, jsonFlag)
		}

		// React: new comments (questions or feedback).
		// On first discovery (lastCommentCount == 0), react to existing comments too.
		commentCount := len(pr.Comments)
		if commentCount > st.lastCommentCount && st.commentRetries < maxReactRetries {
			// Find the newest comment(s) that we haven't seen.
			startIdx := st.lastCommentCount
			if startIdx < 0 {
				startIdx = 0
			}
			newComments := pr.Comments[startIdx:]
			// Filter: skip towr's own replies and bot comments.
			// Only react to comments containing @towr mention.
			var actionableComments []ghComment
			for _, c := range newComments {
				// Skip our own replies (towr signature).
				if strings.Contains(c.Body, towrReplySignature) {
					continue
				}
				// Skip bot/automated comments.
				if strings.Contains(c.Body, "Auto-generated by") || strings.Contains(c.Body, "towr orchestrate") {
					continue
				}
				// Only react to comments that mention @towr.
				if !strings.Contains(strings.ToLower(c.Body), "@towr") {
					continue
				}
				actionableComments = append(actionableComments, c)
			}
			if len(actionableComments) > 0 {
				st.commentRetries++
				lastComment := actionableComments[len(actionableComments)-1]
				prompt := fmt.Sprintf(
					"There is a new comment on PR #%d from @%s:\n\n> %s\n\n"+
						"Read the full conversation with 'gh pr view %d --comments'.\n\n"+
						"CRITICAL: Your reply MUST start with this exact line:\n%s\n\n"+
						"If the comment is a question, reply with gh pr comment.\n"+
						"If it requests code changes, make changes and push.\n"+
						"Example reply command:\ngh pr comment %d --body '%s\n\nYour detailed reply here'",
					pr.Number, lastComment.Author.Login, truncate(lastComment.Body, 500),
					pr.Number,
					towrReplySignature,
					pr.Number, towrReplySignature)
				if *jsonFlag {
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"event":     "pr_new_comment",
						"pr":        pr.Number,
						"branch":    pr.HeadRefName,
						"workspace": wsID,
						"author":    lastComment.Author.Login,
						"comment":   truncate(lastComment.Body, 200),
					})
				} else {
					fmt.Printf("[%s] \U0001f4ac PR #%d (%s): new comment from @%s \u2014 dispatching reply\n",
						formatTime(now), pr.Number, pr.HeadRefName, lastComment.Author.Login)
				}
				dispatchReaction(wsID, prompt, jsonFlag)
			}
		}
		st.lastCommentCount = commentCount

		// Notify: approved + CI green.
		if pr.ReviewDecision == "APPROVED" && ciStatus == "SUCCESS" {
			// Only notify once per state combination.
			if !st.reactedReview || !st.reactedCI {
				st.reactedReview = true
				st.reactedCI = true
				if *jsonFlag {
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"event":     "pr_ready_to_merge",
						"pr":        pr.Number,
						"branch":    pr.HeadRefName,
						"workspace": wsID,
					})
				} else {
					fmt.Printf("[%s] \u2713 PR #%d (%s): approved + CI passing \u2014 ready to merge\n",
						formatTime(now), pr.Number, pr.HeadRefName)
				}
			}
		}
	}
}

// pollPRsAllRepos polls PRs and reacts in all-repos mode.
func pollPRsAllRepos(cache *repoStoreCache, prStates map[int]*prState, wsStates map[string]*watchState, jsonFlag *bool) {
	// In all-repos mode, we still call `gh` from the cwd.
	// The user is expected to be authenticated with gh for the relevant repos.
	prs, err := fetchOpenPRs()
	if err != nil {
		return
	}

	for _, pr := range prs {
		wsID := branchToWorkspaceID(pr.HeadRefName)
		if wsID == "" {
			continue
		}

		ciStatus := prCIStatus(pr.StatusChecks)

		st, ok := prStates[pr.Number]
		if !ok {
			st = &prState{
				number:      pr.Number,
				workspaceID: wsID,
			}
			prStates[pr.Number] = st
		}

		if ciStatus != st.lastCI {
			st.reactedCI = false
			if ciStatus != "FAILURE" {
				st.ciRetries = 0
			}
			st.lastCI = ciStatus
		}
		if pr.ReviewDecision != st.lastReview {
			st.reactedReview = false
			if pr.ReviewDecision != "CHANGES_REQUESTED" {
				st.reviewRetries = 0
			}
			st.lastReview = pr.ReviewDecision
		}

		now := time.Now()

		if ciStatus == "FAILURE" && !st.reactedCI && st.ciRetries < maxReactRetries {
			st.reactedCI = true
			st.ciRetries++
			prompt := fmt.Sprintf("CI checks failed on PR #%d (%s). Run the failing tests, read the error output, and fix the code. Then push your fixes.", pr.Number, pr.URL)
			if *jsonFlag {
				emitJSON(map[string]interface{}{
					"time":      formatTime(now),
					"event":     "pr_ci_failed",
					"pr":        pr.Number,
					"branch":    pr.HeadRefName,
					"workspace": wsID,
					"retry":     st.ciRetries,
				})
			} else {
				fmt.Printf("[%s] \u2717 PR #%d (%s): CI failed \u2014 dispatching fix (%d/%d)\n",
					formatTime(now), pr.Number, pr.HeadRefName, st.ciRetries, maxReactRetries)
			}
			dispatchReaction(wsID, prompt, jsonFlag)
		}

		if pr.ReviewDecision == "CHANGES_REQUESTED" && !st.reactedReview && st.reviewRetries < maxReactRetries {
			st.reactedReview = true
			st.reviewRetries++
			prompt := fmt.Sprintf("Code review on PR #%d has requested changes. Read the review comments with 'gh pr view %d --comments' and address each issue. Then push your fixes.", pr.Number, pr.Number)
			if *jsonFlag {
				emitJSON(map[string]interface{}{
					"time":      formatTime(now),
					"event":     "pr_changes_requested",
					"pr":        pr.Number,
					"branch":    pr.HeadRefName,
					"workspace": wsID,
					"retry":     st.reviewRetries,
				})
			} else {
				fmt.Printf("[%s] \U0001f4ac PR #%d (%s): changes requested \u2014 dispatching fix (%d/%d)\n",
					formatTime(now), pr.Number, pr.HeadRefName, st.reviewRetries, maxReactRetries)
			}
			dispatchReaction(wsID, prompt, jsonFlag)
		}

		// React: new comments containing @towr mention.
		commentCount := len(pr.Comments)
		if commentCount > st.lastCommentCount && st.commentRetries < maxReactRetries {
			startIdx := st.lastCommentCount
			if startIdx < 0 {
				startIdx = 0
			}
			newComments := pr.Comments[startIdx:]
			var actionableComments []ghComment
			for _, c := range newComments {
				if strings.Contains(c.Body, towrReplySignature) {
					continue
				}
				if strings.Contains(c.Body, "Auto-generated by") || strings.Contains(c.Body, "towr orchestrate") {
					continue
				}
				if !strings.Contains(strings.ToLower(c.Body), "@towr") {
					continue
				}
				actionableComments = append(actionableComments, c)
			}
			if len(actionableComments) > 0 {
				st.commentRetries++
				lastComment := actionableComments[len(actionableComments)-1]
				prompt := fmt.Sprintf(
					"There is a new comment on PR #%d from @%s:\n\n> %s\n\n"+
						"Read the full conversation with 'gh pr view %d --comments'.\n\n"+
						"CRITICAL: Your reply MUST start with this exact line:\n%s\n\n"+
						"If the comment is a question, reply with gh pr comment.\n"+
						"If it requests code changes, make changes and push.\n"+
						"Example reply command:\ngh pr comment %d --body '%s\n\nYour detailed reply here'",
					pr.Number, lastComment.Author.Login, truncate(lastComment.Body, 500),
					pr.Number,
					towrReplySignature,
					pr.Number, towrReplySignature)
				if *jsonFlag {
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"event":     "pr_new_comment",
						"pr":        pr.Number,
						"branch":    pr.HeadRefName,
						"workspace": wsID,
						"author":    lastComment.Author.Login,
						"comment":   truncate(lastComment.Body, 200),
					})
				} else {
					fmt.Printf("[%s] \U0001f4ac PR #%d (%s): new comment from @%s \u2014 dispatching reply\n",
						formatTime(now), pr.Number, pr.HeadRefName, lastComment.Author.Login)
				}
				dispatchReaction(wsID, prompt, jsonFlag)
			}
		}
		st.lastCommentCount = commentCount

		if pr.ReviewDecision == "APPROVED" && ciStatus == "SUCCESS" {
			if !st.reactedReview || !st.reactedCI {
				st.reactedReview = true
				st.reactedCI = true
				if *jsonFlag {
					emitJSON(map[string]interface{}{
						"time":      formatTime(now),
						"event":     "pr_ready_to_merge",
						"pr":        pr.Number,
						"branch":    pr.HeadRefName,
						"workspace": wsID,
					})
				} else {
					fmt.Printf("[%s] \u2713 PR #%d (%s): approved + CI passing \u2014 ready to merge\n",
						formatTime(now), pr.Number, pr.HeadRefName)
				}
			}
		}
	}
}

// dispatchReaction shells out to `towr dispatch` or `towr send` to handle a reaction.
// If dispatch fails (workspace RUNNING), falls back to `towr send` for interactive sessions.
func dispatchReaction(wsID, prompt string, jsonFlag *bool) {
	towrBin, err := os.Executable()
	if err != nil {
		towrBin = "towr"
	}

	cmd := exec.Command(towrBin, "dispatch", wsID, prompt)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		// Dispatch failed — workspace may be RUNNING. Try `towr send` as fallback.
		sendCmd := exec.Command(towrBin, "send", wsID, prompt)
		sendCmd.Stdout = os.Stdout
		sendCmd.Stderr = os.Stderr
		if sendErr := sendCmd.Run(); sendErr == nil {
			return // send succeeded
		}
		now := time.Now()
		if *jsonFlag {
			emitJSON(map[string]interface{}{
				"time":      formatTime(now),
				"event":     "dispatch_failed",
				"workspace": wsID,
				"error":     err.Error(),
			})
		} else {
			fmt.Printf("[%s] \u26a0 %s: dispatch failed \u2014 %v\n", formatTime(now), wsID, err)
		}
	}
}
