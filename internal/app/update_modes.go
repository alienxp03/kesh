package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.saving || m.mergedWorktreeBusy || m.destroyPlanning {
		return m, nil
	}
	switch m.mode {
	case modeHelp:
		return m.updateHelpKey(msg)
	case modeSaveConfirm:
		return m.updateSaveConfirmKey(msg)
	case modePin:
		return m.updatePinKey(msg)
	case modeCreateSession:
		return m.updateCreateSessionKey(msg)
	case modeRename:
		return m.updateRenameKey(msg)
	case modeSearch:
		return m.updateSearchKey(msg)
	case modeCloseConfirm:
		return m.updateCloseConfirmKey(msg)
	case modeClone:
		return m.updateCloneKey(msg)
	case modeCheckoutPR:
		return m.updateCheckoutKey(msg)
	case modeWorktreeCreate:
		return m.updateWorktreeCreateKey(msg)
	default:
		return m.updateNormalKey(msg)
	}
}

func (m model) updateHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "?", "q":
		m.cancelMode()
	case "up", "k", "ctrl+k":
		m.helpScroll = max(0, m.helpScroll-1)
	case "down", "j", "ctrl+j":
		m.helpScroll++
	case "pgup", "ctrl+u":
		m.helpScroll = max(0, m.helpScroll-5)
	case "pgdown", "ctrl+d":
		m.helpScroll += 5
	case "home", "g":
		m.helpScroll = 0
	case "end", "G":
		m.helpScroll = 1 << 30
	}
	return m, nil
}

func (m model) updateCloseConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.closeBusy {
		return m, nil
	}
	isWorktreeRow := m.closeRow.section == "wt-filter"
	switch msg.String() {
	case "esc":
		m.cancelMode()
		m.err = nil
		return m, nil
	case "y":
		if m.destroyPlan != nil {
			m.closeBusy = true
			m.err = nil
			selected := m.closeRow
			return m, runDestroy(m.kitty, m.zoxide, m.entries[selected.entryIndex], *m.destroyPlan)
		}
		if len(m.mergedWorktrees) > 0 {
			if m.worktreeForcePrompt {
				m.err = fmt.Errorf("press f to force removal, or esc to cancel")
				return m, nil
			}
			m.closeBusy = true
			m.err = nil
			return m, m.runRemoveMergedWorktrees(false)
		}
		if m.closeRow.section == "wt-bulk" {
			m.closeBusy = true
			m.err = nil
			return m, m.runRemoveWorktrees()
		}
		if isWorktreeRow {
			if m.worktreeForcePrompt {
				m.err = fmt.Errorf("press f to force, or esc to cancel")
				return m, nil
			}
			m.closeBusy = true
			m.err = nil
			return m, m.runRemoveWorktree(false)
		}
		m.closeBusy = true
		m.err = nil
		selected := m.closeRow
		if m.unsave {
			return m, runUnsave(m.kitty, m.zoxide, m.entries[selected.entryIndex], selected.entryIndex)
		}
		return m, runClose(m.kitty, m.zoxide, m.entries[selected.entryIndex], selected)
	case "f":
		if len(m.mergedWorktrees) > 0 && m.worktreeForcePrompt {
			m.closeBusy = true
			m.err = nil
			return m, m.runRemoveMergedWorktrees(true)
		}
		if isWorktreeRow {
			m.closeBusy = true
			m.err = nil
			return m, m.runRemoveWorktree(true)
		}
		m.err = fmt.Errorf("press y to confirm or esc to cancel")
	default:
		m.err = fmt.Errorf("press y to confirm or esc to cancel")
	}
	return m, nil
}

func (m model) updateCloneKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.cloneBusy {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.resetClone()
	case "tab", "shift+tab":
		m.cloneDestinationFocus = !m.cloneDestinationFocus
		m.err = nil
	case "enter":
		if _, err := repositoryName(m.cloneRepository); err != nil {
			m.err = err
			return m, nil
		}
		destination, err := resolveCloneDestination(m.cloneDestination, m.cloneRoot)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.cloneBusy = true
		m.err = nil
		return m, runClone(m.kitty, m.zoxide, m.cloneRepository, destination)
	case "backspace":
		value := &m.cloneRepository
		if m.cloneDestinationFocus {
			value = &m.cloneDestination
			m.cloneDestinationEdited = true
		}
		runes := []rune(*value)
		if len(runes) > 0 {
			*value = string(runes[:len(runes)-1])
		}
		m.refreshCloneDestination()
	case "ctrl+u":
		if m.cloneDestinationFocus {
			m.cloneDestination = ""
			m.cloneDestinationEdited = true
		} else {
			m.cloneRepository = ""
		}
		m.refreshCloneDestination()
	default:
		if len(msg.Runes) > 0 && !msg.Alt {
			if m.cloneDestinationFocus {
				m.cloneDestination += string(msg.Runes)
				m.cloneDestinationEdited = true
			} else {
				m.cloneRepository += string(msg.Runes)
			}
			m.refreshCloneDestination()
		}
	}
	return m, nil
}

func (m model) updateCheckoutKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.prCheckoutBusy {
		return m, nil
	}
	previousValue := m.prCheckoutValue
	switch msg.String() {
	case "esc":
		m.cancelMode()
		m.err = nil
		return m, nil
	case "enter":
		owner, repo, number, useSelected, err := parsePullRequestInput(m.prCheckoutValue)
		if err != nil {
			m.err = err
			return m, nil
		}
		selectedRepoPath := ""
		if useSelected {
			if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
				m.err = fmt.Errorf("select a project or paste a full PR URL")
				return m, nil
			}
			selectedRepoPath = m.entries[m.rows[m.cursor].entryIndex].path
			if selectedRepoPath == "" {
				m.err = fmt.Errorf("select a project or paste a full PR URL")
				return m, nil
			}
		}
		m.prCheckoutBusy = true
		m.err = nil
		return m, runCheckoutPR(m.kitty, m.zoxide, owner, repo, number, selectedRepoPath, m.checkoutRoot, m.checkoutCloneRoot)
	case "backspace":
		runes := []rune(m.prCheckoutValue)
		if len(runes) > 0 {
			m.prCheckoutValue = string(runes[:len(runes)-1])
			m.err = nil
		}
	case "ctrl+u":
		m.prCheckoutValue = ""
		m.err = nil
	default:
		if len(msg.Runes) > 0 && !msg.Alt {
			m.prCheckoutValue += string(msg.Runes)
			m.err = nil
		}
	}
	if m.prCheckoutValue == previousValue {
		return m, nil
	}
	m.prCheckoutBranch = ""
	m.prCheckoutPath = ""
	m.prCheckoutClone = false
	if owner, repo, number, selected, err := parsePullRequestInput(m.prCheckoutValue); err == nil && !selected {
		return m, resolvePRPreview(m.prCheckoutValue, owner, repo, number, m.checkoutRoot, m.checkoutCloneRoot)
	}
	return m, nil
}

func (m model) updateWorktreeCreateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.worktreeBusy {
		return m, nil
	}
	key := msg.String()
	switch key {
	case "esc":
		m.cancelMode()
		m.err = nil
	case "tab", "shift+tab":
		if m.worktreeRecipe != nil {
			modes := []string{"native", "workspaces"}
			current := "workspaces"
			if m.worktreeRecipeMode == "none" {
				current = "native"
			}
			index := 0
			for i, mode := range modes {
				if mode == current {
					index = i
					break
				}
			}
			if key == "shift+tab" {
				index = (index - 1 + len(modes)) % len(modes)
			} else {
				index = (index + 1) % len(modes)
			}
			switch modes[index] {
			case "native":
				m.worktreeRecipeMode = "none"
				m.worktreeCustomWorkspaces = false
			case "workspaces":
				if len(m.worktreeRecipe.Workspaces) > 1 {
					m.worktreeRecipeMode = "selected"
					m.worktreeCustomWorkspaces = true
					m.ensureWorktreeSelection()
				} else {
					m.worktreeRecipeMode = "single"
					m.worktreeCustomWorkspaces = false
				}
			}
		}
	case "up", "ctrl+k":
		if m.worktreeCustomWorkspaces && m.worktreeWorkspaceCursor > 0 {
			m.worktreeWorkspaceCursor--
		}
	case "down", "ctrl+j":
		if m.worktreeCustomWorkspaces && m.worktreeWorkspaceCursor < len(m.worktreeSelected)-1 {
			m.worktreeWorkspaceCursor++
		}
	case "enter":
		if m.launchOnFolder {
			return m.confirmLaunchLayout()
		}
		if m.worktreeBranch == "" {
			m.err = fmt.Errorf("branch name is required")
			return m, nil
		}
		m.err = nil
		if m.worktreeRecipe != nil && m.worktreeRecipeMode != "none" {
			var selected []string
			if m.worktreeRecipeMode == "selected" {
				selected = m.selectedWorkspaceNames()
				if len(selected) == 0 {
					m.err = fmt.Errorf("select at least one workspace")
					return m, nil
				}
			}
			m.worktreeBusy = true
			return m, runWktreeNew(m.worktreeRecipePath, m.worktreeRecipeMode, m.worktreeBranch, selected)
		}
		return m, m.validateWorktreeBranch()
	case "backspace":
		if m.launchOnFolder {
			runes := []rune(m.worktreeSessionName)
			if len(runes) > 0 {
				m.worktreeSessionName = string(runes[:len(runes)-1])
				m.err = nil
			}
		} else {
			runes := []rune(m.worktreeBranch)
			if len(runes) > 0 {
				m.worktreeBranch = string(runes[:len(runes)-1])
				m.worktreePaths = m.calculateWorktreePaths()
				m.err = nil
			}
		}
	case "ctrl+u":
		if m.launchOnFolder {
			m.worktreeSessionName = ""
		} else {
			m.worktreeBranch = ""
			m.worktreePaths = m.calculateWorktreePaths()
		}
		m.err = nil
	default:
		if m.worktreeCustomWorkspaces && key == " " && len(m.worktreeSelected) > 0 {
			m.worktreeSelected[m.worktreeWorkspaceCursor] = !m.worktreeSelected[m.worktreeWorkspaceCursor]
			return m, nil
		}
		if len(msg.Runes) > 0 && !msg.Alt && !msg.Paste {
			if m.launchOnFolder {
				m.worktreeSessionName += string(msg.Runes)
			} else {
				m.worktreeBranch += string(msg.Runes)
				m.worktreePaths = m.calculateWorktreePaths()
			}
			m.err = nil
		}
	}
	return m, nil
}

func (m model) updateSaveConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.saveForeground {
		switch msg.String() {
		case "esc":
			m.cancelMode()
			m.err = nil
		case "y":
			entryIndex := m.saveEntry
			entry := m.entries[entryIndex]
			m.cancelMode()
			m.saving = true
			m.err = nil
			return m, runSaveSession(m.kitty, entry, entryIndex, entry.name, true)
		default:
			m.err = fmt.Errorf("press y to confirm or esc to cancel")
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		m.cancelMode()
		m.err = nil
	case "enter":
		name := strings.TrimSpace(m.saveName)
		if name == "" {
			m.err = fmt.Errorf("saved session name is required")
			return m, nil
		}
		entryIndex := m.saveEntry
		entry := m.entries[entryIndex]
		m.cancelMode()
		m.saving = true
		m.err = nil
		return m, runSaveSession(m.kitty, entry, entryIndex, name, false)
	case "backspace":
		runes := []rune(m.saveName)
		if len(runes) > 0 {
			m.saveName = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		m.saveName = ""
	default:
		if len(msg.Runes) > 0 && !msg.Alt && !msg.Paste {
			m.saveName += string(msg.Runes)
		}
	}
	return m, nil
}

func (m model) updatePinKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pinBusy {
		return m, nil
	}
	key := msg.String()
	switch {
	case key == "esc":
		m.cancelMode()
		m.err = nil
	case key == "x":
		return m, m.unpinSelected()
	case validSlot(key):
		return m, m.assignPin(key)
	default:
		m.err = fmt.Errorf("pin slot must be a digit from 0 to 9")
	}
	return m, nil
}

func (m model) updateCreateSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cancelMode()
		m.err = nil
	case "enter":
		name := safeName(m.createValue)
		if name == "" {
			m.err = fmt.Errorf("session name is required")
		} else {
			return m, runCreateSession(m.kitty, m.selectedEntries(), name)
		}
	case "backspace":
		runes := []rune(m.createValue)
		if len(runes) > 0 {
			m.createValue = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		m.createValue = ""
	default:
		if len(msg.Runes) > 0 && !msg.Alt && !msg.Paste {
			m.createValue += string(msg.Runes)
		}
	}
	return m, nil
}

func (m model) updateRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cancelMode()
	case "enter":
		if len(m.rows) > 0 {
			selected := m.rows[m.cursor]
			return m, runRename(m.kitty, m.entries[selected.entryIndex], selected, m.renameValue, m.names)
		}
	case "backspace":
		runes := []rune(m.renameValue)
		if len(runes) > 0 {
			m.renameValue = string(runes[:len(runes)-1])
		}
	case "ctrl+u":
		m.renameValue = ""
	default:
		if len(msg.Runes) > 0 && !msg.Alt && !msg.Paste {
			m.renameValue += string(msg.Runes)
		}
	}
	return m, nil
}

func (m model) updateSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cancelMode()
	case "enter":
		m.cancelMode()
		return m.openSelected()
	case "up", "ctrl+k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "ctrl+j":
		if m.cursor+1 < len(m.rows) {
			m.cursor++
		}
	case " ":
		m.query += " "
		m.rebuildRows()
	case "backspace":
		runes := []rune(m.query)
		if len(runes) > 0 {
			m.query = string(runes[:len(runes)-1])
			m.rebuildRows()
		}
	case "ctrl+u":
		m.query = ""
		m.rebuildRows()
	default:
		if len(msg.Runes) > 0 && !msg.Alt && !msg.Paste {
			m.query += string(msg.Runes)
			m.rebuildRows()
		}
	}
	return m, m.queuePreview()
}
