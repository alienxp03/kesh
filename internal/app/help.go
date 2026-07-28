package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type helpItem struct {
	keys      string
	action    string
	dangerous bool
}

type helpSection struct {
	title string
	items []helpItem
}

var keymapHelp = []helpSection{
	{
		title: "Navigation",
		items: []helpItem{
			{keys: "j/k, arrows", action: "move"},
			{keys: "ctrl+d/u", action: "page up/down"},
			{keys: "gg/G", action: "jump to top/bottom"},
			{keys: "h", action: "collapse or return to parent"},
			{keys: "l", action: "expand or descend"},
			{keys: "e", action: "expand or collapse"},
		},
	},
	{
		title: "Sessions and projects",
		items: []helpItem{
			{keys: "enter", action: "open with layout, focus, or restore"},
			{keys: "O", action: "open folder without layout"},
			{keys: "space", action: "select"},
			{keys: "n", action: "new session"},
			{keys: "s/S", action: "save"},
			{keys: "p then 0–9", action: "pin"},
			{keys: "p then x", action: "unpin"},
			{keys: "r", action: "rename"},
		},
	},
	{
		title: "Project actions",
		items: []helpItem{
			{keys: "o", action: "open PR"},
			{keys: "c", action: "clone"},
			{keys: "C", action: "check out PR"},
			{keys: "x then y", action: "close"},
			{keys: "D then y", action: "destroy entry and linked layers", dangerous: true},
			{keys: "X then y", action: "remove merged worktrees", dangerous: true},
		},
	},
	{
		title: "Worktrees",
		items: []helpItem{
			{keys: "w", action: "open from a window or unopened folder"},
			{keys: "n", action: "create"},
			{keys: "enter", action: "open or focus"},
			{keys: "f", action: "pull (fetch and rebase)"},
			{keys: "r", action: "refresh"},
			{keys: "o", action: "open PR"},
			{keys: "x then y", action: "remove checkout"},
			{keys: "D then y", action: "destroy checkout and branch", dangerous: true},
			{keys: "X then y", action: "remove merged worktrees", dangerous: true},
			{keys: "space", action: "bulk select"},
			{keys: "esc", action: "back"},
		},
	},
	{
		title: "Agents",
		items: []helpItem{
			{keys: "enter", action: "focus"},
			{keys: "p", action: "preview"},
			{keys: "r", action: "rename"},
			{keys: "x then y", action: "close"},
		},
	},
	{
		title: "Search and forms",
		items: []helpItem{
			{keys: "/", action: "search"},
			{keys: "type", action: "filter"},
			{keys: "backspace", action: "delete"},
			{keys: "ctrl+u", action: "clear"},
			{keys: "tab/shift+tab", action: "change field"},
			{keys: "enter", action: "open result or confirm"},
			{keys: "esc", action: "return to command mode or cancel"},
		},
	},
	{
		title: "General",
		items: []helpItem{
			{keys: "tab/shift+tab", action: "cycle filters"},
			{keys: "?", action: "show or close help"},
			{keys: "q", action: "quit"},
		},
	},
}

func (m model) helpPopupView(width, height int) string {
	popupWidth := min(82, max(36, width-6))
	contentWidth := popupWidth - 6
	allLines := helpContent(contentWidth)
	viewportHeight := max(1, height-13)
	if height <= 0 {
		viewportHeight = len(allLines)
	}
	maxScroll := max(0, len(allLines)-viewportHeight)
	scroll := min(max(0, m.helpScroll), maxScroll)
	visible := allLines[scroll:min(len(allLines), scroll+viewportHeight)]

	lines := []string{accentStyle.Render("Keyboard shortcuts"), strings.Repeat("─", contentWidth)}
	lines = append(lines, visible...)
	help := "Press ? or Esc to close"
	if maxScroll > 0 {
		help = fmt.Sprintf("j/k scroll  •  %d/%d  •  ? / Esc close", scroll+1, maxScroll+1)
	}
	lines = append(lines, "", dimStyle.Render(help))
	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(popupWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Render(body)
}

func helpContent(width int) []string {
	keyWidth := min(18, max(10, width/3))
	lines := make([]string, 0, 40)
	for index, section := range keymapHelp {
		if index > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, focusStyle.Render(section.title))
		for _, item := range section.items {
			line := fmt.Sprintf("  %-*s  %s", keyWidth, item.keys, item.action)
			if item.dangerous {
				line = errorStyle.Bold(true).Render(line)
			}
			lines = append(lines, lipgloss.NewStyle().Width(width).Render(line))
		}
	}
	return lines
}
