package app

import (
	"fmt"
	"strings"
	"time"

	kittyx "github.com/alienxp03/kesh/internal/kitty"
	tea "github.com/charmbracelet/bubbletea"
)

func (m model) updateMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case zoxideEntriesMsg:
		// Zoxide source projects arrive after first paint so the slow query
		// never blocks the UI. Merge, re-apply aliases/pins, and refresh.
		m.zoxidePending = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		if len(msg.entries) > 0 {
			m.entries = append(m.entries, msg.entries...)
			sortEntries(m.entries)
			applyNames(m.entries, m.names)
			applyPins(m.entries, m.pins)
			m.rebuildRows()
		}
		return m, nil
	case actionMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Quit
	case openPRMsg:
		m.err = msg.err
		return m, nil
	case pinsPersistMsg:
		if m.mode == modePin {
			m.pinBusy = false
		}
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.pins = msg.pins
		applyPins(m.entries, m.pins)
		if msg.finishPin && m.mode == modePin {
			m.cancelMode()
		}
		m.err = nil
		return m, nil
	case worktreeOpenMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("find existing worktree window: %w", msg.err)
			return m, nil
		}
		entryIndex := m.entryIndexByKey(msg.entryKey)
		if entryIndex < 0 {
			return m, nil
		}
		if len(msg.windowIDs) > 0 {
			windowID := msg.windowIDs[0]
			return m, func() tea.Msg {
				return actionMsg{err: (kittyx.Client{Executable: m.kitty}).FocusWindow(windowID)}
			}
		}
		return m, runAction(m.kitty, m.zoxide, m.entries[entryIndex], row{
			entryIndex: entryIndex, tabIndex: -1, windowIndex: -1, worktreePath: msg.path,
		})
	case closeMsg:
		m.closeBusy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		var pinsCmd tea.Cmd
		if msg.deletedSavedKey != "" {
			updatedPins := make(pinStore, len(m.pins))
			for slot, target := range m.pins {
				if target.Key != msg.deletedSavedKey {
					updatedPins[slot] = target
				}
			}
			if len(updatedPins) != len(m.pins) {
				pinsCmd = persistPinsCommand(m.kitty, updatedPins, false)
			}
		}
		preserveExpandedState(m.entries, msg.entries)
		m.entries = msg.entries
		applyNames(m.entries, m.names)
		applyPins(m.entries, m.pins)
		m.cancelMode()
		m.err = nil
		m.previewID = 0
		m.preview = ""
		m.previewErr = nil
		m.previewBusy = false
		m.rebuildRows()
		return m, tea.Batch(m.queuePreview(), pinsCmd)
	case destroyMsg:
		m.closeBusy = false
		m.destroyPlan = nil
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		// runDestroy reloads every entry, so the Worktree tab's entry index may
		// have shifted and its worktrees are no longer loaded. Capture the
		// project path before the swap, re-resolve it by path, and refetch its
		// worktree list so the tab reflects the removal.
		var tabPath string
		if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
			tabPath = m.entries[m.worktreeFilterEntryIndex].path
		}
		preserveExpandedState(m.entries, msg.entries)
		m.entries = msg.entries
		applyNames(m.entries, m.names)
		applyPins(m.entries, m.pins)
		m.cancelMode()
		m.err = nil
		m.previewID = 0
		m.preview = ""
		m.previewErr = nil
		m.previewBusy = false
		if m.filter == filterWorktrees && tabPath != "" {
			for i := range m.entries {
				if m.entries[i].path == tabPath {
					m.worktreeFilterEntryIndex = i
					return m, fetchWorktrees(tabPath, i, -1, -1)
				}
			}
			// The project itself was destroyed; leave the tab.
			m.filter = m.previousFilter
			if m.filter == filterWorktrees {
				m.filter = filterAll
			}
			m.worktreeFilterEntryIndex = -1
			m.worktreeFilterRows = nil
		}
		m.rebuildRows()
		return m, m.queuePreview()
	case destroyPlanMsg:
		m.destroyPlanning = false
		entryIndex := m.entryIndexByKey(msg.entryKey)
		if entryIndex < 0 {
			return m, nil
		}
		m.activateMode(modeCloseConfirm)
		m.closeRow = row{entryIndex: entryIndex, tabIndex: -1, windowIndex: -1}
		m.destroyPlan = &msg.plan
		m.err = nil
		return m, nil
	case previewMsg:
		if msg.windowID != m.previewID || msg.request != m.previewRequest {
			return m, nil
		}
		m.previewBusy = false
		m.preview = msg.content
		m.previewErr = msg.err
		return m, m.queuePreviewRefresh(msg.windowID, msg.request)
	case previewRefreshMsg:
		if msg.request != m.previewRequest || !m.shouldRefreshPreview(msg.windowID) {
			return m, nil
		}
		m.previewRequest++
		return m, fetchPreviewRequest(m.kitty, msg.windowID, m.previewRequest)
	case renameMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		entryIndex, tabIndex, windowIndex := m.resolveRenameTarget(msg.target)
		if entryIndex < 0 {
			return m, nil
		}
		entry := &m.entries[entryIndex]
		if windowIndex >= 0 {
			entry.tabs[tabIndex].windows[windowIndex].title = msg.title
		} else if tabIndex >= 0 {
			entry.tabs[tabIndex].title = msg.title
		} else {
			m.names = msg.names
			entry.name = msg.title
			if entry.name == "" {
				entry.name = entry.originalName
			}
			m.rebuildRows()
		}
		m.cancelMode()
		m.err = nil
	case createMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Quit
	case cloneMsg:
		m.cloneBusy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Quit
	case prCheckoutMsg:
		m.prCheckoutBusy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		return m, tea.Quit
	case prPreviewMsg:
		// A lookup may finish after the input changed; never show its stale path.
		if m.mode == modeCheckoutPR && msg.value == m.prCheckoutValue {
			m.prCheckoutBranch = msg.branch
			m.prCheckoutPath = msg.repoPath
			m.prCheckoutClone = msg.newClone
		}
		return m, nil
	case worktreeMsg:
		if m.mode != modeWorktreeCreate {
			return m, nil
		}
		if m.worktreeBusy {
			// Creation completed.
			m.worktreeBusy = false
			m.cancelMode()
			if msg.err != nil {
				m.err = msg.err
				return m, nil
			}
			if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
				entryIndex := m.worktreeFilterEntryIndex
				return m, fetchWorktrees(m.entries[entryIndex].path, entryIndex, -1, -1)
			}
			return m, tea.Quit
		}
		// Validation runs only on Enter. On success, proceed to create;
		// otherwise surface the error without leaving the form.
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.worktreeBusy = true
		return m, m.createWorktree()
	case worktreeRecipeMsg:
		if m.mode != modeWorktreeCreate {
			return m, nil
		}
		entries := m.worktreeEntries()
		if len(entries) != 1 || entries[0].path != msg.projectPath {
			return m, nil
		}
		if msg.err != nil {
			m.cancelMode()
			m.err = msg.err
			return m, nil
		}
		m.worktreeRecipe = msg.recipe
		m.worktreeRecipePath = msg.recipePath
		m.worktreeRepositories = msg.repositories
		if msg.recipe != nil {
			// Workspaces is the single recipe-honoring mode. With more than
			// one workspace it shows an editable checklist (mode "selected");
			// otherwise there is nothing to toggle, so defer to the recipe.
			if len(msg.recipe.Workspaces) > 1 {
				m.worktreeRecipeMode = "selected"
				m.worktreeCustomWorkspaces = true
			} else {
				m.worktreeRecipeMode = msg.recipe.WorkspaceMode
				m.worktreeCustomWorkspaces = false
			}
			m.ensureWorktreeSelection()
		}
		return m, nil
	case worktreeListMsg:
		updated := false
		entryIndex, _, _ := m.resolveWorktreeDirectory(msg.dir)
		if entryIndex >= 0 {
			e := &m.entries[entryIndex]
			if msg.err != nil {
				e.worktreesLoaded = false
				m.err = msg.err
				return m, nil
			}
			e.worktrees = msg.worktrees
			e.worktreesLoaded = true
			updated = true
		}
		// Handle worktree filter mode
		if m.filter == filterWorktrees && msg.err == nil {
			if entryIndex >= 0 && entryIndex < len(m.entries) && m.entries[entryIndex].path == msg.dir {
				m.worktreeFilterEntryIndex = entryIndex
				e := &m.entries[entryIndex]
				e.worktrees = msg.worktrees
				e.worktreesLoaded = true
				m.rebuildWorktreeRows()
				return m, m.refreshPRStatuses(msg.dir, false)
			}
		}
		if updated {
			m.err = nil
			m.rebuildRows()
			return m, m.refreshPRStatuses(msg.dir, false)
		}
		return m, nil
	case worktreeFetchedMsg:
		// Reload even if fetch failed so local worktree state remains useful, but
		// make the failed remote refresh visible to the user.
		if msg.err != nil {
			m.err = msg.err
		}
		entryIndex, _, _ := m.resolveWorktreeDirectory(msg.dir)
		if entryIndex < 0 {
			return m, nil
		}
		cmds := []tea.Cmd{fetchWorktrees(msg.dir, entryIndex, -1, -1)}
		cmds = append(cmds, m.refreshPRStatuses(msg.dir, true))
		return m, tea.Batch(cmds...)
	case worktreePullMsg:
		m.worktreePullBusy = false
		entryIndex, _, _ := m.resolveWorktreeDirectory(msg.dir)
		if entryIndex < 0 {
			return m, nil
		}
		if msg.err != nil {
			// Bulk pull may partially succeed, so surface the error but still
			// reload — the worktrees that did pull have new ahead/behind state.
			m.err = msg.err
			return m, fetchWorktrees(msg.dir, entryIndex, -1, -1)
		}
		m.err = nil
		return m, fetchWorktrees(msg.dir, entryIndex, -1, -1)
	case pathPRMsg:
		if m.pathPRChecked == nil {
			m.pathPRChecked = map[string]bool{}
		}
		m.pathPRChecked[msg.path] = true
		for index := range m.entries {
			if m.entries[index].path == msg.path {
				m.entries[index].pathPR = msg.info
			}
			for tabIndex := range m.entries[index].tabs {
				for windowIndex := range m.entries[index].tabs[tabIndex].windows {
					window := &m.entries[index].tabs[tabIndex].windows[windowIndex]
					if window.cwd == msg.path {
						window.pathPR = msg.info
					}
				}
			}
		}
		if msg.info.RepoKey != "" {
			return m, m.refreshPRStatuses(msg.path, false)
		}
		return m, nil
	case prStatusCacheMsg:
		if m.prStatusDirPending != nil {
			m.prStatusDirPending[msg.dir] = false
		}
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		focusedWorktree := m.focusedWorktreePath()
		if !msg.force {
			m.applyPRStatuses(msg.repoKey, msg.pullRequests)
			m.rebuildRows()
			m.restoreFocusedWorktree(focusedWorktree)
			lastFetch := msg.cachedAt
			if fetched := m.prStatusLastFetch[msg.repoKey]; fetched.After(lastFetch) {
				lastFetch = fetched
			}
			if !lastFetch.IsZero() && msg.now.Sub(lastFetch) < prStatusThrottle {
				return m, nil
			}
		}
		if m.prStatusPending == nil {
			m.prStatusPending = map[string]bool{}
		}
		if m.prStatusPending[msg.repoKey] {
			return m, nil
		}
		m.prStatusPending[msg.repoKey] = true
		return m, queryPRStatusesCommand(msg.dir, msg.repoKey)
	case prStatusMsg:
		if m.prStatusPending != nil {
			m.prStatusPending[msg.repoKey] = false
		}
		if msg.err != nil {
			if strings.Contains(msg.repoKey, "github.com") {
				m.err = fmt.Errorf("refresh PR status: %w", msg.err)
			}
			return m, nil
		}
		if m.prStatusLastFetch == nil {
			m.prStatusLastFetch = map[string]time.Time{}
		}
		m.prStatusLastFetch[msg.repoKey] = msg.fetchedAt
		focusedWorktree := m.focusedWorktreePath()
		m.applyPRStatuses(msg.repoKey, msg.pullRequests)
		m.rebuildRows()
		m.restoreFocusedWorktree(focusedWorktree)
		return m, nil
	case mergedWorktreeListMsg:
		m.mergedWorktreeBusy = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		if len(msg.worktrees) == 0 {
			m.err = fmt.Errorf("no merged worktrees to remove")
			return m, nil
		}
		entryIndex, tabIndex, windowIndex := m.resolveWorktreeDirectory(msg.dir)
		if entryIndex < 0 {
			return m, nil
		}
		m.activateMode(modeCloseConfirm)
		m.closeRow = row{
			entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex,
			section: msg.selected.section, worktreePath: msg.selected.worktreePath,
		}
		m.mergedWorktrees = msg.worktrees
		m.closeBusy = false
		m.worktreeForcePrompt = false
		m.err = nil
		return m, nil
	case mergedWorktreeRemoveMsg:
		m.closeBusy = false
		if msg.err != nil {
			entryIndex, tabIndex, windowIndex := m.resolveWorktreeDirectory(msg.dir)
			m.invalidateWorktrees(row{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex})
			if !msg.forceTried {
				// Keep the confirmation visible: a dirty or locked merged worktree
				// can be removed deliberately with force, rather than leaving a
				// truncated footer error with no discoverable next action.
				m.mergedWorktrees = msg.remaining
				m.worktreeForcePrompt = true
				m.err = fmt.Errorf("some merged worktrees could not be removed")
				return m, nil
			}
			m.cancelMode()
			m.err = msg.err
			m.rebuildRows()
			return m, nil
		}
		m.cancelMode()
		m.err = nil
		entryIndex, tabIndex, windowIndex := m.resolveWorktreeDirectory(msg.dir)
		if entryIndex < 0 {
			return m, nil
		}
		return m, fetchWorktrees(msg.dir, entryIndex, tabIndex, windowIndex)
	case bulkWorktreeRemoveMsg:
		m.closeBusy = false
		m.cancelMode()
		m.wtBulkSelected = nil
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
		}
		entryIndex, _, _ := m.resolveWorktreeDirectory(msg.dir)
		if entryIndex >= 0 {
			return m, fetchWorktrees(msg.dir, entryIndex, -1, -1)
		}
		m.rebuildRows()
		return m, nil
	case worktreeRemoveMsg:
		m.closeBusy = false
		if msg.err != nil {
			if !msg.forceTried {
				// Normal remove failed (dirty, locked, etc.) — offer force.
				m.worktreeForcePrompt = true
				m.err = msg.err
				return m, nil
			}
			// Force failed too: surface the error and leave the form.
			m.cancelMode()
			m.err = msg.err
			return m, nil
		}
		// Removed: refresh that window's worktree list and close the popup.
		m.cancelMode()
		m.err = nil
		// In the Worktree tab the removal targets the tab's project; windowAt
		// and closedEntryAt do not resolve it (open entries, tabIndex -1), so
		// refetch the tab's worktree list directly.
		entryIndex, tabIndex, windowIndex := m.resolveWorktreeDirectory(msg.dir)
		if entryIndex >= 0 {
			return m, fetchWorktrees(msg.dir, entryIndex, tabIndex, windowIndex)
		}
		return m, nil
	case saveSessionMsg:
		m.saving = false
		m.cancelMode()
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		entryIndex := m.entryIndexByKey(msg.entryKey)
		if entryIndex < 0 {
			return m, nil
		}
		entry := &m.entries[entryIndex]
		entry.saved = true
		entry.name = msg.record.Name
		entry.originalName = msg.record.Name
		entry.sessionFile = msg.record.SessionFile
		updatedPins := copyPins(m.pins)
		pinsChanged := false
		for slot, target := range m.pins {
			if target.Key == entry.key && target.SessionFile != msg.record.SessionFile {
				target.SessionFile = msg.record.SessionFile
				updatedPins[slot] = target
				pinsChanged = true
			}
		}
		if pinsChanged {
			m.err = nil
			return m, persistPinsCommand(m.kitty, updatedPins, false)
		}
		m.err = nil
		return m, nil
	}
	return m, nil
}
