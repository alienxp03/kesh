package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type helpSection struct {
	title string
	body  string
}

var keymapHelp = []helpSection{
	{
		title: "Navigation",
		body:  "j/k, arrows, ctrl+j/k move  •  ctrl+d/u page  •  home/end/G jump  •  h/l descend or return  •  e expand",
	},
	{
		title: "Sessions and projects",
		body:  "enter open/focus/restore  •  space select  •  n new session  •  s/S save  •  p then 0–9 pin  •  p then x unpin  •  r rename",
	},
	{
		title: "Project actions",
		body:  "o launch layout  •  c clone  •  C check out PR  •  g open PR  •  x then y close  •  D then y delete closed  •  X then y remove merged",
	},
	{
		title: "Worktrees",
		body:  "w open worktrees  •  n create  •  enter focus  •  p pull  •  r refresh  •  g PR  •  x then y remove  •  D then y destroy  •  X then y remove merged  •  space bulk select  •  esc back",
	},
	{
		title: "Agents",
		body:  "tab to Agents  •  enter focus  •  p preview  •  r rename  •  x then y close",
	},
	{
		title: "Search and forms",
		body:  "/ search  •  type filter  •  backspace delete  •  ctrl+u clear  •  tab/shift+tab change field or filter  •  enter confirm  •  esc cancel",
	},
	{
		title: "General",
		body:  "? show or close help  •  q quit",
	},
}

func (m model) helpPopupView(width int) string {
	popupWidth := min(100, max(36, width-6))
	contentWidth := popupWidth - 6
	lines := []string{accentStyle.Render("Keyboard shortcuts"), strings.Repeat("─", contentWidth)}
	for _, section := range keymapHelp {
		line := focusStyle.Render(section.title) + "  " + section.body
		lines = append(lines, lipgloss.NewStyle().Width(contentWidth).Render(line))
	}
	lines = append(lines, "", dimStyle.Render("Press ? or Esc to close"))
	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Width(popupWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Render(body)
}
