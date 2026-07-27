package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type helpItem struct {
	keys   string
	action string
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
			{keys: "home/end/G", action: "jump"},
			{keys: "h/l", action: "descend or return"},
			{keys: "e", action: "expand or collapse"},
		},
	},
	{
		title: "Sessions and projects",
		items: []helpItem{
			{keys: "enter", action: "open, focus, or restore"},
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
			{keys: "o", action: "launch layout"},
			{keys: "c", action: "clone"},
			{keys: "C", action: "check out PR"},
			{keys: "g", action: "open PR"},
			{keys: "x then y", action: "close"},
			{keys: "D then y", action: "delete closed"},
			{keys: "X then y", action: "remove merged"},
		},
	},
	{
		title: "Worktrees",
		items: []helpItem{
			{keys: "w", action: "open worktrees"},
			{keys: "n", action: "create"},
			{keys: "enter", action: "focus"},
			{keys: "p", action: "pull"},
			{keys: "r", action: "refresh"},
			{keys: "g", action: "open PR"},
			{keys: "x then y", action: "remove"},
			{keys: "D then y", action: "destroy"},
			{keys: "X then y", action: "remove merged"},
			{keys: "space", action: "bulk select"},
			{keys: "esc", action: "back"},
		},
	},
	{
		title: "Agents",
		items: []helpItem{
			{keys: "tab", action: "open Agents"},
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
			{keys: "tab/shift+tab", action: "change field or filter"},
			{keys: "enter", action: "confirm"},
			{keys: "esc", action: "cancel"},
		},
	},
	{
		title: "General",
		items: []helpItem{
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
			lines = append(lines, lipgloss.NewStyle().Width(width).Render(line))
		}
	}
	return lines
}
