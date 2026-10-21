package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// wsStateGlyph renders the exclusive worktree state marker: ⊘ blocked, ⠋
// working, ● open, ○ idle.
func wsStateGlyph(state string) string {
	switch state {
	case "blocked":
		return errorStyle.Render("⊘")
	case "working":
		return sshStyle.Render("⠋")
	case "open":
		return openStyle.Render("●")
	default:
		return dimStyle.Render("○")
	}
}

// renderWorkRow renders one main-surface row: a repo header (expand state,
// rollup badges, tree count) or a worktree (state glyph, branch, PR/sync).
func (m model) renderWorkRow(r row, width int, focused bool) string {
	group, wt, ok := m.wsRow(r)
	if !ok {
		return ""
	}
	if r.wi < 0 {
		arrow := "▸"
		if m.wsRepoExpanded(group) {
			arrow = "▾"
		}
		left := arrow + " " + projectStyle.Render(truncate(group.name, max(4, width/2)))
		var badges []string
		if group.blocked > 0 {
			badges = append(badges, errorStyle.Render(fmt.Sprintf("⊘%d", group.blocked)))
		}
		if group.working > 0 {
			badges = append(badges, sshStyle.Render(fmt.Sprintf("⠋%d", group.working)))
		}
		badges = append(badges, dimStyle.Render(fmt.Sprintf("%d tree%s", len(group.worktrees), plural(len(group.worktrees)))))
		right := strings.Join(badges, " ")
		if focused {
			left = focusStyle.Render(ansi.Strip(left))
		}
		return padColumnsAt(left, right, width, 55)
	}

	leftWidth := max(8, width*55/100)
	left := "  " + wsStateGlyph(wt.state) + " " + truncate(wt.item.branch, max(4, leftWidth-8))
	if wt.item.current {
		left += " " + accentStyle.Render("★")
	}
	if focused {
		left = focusStyle.Render(ansi.Strip(left))
	}
	var badges []string
	if wt.item.prNumber > 0 {
		badge := prStatusIcon(wt.item.prStatus) + " " + dimStyle.Render("#"+strconv.Itoa(wt.item.prNumber))
		badges = append(badges, ansi.Truncate(badge, 14, "…"))
	}
	if sync := worktreeSyncBadge(wt.item); sync != "" {
		badges = append(badges, sync)
	}
	if wt.state == "idle" && wt.item.prStatus == "merged" {
		badges = append(badges, dimStyle.Render("merged"))
	}
	right := strings.Join(badges, " ")
	return padColumnsAt(left, right, width, 55)
}

// renderWsOtherHeader renders the collapsed "other" group header: SSH hosts,
// saved sessions, and projects without repo groups.
func (m model) renderWsOtherHeader(width int, focused bool) string {
	count := len(m.wsOtherEntries())
	left := "▸ " + dimStyle.Render(fmt.Sprintf("other · ssh, saved, projects (%d)", count))
	if m.wsOtherExpanded {
		left = "▾ " + dimStyle.Render(fmt.Sprintf("other · ssh, saved, projects (%d)", count))
	}
	if focused {
		left = focusStyle.Render(ansi.Strip(left))
	}
	return ansi.Truncate(left, width, "…")
}

// agentPaneRow is one flat line in the agent pane.
type agentPaneRow struct {
	tool     string
	status   string
	since    time.Time
	worktree string
	repo     string
}

// agentPaneRows flattens repo groups into the pane's dense agent list: blocked
// first (oldest first), then working (newest first), then idle.
func (m model) agentPaneRows() []agentPaneRow {
	flat := flattenAgents(m.wsRepos)
	rows := make([]agentPaneRow, 0, len(flat))
	for _, entry := range flat {
		rows = append(rows, agentPaneRow{
			tool: entry.agent.tool, status: entry.agent.status, since: entry.agent.since,
			worktree: entry.worktree, repo: entry.repo,
		})
	}
	return rows
}

// agentPaneView renders the dense agent list: `status · worktree · repo ·
// agent · age`, one line per agent, blocked agents first.
func (m model) agentPaneView(width, height int) string {
	rows := m.agentPaneRows()
	blocked := 0
	for _, r := range rows {
		if wsAgentBlocked(wsAgent{status: r.status}) {
			blocked++
		}
	}
	lines := []string{dimStyle.Render(fmt.Sprintf("AGENTS · %d blocked", blocked))}
	shown := 0
	for _, r := range rows {
		if shown >= max(0, height-1) {
			hidden := len(rows) - shown
			lines = append(lines, dimStyle.Render(fmt.Sprintf("  … %d more", hidden)))
			break
		}
		glyph := dimStyle.Render("○")
		switch {
		case r.status == "finished" || r.status == "errored":
			glyph = errorStyle.Render("⊘")
		case r.status == "working":
			glyph = sshStyle.Render("⠋")
		}
		age := ""
		if !r.since.IsZero() {
			age = formatAge(time.Since(r.since))
		}
		nameWidth := max(6, width*30/100)
		where := truncate(r.worktree+" · "+r.repo, max(8, width*45/100))
		line := " " + glyph + " " + focusStyle.Render(ansi.Strip(where)) +
			"  " + dimStyle.Render(truncate(r.tool, nameWidth))
		if age != "" {
			line = padColumnsAt(line, dimStyle.Render(age), width, 70)
		} else {
			line = ansi.Truncate(line, width, "…")
		}
		lines = append(lines, line)
		shown++
	}
	if len(rows) == 0 {
		lines = append(lines, dimStyle.Render("  no agents running"))
	}
	panel := lipgloss.NewStyle().Width(width).Height(height).Padding(0, 1)
	return panel.Render(strings.Join(lines, "\n"))
}
