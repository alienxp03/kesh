package app

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) toggleSelected() {
	if len(m.rows) == 0 {
		return
	}
	// In the Worktree tab, space bulk-selects worktree paths for x (remove)
	// and p (pull) instead of toggling project selection.
	if m.filter == filterWorktrees && m.cursor >= 0 && m.cursor < len(m.worktreeFilterRows) {
		wt := m.worktreeFilterRows[m.cursor].worktree
		if wt.path == "" {
			return
		}
		if m.wtBulkSelected == nil {
			m.wtBulkSelected = map[string]bool{}
		}
		if m.wtBulkSelected[wt.path] {
			delete(m.wtBulkSelected, wt.path)
		} else {
			m.wtBulkSelected[wt.path] = true
		}
		m.err = nil
		return
	}
	r := m.rows[m.cursor]
	if r.tabIndex >= 0 || (m.entries[r.entryIndex].kind != "project" && m.entries[r.entryIndex].kind != "ssh") {
		m.err = fmt.Errorf("select a source project or SSH host, not a workspace, tab, or window")
		return
	}
	key := m.entries[r.entryIndex].key
	if m.selected == nil {
		m.selected = map[string]bool{}
	}
	if m.selected[key] {
		delete(m.selected, key)
	} else {
		m.selected[key] = true
	}
	m.err = nil
}

func (m model) selectedEntries() []entry {
	entries := make([]entry, 0, len(m.selected))
	for _, candidate := range m.entries {
		if m.selected[candidate.key] {
			entries = append(entries, candidate)
		}
	}
	return entries
}

// resetWorktreeTab clears the Worktree tab's project context and selection. The
// tab is only meaningful when a project is chosen with w; arriving by cycling
// Tab/Shift+tab carries no selection, so it must render empty with no folder
// name rather than reusing a stale (or default 0) entry index.
func (m *model) resetWorktreeTab() {
	m.worktreeFilterEntryIndex = -1
	m.worktreeFilterRows = nil
	m.wtBulkSelected = nil
}

// worktreeEntries resolves the projects a worktree action targets. In the
// Worktree tab it is the project whose tab is open; otherwise selection drives
// multi-project worktrees, and with nothing selected the project under the
// cursor is used so a single worktree needs no explicit selection.
func (m *model) worktreeEntries() []entry {
	if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
		if e := m.entries[m.worktreeFilterEntryIndex]; e.kind == "project" && e.path != "" {
			return []entry{e}
		}
	}
	if len(m.selected) > 0 {
		return m.selectedEntries()
	}
	if len(m.rows) == 0 {
		return nil
	}
	e := m.entries[m.rows[m.cursor].entryIndex]
	if e.kind != "project" {
		return nil
	}
	return []entry{e}
}

func (m *model) beginClose() {
	if len(m.rows) == 0 {
		return
	}
	selected := m.rows[m.cursor]
	entry := m.entries[selected.entryIndex]
	if selected.tabIndex < 0 && len(entry.tabs) == 0 && !(entry.saved && !entry.open) {
		m.err = fmt.Errorf("%s is not open", entry.name)
		return
	}
	m.activateMode(modeCloseConfirm)
	m.closeRow = selected
	m.closeBusy = false
	m.worktreeForcePrompt = false
	m.mergedWorktrees = nil
	m.err = nil
}

func (m *model) refreshCloneDestination() {
	if m.cloneDestinationEdited {
		return
	}
	m.cloneDestination = displayPath(m.cloneRoot, os.Getenv("HOME"))
	if name, err := repositoryName(m.cloneRepository); err == nil {
		m.cloneDestination = displayPath(filepath.Join(m.cloneRoot, name), os.Getenv("HOME"))
	}
}

func (m *model) resetClone() {
	m.cancelMode()
	m.err = nil
}

func (m *model) beginPin() {
	if len(m.rows) == 0 {
		return
	}
	selected := m.rows[m.cursor]
	m.activateMode(modePin)
	m.pinEntry = selected.entryIndex
	m.confirmSlot = ""
	m.err = nil
}

func (m *model) assignPin(slot string) tea.Cmd {
	selected := m.entries[m.pinEntry]
	if occupied, ok := m.pins[slot]; ok && occupied.Key != selected.key && m.confirmSlot != slot {
		m.confirmSlot = slot
		m.err = fmt.Errorf("slot %s is pinned to %s; press %s again to replace it", slot, occupied.Name, slot)
		return nil
	}
	current := copyPins(m.pins)
	m.pinBusy = true
	m.err = nil
	return func() tea.Msg {
		updated := make(pinStore, len(current)+1)
		for existingSlot, target := range current {
			if target.Key != selected.key && existingSlot != slot {
				updated[existingSlot] = target
			}
		}
		target, err := pinTargetForEntry(selected)
		if err == nil {
			updated[slot] = target
			err = persistPins(m.kitty, updated)
		}
		return pinsPersistMsg{pins: updated, finishPin: true, err: err}
	}
}

func copyPins(pins pinStore) pinStore {
	copied := make(pinStore, len(pins))
	for existingSlot, target := range pins {
		copied[existingSlot] = target
	}
	return copied
}

func persistPins(kitty string, pins pinStore) error {
	if err := savePins(pins); err != nil {
		return err
	}
	if err := syncPinShortcuts(kitty, pins); err != nil {
		return err
	}
	return nil
}

func persistPinsCommand(kitty string, pins pinStore, finishPin bool) tea.Cmd {
	pins = copyPins(pins)
	return func() tea.Msg {
		return pinsPersistMsg{pins: pins, finishPin: finishPin, err: persistPins(kitty, pins)}
	}
}

func (m *model) unpinSelected() tea.Cmd {
	selected := m.entries[m.pinEntry]
	updated := make(pinStore, len(m.pins))
	for slot, target := range m.pins {
		if target.Key != selected.key {
			updated[slot] = target
		}
	}
	if len(updated) == len(m.pins) {
		m.err = fmt.Errorf("%s is not pinned", selected.name)
		return nil
	}
	m.pinBusy = true
	m.err = nil
	return persistPinsCommand(m.kitty, updated, true)
}

func (m *model) beginRename() {
	if len(m.rows) == 0 {
		return
	}
	selected := m.rows[m.cursor]
	entry := &m.entries[selected.entryIndex]
	if selected.windowIndex >= 0 {
		m.activateMode(modeRename)
		m.renameValue = entry.tabs[selected.tabIndex].windows[selected.windowIndex].title
		m.err = nil
		return
	}
	if selected.tabIndex >= 0 {
		m.activateMode(modeRename)
		m.renameValue = entry.tabs[selected.tabIndex].title
		m.err = nil
		return
	}
	if entry.kind == "project" {
		m.err = fmt.Errorf("rename an open workspace, not its source project")
		return
	}
	m.activateMode(modeRename)
	m.renameValue = entry.name
	m.err = nil
}
