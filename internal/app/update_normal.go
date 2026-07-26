package app

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) updateNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
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
	case "home":
		m.cursor = 0
	case "end", "G":
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
		if len(m.rows) == 0 {
			return m, nil
		}
		r := m.rows[m.cursor]
		if m.filter == filterWorktrees && r.section == "wt-filter" {
			// Focus the worktree
			if m.cursor >= len(m.worktreeFilterRows) {
				return m, nil
			}
			wtRow := m.worktreeFilterRows[m.cursor]
			wt := wtRow.worktree
			entry := m.entries[m.worktreeFilterEntryIndex]
			return m, findWorktreeWindow(m.kitty, entry.key, wt.path)
		}
		return m, runAction(m.kitty, m.zoxide, m.entries[r.entryIndex], r)
	case "n":
		if m.filter == filterWorktrees {
			return m, m.beginWorktreeCreate()
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
		// Launch the project's .kesh.yaml layout on its existing folders — no
		// worktree. Additive to Enter (bare folder open); requires a project
		// entry under the cursor, not a worktree or window row.
		if len(m.rows) == 0 {
			m.err = fmt.Errorf("no entry selected")
			return m, nil
		}
		selected := m.rows[m.cursor]
		if selected.section != "" {
			m.err = fmt.Errorf("select a project entry, not a worktree or window")
			return m, nil
		}
		return m, m.beginLaunchLayout()
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
		if m.cloneBaseRoot == "" || m.checkoutBaseRoot == "" {
			m.err = fmt.Errorf("checkout roots are not configured")
			return m, nil
		}
		m.activateMode(modeCheckoutPR)
		m.prCheckoutValue = ""
		m.prCheckoutBranch = ""
		m.checkoutCloneRoot = m.cloneBaseRoot
		m.checkoutRoot = m.checkoutBaseRoot
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
		m.err = nil
		return m, nil
	case "g":
		if m.filter == filterWorktrees && len(m.worktreeFilterRows) > 0 && m.cursor < len(m.worktreeFilterRows) {
			wt := m.worktreeFilterRows[m.cursor].worktree
			if wt.prURL != "" {
				prURL := wt.prURL
				return m, func() tea.Msg {
					return openPRMsg{err: openURL(prURL)}
				}
			}
			m.err = fmt.Errorf("no PR associated with this worktree")
			return m, nil
		}
		if m.filter == filterWorktrees {
			return m, m.openWorktreePR()
		}
		// vim-style: jump to the top of the list.
		m.cursor = 0
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
		// Open the Worktree tab for the project under the cursor.
		if len(m.rows) == 0 {
			m.err = fmt.Errorf("no entry selected")
			return m, nil
		}
		selected := m.rows[m.cursor]
		if selected.section != "" {
			m.err = fmt.Errorf("select a project entry, not a worktree or window")
			return m, nil
		}
		m.previousFilter = m.filter
		m.worktreeFilterEntryIndex = selected.entryIndex
		m.filter = filterWorktrees
		m.query = ""
		m.worktreeFilterRows = nil

		// Fetch worktrees if not already loaded
		entry := m.entries[selected.entryIndex]
		if !entry.worktreesLoaded {
			return m, fetchWorktrees(entry.path, selected.entryIndex, -1, -1)
		}
		m.rebuildWorktreeRows()
		return m, nil
	case "p":
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
