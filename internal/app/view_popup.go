package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m model) popupView(width int) string {
	if m.mode != modeHelp && m.mode != modeRename && m.mode != modeCreateSession && m.mode != modeClone && m.mode != modeSaveConfirm && m.mode != modePin && m.mode != modeCloseConfirm && m.mode != modeWorktreeCreate && m.mode != modeCheckoutPR && !m.mergedWorktreeBusy && !m.worktreePullBusy {
		return ""
	}
	if m.mode == modeHelp {
		return m.helpPopupView(width, m.height)
	}
	popupWidth := min(50, max(28, width-10))
	if m.mode == modeClone || (m.mode == modeSaveConfirm && m.saveForeground) || m.mode == modeWorktreeCreate || (m.mode == modeCloseConfirm && m.closeRow.section == "wt-filter") {
		popupWidth = min(72, max(36, width-6))
	}
	if m.mode == modeCloseConfirm && m.destroyPlan != nil {
		popupWidth = min(64, max(36, width-6))
	}
	// PR URLs and worktree paths are often long. Let this form use the available
	// terminal width instead of leaving an artificial empty right-hand column.
	if m.mode == modeCheckoutPR {
		popupWidth = min(100, max(36, width-6))
	}
	var title, field, help string
	if m.worktreePullBusy {
		title = "Syncing worktree"
		field = "Running git pull --rebase…"
		help = "Updates the worktree's branch from its upstream"
	} else if m.mergedWorktreeBusy {
		title = "Checking merged worktrees"
		field = "Querying GitHub for current PR status…"
		help = "This always uses live data before removal"
	} else if m.mode == modeSaveConfirm {
		entry := m.entries[m.saveEntry]
		if m.saveForeground {
			title = "Save with running commands"
			lines := []string{fmt.Sprintf("Save %q and rerun foreground commands when restored?", entry.name)}
			commands := workspaceForegroundCommands(entry)
			if len(commands) == 0 {
				lines = append(lines, "", dimStyle.Render("No non-shell foreground commands detected."))
			} else {
				lines = append(lines, "", "Restoring will rerun:")
				for index, command := range commands {
					if index == 4 {
						lines = append(lines, fmt.Sprintf("  • …and %d more", len(commands)-index))
						break
					}
					lines = append(lines, "  • "+truncate(command, popupWidth-12))
				}
			}
			field = lipgloss.NewStyle().Width(popupWidth - 6).Render(strings.Join(lines, "\n"))
		} else {
			title = "Save workspace"
			field = selectedStyle.Width(popupWidth - 6).Render(m.saveName + "█")
			help = "Enter save  •  Esc cancel"
		}
	} else if m.mode == modeClone {
		title = "Clone repository"
		repositoryCursor := ""
		destinationCursor := ""
		if !m.cloneBusy {
			if m.cloneDestinationFocus {
				destinationCursor = "█"
			} else {
				repositoryCursor = "█"
			}
		}
		repositoryValueStyle := focusStyle
		destinationValueStyle := focusStyle
		if !m.cloneBusy && !m.cloneDestinationFocus {
			repositoryValueStyle = selectedTextStyle
		}
		if !m.cloneBusy && m.cloneDestinationFocus {
			destinationValueStyle = selectedTextStyle
		}
		fieldWidth := popupWidth - 6
		repositoryField := lipgloss.NewStyle().Width(fieldWidth).Render(
			dimStyle.Render("Repository: ") + repositoryValueStyle.Render(m.cloneRepository+repositoryCursor),
		)
		destinationField := lipgloss.NewStyle().Width(fieldWidth).Render(
			dimStyle.Render("Clone into: ") + destinationValueStyle.Render(m.cloneDestination+destinationCursor),
		)
		field = repositoryField + "\n\n" + destinationField
		if m.cloneBusy {
			help = "Cloning…"
		} else {
			help = "Tab switch field  •  Enter clone  •  Esc cancel"
		}
	} else if m.mode == modeCreateSession {
		title = fmt.Sprintf("Create session (%d tabs)", len(m.selected))
		field = selectedStyle.Width(popupWidth - 6).Render(m.createValue + "█")
		entries := m.selectedEntries()
		if len(entries) > 0 {
			lines := []string{dimStyle.Render("Projects:")}
			for _, entry := range entries {
				lines = append(lines, "  • "+entry.name)
			}
			field += "\n\n" + dimStyle.Render(strings.Join(lines, "\n"))
		}
		help = "Enter create  •  Esc cancel"
	} else if m.mode == modeRename {
		title = "Rename"
		field = selectedStyle.Width(popupWidth - 6).Render(m.renameValue + "█")
		help = "Enter save  •  Esc cancel"
	} else if m.mode == modeWorktreeCreate {
		launchAction := m.launchOnFolder
		if launchAction {
			title = "Launch layout"
		} else {
			title = "Create worktree"
		}
		cursor := "█"
		if m.worktreeBranch != "" && !m.worktreeBusy {
			cursor = ""
		}
		// Worktree-create types a branch; launch types a Kitty session name,
		// initially prefixed with the selected folder's name.
		branchField := dimStyle.Render("Branch: ") + focusStyle.Render(m.worktreeBranch+cursor)
		if launchAction {
			sessionCursor := "█"
			if m.worktreeBusy {
				sessionCursor = ""
			}
			branchField = dimStyle.Render("Session: ") + focusStyle.Render(m.worktreeSessionName+sessionCursor)
		}
		fieldWidth := popupWidth - 6

		var pathsField string
		if m.worktreeRecipe != nil {
			menuWidth := 18
			previewWidth := fieldWidth - menuWidth - 2
			if previewWidth < 18 {
				menuWidth = 14
				previewWidth = fieldWidth - menuWidth - 2
			}
			preview := ""
			switch {
			case launchAction && m.worktreeRecipeMode == "none":
				preview = dimStyle.Render("Opens the folder as a single window (no layout).")
			case !launchAction && m.worktreeRecipeMode == "none":
				preview = dimStyle.Render("Creates a plain Kesh worktree.")
			default:
				preview = dimStyle.Render("Template: " + displayPath(m.worktreeRecipePath, os.Getenv("HOME")))
				preview += "\n"
				selected := m.worktreePreviewSelection()
				preview += m.renderWorktreeChecklist(selected, m.worktreeCustomWorkspaces)
				layout := layoutPreview(m.worktreeRecipe, m.worktreeRecipePath, m.worktreeRecipeMode, previewWidth, selected)
				preview += "\n" + dimStyle.Render("Layout preview") + "\n" + dimStyle.Render(strings.Join(layout, "\n"))
			}
			pathsField = "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top,
				m.worktreeModeMenuView(menuWidth), "  ",
				lipgloss.NewStyle().Width(previewWidth).Render(preview),
			)
		} else if launchAction {
			pathsField = "\n\n" + dimStyle.Render("No .kesh.yaml — Enter opens the folder directly.")
		} else if len(m.worktreePaths) > 0 {
			label := "Preview"
			if len(m.worktreePaths) > 1 {
				label = fmt.Sprintf("Preview (%d)", len(m.worktreePaths))
			}
			pathsField = "\n\n" + dimStyle.Render(label+":") + "\n"
			prefix := "  󰉋 "
			hanging := strings.Repeat(" ", lipgloss.Width(prefix))
			wrapWidth := fieldWidth - lipgloss.Width(prefix)
			for i, path := range m.worktreePaths {
				if i >= 3 {
					pathsField += "\n  " + dimStyle.Render(fmt.Sprintf("…and %d more", len(m.worktreePaths)-i))
					break
				}
				wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(path)
				wrapped = strings.ReplaceAll(wrapped, "\n", "\n"+hanging)
				pathsField += "\n" + mutedStyle.Render(prefix+wrapped)
			}
		}

		field = lipgloss.NewStyle().Width(fieldWidth).Render(branchField + pathsField)
		actionVerb := "create"
		if launchAction {
			actionVerb = "launch"
		}
		if m.worktreeBusy {
			if launchAction {
				help = "Launching…"
			} else {
				help = "Creating…"
			}
		} else if m.worktreeRecipe != nil && m.worktreeCustomWorkspaces {
			help = "↑↓/Ctrl+J/K choose workspace  •  space toggle  •  Tab/Shift+Tab mode  •  Enter " + actionVerb + "  •  Esc cancel"
		} else if m.worktreeRecipe != nil {
			help = "Tab/Shift+Tab mode  •  Enter " + actionVerb + "  •  Esc cancel"
		} else {
			help = "Enter " + actionVerb + "  •  Esc cancel"
		}
	} else if m.mode == modeCheckoutPR {
		title = "Checkout pull request"
		fieldWidth := popupWidth - 6
		prCursor, pathCursor := "", ""
		if !m.prCheckoutBusy {
			if m.prCheckoutPathFocus {
				pathCursor = "█"
			} else {
				prCursor = "█"
			}
		}
		prStyle, pathStyle := focusStyle, focusStyle
		if !m.prCheckoutBusy && !m.prCheckoutPathFocus {
			prStyle = selectedTextStyle
		}
		if !m.prCheckoutBusy && m.prCheckoutPathFocus {
			pathStyle = selectedTextStyle
		}
		prField := lipgloss.NewStyle().Width(fieldWidth).Render(
			dimStyle.Render("PR: ") + prStyle.Render(m.prCheckoutValue+prCursor),
		)
		cloneNote := ""
		if m.prCheckoutClone && !m.prCheckoutPathEdited {
			cloneNote = dimStyle.Render(" (new clone)")
		}
		pathField := lipgloss.NewStyle().Width(fieldWidth).Render(
			dimStyle.Render("Root repo path: ") + pathStyle.Render(m.prCheckoutPath+pathCursor) + cloneNote,
		)
		previewField := ""
		if !m.prCheckoutBusy {
			previewField = prCheckoutPreview(m.prCheckoutValue, m.prCheckoutBranch, m.prCheckoutClone, m.checkoutCloneRoot, m.worktreeRoot, fieldWidth)
		}
		field = prField + "\n\n" + pathField + previewField
		if m.prCheckoutBusy {
			help = "Validating and checking out…"
		} else {
			help = "Tab switch field  •  Enter checkout  •  Esc cancel"
		}
	} else if m.mode == modePin {
		title = "Pin " + m.entries[m.pinEntry].name
		slot := "█"
		if m.confirmSlot != "" {
			slot = m.confirmSlot
		}
		field = selectedStyle.Width(popupWidth - 6).Render("Slot: " + slot)
		if m.confirmSlot != "" {
			help = "Press " + m.confirmSlot + " again to replace  •  Esc cancel"
		} else {
			help = "0–9 assign  •  x unpin  •  Esc cancel"
		}
	} else {
		title = "Close"
		if m.unsave {
			title = "Unsave"
		}
		if m.destroyPlan != nil {
			title = "Destroy"
		} else if len(m.mergedWorktrees) > 0 {
			title = "Remove merged worktrees"
		} else if m.closeRow.section == "wt-bulk" {
			title = fmt.Sprintf("Remove %d worktree%s", len(m.bulkWorktrees), plural(len(m.bulkWorktrees)))
		}
		field = lipgloss.NewStyle().Width(popupWidth - 6).Render(m.closePrompt())
		switch {
		case m.closeBusy:
			help = "Removing…"
		case m.worktreeForcePrompt:
			help = "Press f to force  •  Esc cancel"
		case m.unsave:
			help = "Press y to confirm  •  Esc cancel"
		case m.destroyPlan != nil:
			help = "y destroy  •  Esc cancel"
		default:
			if m.closeRow.section == "wt-filter" {
				help = "y remove  •  f force  •  Esc cancel"
			} else {
				help = "Press y to confirm  •  Esc cancel"
			}
		}
	}
	titleStyle := accentStyle
	borderColor := lipgloss.Color("205")
	if m.mode == modeCloseConfirm && m.destroyPlan != nil {
		titleStyle = errorStyle.Bold(true)
		borderColor = lipgloss.Color("196")
	}
	body := titleStyle.Render(title) + "\n\n" + field + "\n\n" + dimStyle.Render(help)
	if m.err != nil {
		body += "\n" + errorStyle.Render(m.err.Error())
	}
	return lipgloss.NewStyle().
		Width(popupWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Render(body)
}

// destroyPrompt renders the layer list for a unified Destroy confirmation,
// showing only the layers that apply to the plan.
