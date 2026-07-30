package app

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) updateNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.pendingG {
		m.pendingG = false
		if key == "g" {
			m.cursor = 0
			return m, m.queuePreview()
		}
	}
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.activateMode(modeHelp)
		m.helpScroll = 0
		m.err = nil
		return m, nil
	case "esc":
		// Escape returns to command mode from transient modes; once there,
		// it is intentionally a no-op so a repeated key cannot close Kesh.
		if m.filter == filterWorktrees {
			// Worktree rows are not selectable; discard any main-list selection
			// before returning so it cannot affect subsequent bulk actions.
			m.selected = map[string]bool{}
			m.wtBulkSelected = nil
			// Return to previous filter
			m.filter = m.previousFilter
			m.worktreeFilterEntryIndex = -1
			m.worktreeFilterRows = nil
			m.worktreeLoading = false
			m.query = ""
			m.rebuildRows()
			return m, nil
		}
		return m, nil
	case "/":
		m.activateMode(modeSearch)
	case "up", "ctrl+k", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "ctrl+j", "j":
		if m.cursor+1 < len(m.rows) {
			m.cursor++
		}
	case "ctrl+d", "pgdown":
		m.cursor = min(m.cursor+m.listPageSize(), max(0, len(m.rows)-1))
	case "ctrl+u", "pgup":
		m.cursor = max(0, m.cursor-m.listPageSize())
	case "G":
		m.cursor = max(0, len(m.rows)-1)
	case "right", "l":
		m.expandOrDescend()
	case "left", "h":
		m.ascendOrCollapse()
	case "e":
		// Toggle the selected node's subtree (session or tab).
		m.toggleExpandAtCursor()
	case " ":
		m.toggleSelected()
	case "enter":
		return m.openSelected()
	case "n":
		if m.filter == filterWorktrees {
			return m, m.beginWorktreeCreate()
		}
		if len(m.selected) == 0 && len(m.rows) > 0 {
			entry := m.entries[m.rows[m.cursor].entryIndex]
			if entry.kind == "project" || entry.kind == "ssh" {
				m.selected = map[string]bool{entry.key: true}
			}
		}
		if len(m.selected) == 0 {
			m.err = fmt.Errorf("select at least one project or SSH host first")
			return m, nil
		}
		m.activateMode(modeCreateSession)
		m.createValue = ""
		m.err = nil
		return m, nil
	case "o":
		return m, m.openWorktreePR()
	case "O":
		// Bypass a project's .kesh.yaml and open its folder as one plain window.
		if len(m.rows) == 0 {
			m.err = fmt.Errorf("no entry selected")
			return m, nil
		}
		selected := m.rows[m.cursor]
		entry := m.entries[selected.entryIndex]
		if selected.section != "" || selected.tabIndex >= 0 || entry.kind != "project" || entry.open {
			m.err = fmt.Errorf("select an unopened project folder")
			return m, nil
		}
		return m, runAction(m.kitty, m.zoxide, entry, selected)
	case "c":
		if m.cloneBaseRoot == "" {
			m.err = fmt.Errorf("clone root is not configured")
			return m, nil
		}
		m.activateMode(modeClone)
		m.cloneRoot = m.cloneBaseRoot
		m.cloneRepository = ""
		m.cloneDestination = displayPath(m.cloneBaseRoot, os.Getenv("HOME"))
		m.cloneDestinationFocus = false
		m.cloneDestinationEdited = false
		m.err = nil
		return m, nil
	case "C":
		if m.cloneBaseRoot == "" {
			m.err = fmt.Errorf("clone root is not configured")
			return m, nil
		}
		m.activateMode(modeCheckoutPR)
		m.prCheckoutValue = ""
		m.prCheckoutBranch = ""
		m.prCheckoutPath = ""
		m.prCheckoutPathFocus = false
		m.prCheckoutPathEdited = false
		m.prCheckoutClone = false
		if len(m.rows) > 0 && m.cursor >= 0 && m.cursor < len(m.rows) {
			m.prCheckoutPath = displayPath(m.entries[m.rows[m.cursor].entryIndex].path, os.Getenv("HOME"))
		}
		m.checkoutCloneRoot = m.cloneBaseRoot
		m.checkoutRoot = m.cloneBaseRoot
		m.err = nil
		return m, nil
	case "r":
		if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
			return m, fetchOriginThenReload(m.entries[m.worktreeFilterEntryIndex].path, m.worktreeFilterEntryIndex)
		}
		m.beginRename()
	case "s", "S":
		if len(m.rows) == 0 {
			return m, nil
		}
		selected := m.rows[m.cursor]
		entry := m.entries[selected.entryIndex]
		if !entry.open {
			m.err = fmt.Errorf("save an open project or workspace")
			return m, nil
		}
		if entry.session == "" {
			m.err = fmt.Errorf("unnamed workspaces cannot be saved yet")
			return m, nil
		}
		m.activateMode(modeSaveConfirm)
		m.saveForeground = key == "S"
		m.saveEntry = selected.entryIndex
		// Saving a snapshot has an editable display name. Use the user-facing
		// entry name rather than Kitty's generated internal session identifier.
		m.saveName = entry.name
		m.err = nil
		return m, nil
	case "g":
		m.pendingG = true
	case "x":
		if m.filter == filterWorktrees && len(m.wtBulkSelected) > 0 {
			// Bulk remove every selected worktree. The tab list can be a
			// filtered subset, so resolve targets from worktreeFilterRows by
			// path. Route through the confirm popup like the single x.
			targets := m.selectedWorktreeItems()
			if len(targets) == 0 {
				m.err = fmt.Errorf("no matching worktrees to remove")
				return m, nil
			}
			m.activateMode(modeCloseConfirm)
			m.bulkWorktrees = targets
			m.closeRow = row{
				entryIndex:  m.worktreeFilterEntryIndex,
				tabIndex:    -1,
				windowIndex: -1,
				section:     "wt-bulk",
			}
			m.closeBusy = false
			m.worktreeForcePrompt = false
			m.mergedWorktrees = nil
			m.destroyPlan = nil
			m.err = nil
			return m, nil
		}
		if m.filter == filterWorktrees && len(m.worktreeFilterRows) > 0 && m.cursor < len(m.worktreeFilterRows) {
			// Route through the confirm popup (like main-mode x) instead of
			// force-removing immediately. The tab list can be a filtered
			// subset, so address the worktree by path, not by wt index.
			wt := m.worktreeFilterRows[m.cursor].worktree
			m.activateMode(modeCloseConfirm)
			m.closeRow = row{
				entryIndex:   m.worktreeFilterEntryIndex,
				tabIndex:     -1,
				windowIndex:  -1,
				section:      "wt-filter",
				worktreePath: wt.path,
			}
			m.closeBusy = false
			m.worktreeForcePrompt = false
			m.mergedWorktrees = nil
			m.err = nil
			return m, nil
		}
		if m.filter == filterSaved && len(m.rows) > 0 {
			selected := m.rows[m.cursor]
			m.activateMode(modeCloseConfirm)
			m.closeRow = selected
			m.closeBusy = false
			m.unsave = true
			m.worktreeForcePrompt = false
			m.mergedWorktrees = nil
			m.destroyPlan = nil
			m.err = nil
			return m, nil
		}
		m.beginClose()
	case "X":
		return m, m.findMergedWorktrees()
	case "D":
		if m.filter == filterWorktrees && len(m.worktreeFilterRows) > 0 && m.cursor >= 0 && m.cursor < len(m.worktreeFilterRows) {
			// In the Worktree tab, destroy the selected worktree only — not
			// the whole project. The tab list can be a filtered subset, so
			// resolve the worktree from worktreeFilterRows, not by index.
			wt := m.worktreeFilterRows[m.cursor].worktree
			branch := wt.branch
			if branch == "" || branch == "(detached)" {
				branch = ""
			}
			plan := destroyPlan{entryName: wt.branch, worktreePath: wt.path, branch: branch}
			m.activateMode(modeCloseConfirm)
			m.closeRow = row{
				entryIndex:   m.worktreeFilterEntryIndex,
				tabIndex:     -1,
				windowIndex:  -1,
				section:      "wt-filter",
				worktreePath: wt.path,
			}
			m.destroyPlan = &plan
			m.closeBusy = false
			m.worktreeForcePrompt = false
			m.mergedWorktrees = nil
			m.err = nil
			return m, nil
		}
		if len(m.rows) == 0 {
			return m, nil
		}
		selected := m.rows[m.cursor]
		entry := m.entries[selected.entryIndex]
		m.destroyPlanning = true
		m.err = nil
		return m, planDestroy(entry)
	case "w":
		// Worktrees need a real repository directory. A window row supplies its
		// current working directory; an unopened project row supplies its folder.
		// Session and tab rows are labels, not repository targets.
		if len(m.rows) == 0 {
			m.err = fmt.Errorf("no entry selected")
			return m, nil
		}
		selected := m.rows[m.cursor]
		entry := m.entries[selected.entryIndex]
		windowRow := selected.windowIndex >= 0
		unopenedFolder := selected.section == "" && selected.tabIndex < 0 && selected.windowIndex < 0 && entry.kind == "project" && !entry.open
		if selected.section != "" || (!windowRow && !unopenedFolder) {
			m.err = fmt.Errorf("place the cursor on a window or unopened project folder")
			return m, nil
		}
		m.previousFilter = m.filter
		m.worktreeFilterEntryIndex = selected.entryIndex
		m.filter = filterWorktrees
		m.query = ""
		m.worktreeFilterRows = nil

		// Never render stale rows from the originating list under the Worktrees
		// header. Paint an explicit loading state while the lightweight list loads.
		if !entry.worktreesLoaded {
			m.rows = nil
			m.cursor = 0
			m.worktreeLoading = true
			return m, fetchWorktrees(entry.path, selected.entryIndex, -1, -1)
		}
		m.worktreeLoading = false
		m.rebuildWorktreeRows()
		return m, nil
	case "f":
		if m.filter == filterWorktrees && len(m.wtBulkSelected) > 0 {
			if m.worktreePullBusy {
				return m, nil
			}
			targets := m.selectedWorktreeItems()
			if len(targets) == 0 {
				m.err = fmt.Errorf("no matching worktrees to pull")
				return m, nil
			}
			entry := m.entries[m.worktreeFilterEntryIndex]
			m.worktreePullBusy = true
			m.err = nil
			return m, pullWorktrees(targets, entry.path, m.worktreeFilterEntryIndex)
		}
		if m.filter == filterWorktrees && m.cursor >= 0 && m.cursor < len(m.worktreeFilterRows) {
			if m.worktreePullBusy {
				return m, nil
			}
			wt := m.worktreeFilterRows[m.cursor].worktree
			entry := m.entries[m.worktreeFilterEntryIndex]
			m.worktreePullBusy = true
			m.err = nil
			return m, pullWorktree(wt.path, entry.path, m.worktreeFilterEntryIndex)
		}
	case "p":
		if m.hasSelectedAgentWindow() {
			m.showPreview = !m.showPreview
			if m.showPreview {
				m.previewID = 0
			}
		} else {
			m.beginPin()
		}
	case "tab", "shift+tab":
		// Worktrees is a drill-in surface, not a cycle filter: ignore Tab there
		// and let esc return to the originating filter.
		if m.filter == filterWorktrees {
			return m, m.queuePreview()
		}
		step := 1
		if key == "shift+tab" {
			step = -1
		}
		m.filter = cycleFilter(m.filter, step)
		m.rebuildRows()
	}
	return m, m.queuePreview()
}

func (m model) openSelected() (tea.Model, tea.Cmd) {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return m, nil
	}
	selected := m.rows[m.cursor]
	if m.filter == filterWorktrees && selected.section == "wt-filter" {
		if m.cursor >= len(m.worktreeFilterRows) {
			return m, nil
		}
		worktree := m.worktreeFilterRows[m.cursor].worktree
		entry := m.entries[m.worktreeFilterEntryIndex]
		return m, findWorktreeWindow(m.kitty, entry.key, worktree.path)
	}
	entry := m.entries[selected.entryIndex]
	unopenedProject := selected.section == "" && selected.tabIndex < 0 && selected.windowIndex < 0 && entry.kind == "project" && !entry.open
	if unopenedProject {
		return m, m.beginLaunchLayoutForEntry(entry)
	}
	action := runAction(m.kitty, m.zoxide, entry, selected)
	if selected.windowIndex >= 0 {
		window := entry.tabs[selected.tabIndex].windows[selected.windowIndex]
		if window.agentStatus == "finished" || window.agentStatus == "errored" {
			m.acknowledgeAgentStatus(window.id)
			action = acknowledgeThen(m.agentStatusDir, window.id, action)
		}
	}
	return m, action
}

func findWorktreeWindow(kitty, entryKey, path string) tea.Cmd {
	return func() tea.Msg {
		windowIDs, err := worktreeWindowIDs(kitty, path)
		return worktreeOpenMsg{entryKey: entryKey, path: path, windowIDs: windowIDs, err: err}
	}
}

func planDestroy(entry entry) tea.Cmd {
	return func() tea.Msg {
		return destroyPlanMsg{entryKey: entry.key, plan: detectDestroyPlan(entry)}
	}
}
