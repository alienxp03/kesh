package app

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m model) View() string {
	outerWidth := max(40, m.width-4)
	workspaceWidth := min(140, outerWidth)
	selectedPRURL, _ := m.selectedPullRequest()
	hasSelectedPR := selectedPRURL != ""
	bodyHeight := max(5, m.height-6)
	showAgentPreview := m.showPreview && m.hasSelectedAgentWindow() && bodyHeight >= 14
	listWidth, detailWidth := workspaceWidth, workspaceWidth
	listHeight, detailHeight := bodyHeight, bodyHeight
	showAgentPreviewSideBySide := showAgentPreview && m.filter == filterAgents
	if showAgentPreviewSideBySide {
		// Agent browsing benefits from a compact chooser and a wide live screen.
		listWidth = max(42, min(60, workspaceWidth*42/100))
		detailWidth = workspaceWidth - listWidth - 2
	} else if showAgentPreview {
		// The hierarchy stays readable above a full-width terminal preview.
		listHeight = max(5, bodyHeight*40/100)
		detailHeight = max(6, bodyHeight-listHeight-1)
	}

	tabs := []string{"All", "Agents", "SSH", "Saved"}
	// In the worktree drill-in no flat filter is active, so highlight the one
	// the user came from (previousFilter). This keeps the active indicator in
	// place instead of leaping to an appended [Worktrees] chip; the surface is
	// named in the list title instead.
	activeFilter := m.filter
	if activeFilter == filterWorktrees {
		activeFilter = m.previousFilter
	}
	for i := range tabs {
		if i == activeFilter {
			tabs[i] = accentStyle.Render("[" + tabs[i] + "]")
		} else {
			tabs[i] = dimStyle.Render(" " + tabs[i] + " ")
		}
	}
	promptValue := dimStyle.Render("/ to search")
	if m.query != "" {
		promptValue = m.query
	}
	if m.mode == modeSearch {
		promptValue = accentStyle.Render(m.query+"█") + "  " + dimStyle.Render("SEARCH")
	}
	header := accentStyle.Render("Kesh") + "  " + strings.Join(tabs, " ")
	if m.filter == filterWorktrees && len(m.wtBulkSelected) > 0 {
		// Bulk-selection count is the only worktree status shown in the header;
		// the surface itself is named in the list title.
		header += "  " + accentStyle.Render(fmt.Sprintf("Selected (%d)", len(m.wtBulkSelected)))
	}
	if len(m.selected) > 0 {
		names := make([]string, 0, len(m.selected))
		for _, entry := range m.entries {
			if m.selected[entry.key] {
				names = append(names, entry.name)
			}
		}
		summary := fmt.Sprintf("Selected (%d): %s", len(names), strings.Join(names, ", "))
		header += "  " + accentStyle.Render(truncate(summary, max(12, workspaceWidth-lipgloss.Width(header)-2)))
	}

	available := max(1, listHeight-3)
	start := 0
	if m.cursor >= available {
		start = m.cursor - available + 1
	}
	end := min(len(m.rows), start+available)
	// On the Worktrees surface the list is one project's worktrees, so title it
	// with that folder and its count rather than the generic "List".
	listTitle := accentStyle.Render(fmt.Sprintf("List (%d)", len(m.rows)))
	if m.filter == filterWorktrees && m.worktreeFilterEntryIndex >= 0 && m.worktreeFilterEntryIndex < len(m.entries) {
		entry := m.entries[m.worktreeFilterEntryIndex]
		name := truncate(entry.name, max(8, listWidth-20))
		listTitle = projectStyle.Render(fmt.Sprintf("󰉋 %s · worktrees (%d)", name, len(m.rows)))
	}
	listLines := []string{listTitle}
	if m.filter == filterWorktrees && len(m.rows) > 0 {
		// Label the columns so the branch reads as a first-class table column
		// rather than an unlabeled leading field. Keep Branch and Path evenly
		// split so long branch names cannot crowd the directory column. The
		// 2-space indent matches the non-focused row prefix.
		rowWidth := max(8, listWidth-4)
		header := padColumnsAt(dimStyle.Render("Branch"), dimStyle.Render("Path"), rowWidth, 50)
		listLines = append(listLines, "  "+header)
	}
	for i := start; i < end; i++ {
		row := m.rows[i]
		focused := i == m.cursor
		line := m.renderRow(row, max(8, listWidth-4), focused)
		if focused {
			if row.tabIndex < 0 && m.entries[row.entryIndex].open {
				line = accentStyle.Render("▌") + " " + line
			} else {
				line = accentStyle.Render("▌") + " " + focusStyle.Render(ansi.Strip(line))
			}
		} else {
			line = "  " + line
		}
		listLines = append(listLines, line)
	}
	if len(m.rows) == 0 {
		empty := "  No matching sessions"
		if m.filter == filterWorktrees {
			// Worktrees is only ever opened scoped to a project (via w). Distinguish
			// the initial asynchronous load from a genuinely empty repository.
			empty = "  No worktrees — press n to create one"
			if m.worktreeLoading {
				empty = "  Loading worktrees…"
			}
		}
		listLines = append(listLines, dimStyle.Render(empty))
	}
	listPanel := renderListPanel(listLines, listWidth, listHeight)
	body := listPanel
	if showAgentPreview {
		previewPanel := m.previewView(detailWidth, detailHeight)
		if showAgentPreviewSideBySide {
			body = lipgloss.JoinHorizontal(lipgloss.Top, listPanel, "  ", previewPanel)
		} else {
			body = listPanel + "\n" + previewPanel
		}
	}

	footer := m.footerView(workspaceWidth, hasSelectedPR)
	if m.mode == modeSearch {
		footer = dimStyle.Render("type to filter  ctrl+j/k move  backspace delete  ctrl+u clear  enter open  esc command mode")
	}
	if m.saving {
		footer = dimStyle.Render("Saving workspace…")
	}
	if m.zoxidePending && m.mode != modeSearch {
		footer += dimStyle.Render("  · loading projects…")
	}
	if m.err != nil && m.mode != modeRename && m.mode != modeCreateSession && m.mode != modeClone && m.mode != modeSaveConfirm && m.mode != modePin && m.mode != modeCloseConfirm && m.mode != modeWorktreeCreate && !m.mergedWorktreeBusy && !m.worktreePullBusy {
		footer = errorStyle.Render("Error: " + m.err.Error())
	}

	content := strings.Join([]string{
		ansi.Truncate(header, workspaceWidth, "…"),
		ansi.Truncate(fmt.Sprintf("%-6s  %s", "Search", promptValue), workspaceWidth, "…"),
		strings.Repeat("─", workspaceWidth),
		body,
		ansi.Truncate(footer, workspaceWidth, "…"),
	}, "\n")
	if popup := m.popupView(workspaceWidth); popup != "" {
		content = strings.Join(overlayPopup(strings.Split(content, "\n"), popup, workspaceWidth), "\n")
	}
	content = lipgloss.PlaceHorizontal(outerWidth, lipgloss.Center, content)
	// Keep the alternate-screen frame at a stable height. Some detail values
	// wrap differently (notably the worktree summary), and an extra rendered
	// line makes the terminal scroll the entire view upward.
	if m.height > 3 {
		// The two vertical padding rows are outside this fixed content frame.
		frameHeight := m.height - 2
		lines := strings.Split(content, "\n")
		if len(lines) > frameHeight {
			lines = lines[:frameHeight]
		}
		for len(lines) < frameHeight {
			lines = append(lines, "")
		}
		content = strings.Join(lines, "\n")
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

func (m model) footerView(width int, hasSelectedPR bool) string {
	items := []string{"j/k move", "enter open"}
	dangerous := make([]string, 0, 2)

	switch {
	case m.filter == filterAgents && m.hasSelectedAgentWindow():
		items = []string{"j/k move", "enter focus", "p preview"}
	case m.filter == filterWorktrees:
		items = []string{"j/k move", "enter open", "f pull", "n create", "x remove", "esc back"}
		if width >= 90 {
			items = append(items, "space select", "r refresh")
		}
		dangerous = append(dangerous, "D destroy")
	default:
		if width >= 90 {
			items = append(items, "space select", "n new", "h/l tree")
		}
		if m.canHintDestroy() {
			dangerous = append(dangerous, "D destroy")
		}
	}
	items = append(items, "? help")
	if hasSelectedPR {
		items = append(items, "o PR")
	}
	if m.canHintRemoveMerged() && m.filter != filterAgents {
		dangerous = append(dangerous, "X merged")
	}
	items = append(items, "/ search", "tab filters", "q quit")

	footer := dimStyle.Render(strings.Join(items, "  "))
	if len(dangerous) > 0 && width >= 76 {
		footer += "  " + errorStyle.Bold(true).Render(strings.Join(dangerous, "  "))
	}
	return footer
}

func (m model) canHintRemoveMerged() bool {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return false
	}
	return m.worktreeDirectory(m.rows[m.cursor]) != ""
}

func (m model) canHintDestroy() bool {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return false
	}
	selected := m.rows[m.cursor]
	entry := m.entries[selected.entryIndex]
	return selected.section == "wt-filter" || entry.open || entry.saved
}

func renderListPanel(lines []string, width, height int) string {
	panelWidth := max(12, width-2)
	contentHeight := max(1, height-2)
	if len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}
	for index := range lines {
		lines[index] = ansi.Truncate(lines[index], panelWidth, "…")
	}
	return lipgloss.NewStyle().
		Width(panelWidth).
		Height(contentHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("241")).
		Render(strings.Join(lines, "\n"))
}

func (m model) previewView(width, height int) string {
	content := m.preview
	switch {
	case m.previewBusy:
		content = dimStyle.Render("Loading preview…")
	case m.previewErr != nil:
		content = errorStyle.Render("Preview unavailable: " + m.previewErr.Error())
	case content == "":
		content = dimStyle.Render("No terminal content")
	}
	path := displayPath(m.selectedAgentPath(), os.Getenv("HOME"))
	pathLine := dimStyle.Render("Path  ") + mutedStyle.Render(middleTruncate(path, max(1, width-6)))
	header := accentStyle.Render("Agent screen")
	body := lipgloss.NewStyle().Width(width).MaxWidth(width).MaxHeight(max(1, height-3)).Render(content)
	return lipgloss.NewStyle().Width(width).Height(height).Render(pathLine + "\n" + header + "\n" + strings.Repeat("─", width) + "\n" + body)
}

func (m model) selectedAgentPath() string {
	if !m.hasSelectedAgentWindow() {
		return ""
	}
	selected := m.rows[m.cursor]
	return m.entries[selected.entryIndex].tabs[selected.tabIndex].windows[selected.windowIndex].cwd
}

func middleTruncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 3 {
		return strings.Repeat(".", width)
	}
	runes := []rune(value)
	left := (width - 1) / 2
	right := width - 1 - left
	if left+right >= len(runes) {
		return value
	}
	return string(runes[:left]) + "…" + string(runes[len(runes)-right:])
}

type detailField struct {
	label  string
	value  string
	middle bool
}

// worktreeSyncBadge renders a worktree's divergence from its upstream and any
// uncommitted changes as a compact inline marker: ↑N ahead, ↓M behind, ✱ dirty.
func worktreeSyncBadge(worktree worktreeItem) string {
	dirtyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	var segments []string
	if worktree.ahead > 0 {
		segments = append(segments, dimStyle.Render("↑"+strconv.Itoa(worktree.ahead)))
	}
	if worktree.behind > 0 {
		segments = append(segments, dimStyle.Render("↓"+strconv.Itoa(worktree.behind)))
	}
	if worktree.dirty {
		segments = append(segments, dirtyStyle.Render("✱"))
	}
	return strings.Join(segments, " ")
}

// worktreeSyncDetail renders a worktree's working-tree and upstream state for
// the detail panel (e.g. "clean · ↑2 ahead · ↓1 behind").
func worktreeSyncDetail(worktree worktreeItem) string {
	var parts []string
	if worktree.dirty {
		parts = append(parts, "uncommitted changes")
	} else {
		parts = append(parts, "clean")
	}
	if worktree.ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d ahead", worktree.ahead))
	}
	if worktree.behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d behind", worktree.behind))
	}
	if worktree.ahead == 0 && worktree.behind == 0 {
		parts = append(parts, "up to date")
	}
	return strings.Join(parts, " · ")
}

func worktreePRSummary(worktree worktreeItem) string {
	if worktree.prNumber == 0 {
		return "—"
	}
	summary := strings.TrimSpace(prStatusIcon(worktree.prStatus) + " #" + strconv.Itoa(worktree.prNumber))
	if !worktree.prExact {
		summary += " · local HEAD differs"
	}
	return summary
}

func pathPRSummary(info pathPRInfo) string {
	pullRequest := info.PullRequest
	if pullRequest.Number == 0 {
		return ""
	}
	summary := strings.TrimSpace(prStatusIcon(pullRequest.Status) + " #" + strconv.Itoa(pullRequest.Number))
	if !info.Exact {
		summary += " · local HEAD differs"
	}
	return summary
}

func pullRequestRepository(prURL string) string {
	parsed, err := url.Parse(prURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 4 || parts[2] != "pull" || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func entryPRSummary(info pathPRInfo) string {
	if info.PullRequest.Number == 0 {
		return ""
	}
	if repository := pullRequestRepository(info.PullRequest.URL); repository != "" {
		return repository + " #" + strconv.Itoa(info.PullRequest.Number)
	}
	return "#" + strconv.Itoa(info.PullRequest.Number)
}

func entryPRColumn(entry entry) string {
	if summary := entryPRSummary(entry.pathPR); summary != "" {
		return summary
	}
	for _, tab := range entry.tabs {
		for _, window := range tab.windows {
			if summary := entryPRSummary(window.pathPR); summary != "" {
				return summary
			}
		}
	}
	return ""
}

func renderDetailPanel(title string, fields []detailField, action string, extra []string, width, height int, compact bool) string {
	panelWidth := max(12, width-2)
	contentHeight := max(1, height-2)
	valueWidth := max(4, panelWidth-8)
	lines := make([]string, 0, contentHeight)
	if !compact {
		lines = append(lines, accentStyle.Render(title))
	}
	fieldLimit := len(fields)
	if compact {
		fieldLimit = min(fieldLimit, 3)
	}
	for _, field := range fields[:fieldLimit] {
		value := field.value
		if field.label == "" {
			if compact {
				lines = append(lines, ansi.Truncate(value, panelWidth, "…"))
			} else {
				lines = append(lines, strings.Split(ansi.Wrap(value, panelWidth, " /_·"), "\n")...)
			}
			continue
		}
		if compact {
			parts := strings.Split(value, "\n")
			if len(parts) > 1 {
				value = fmt.Sprintf("%s (+%d more)", parts[0], len(parts)-1)
			}
			if field.middle {
				value = middleTruncate(value, valueWidth)
			} else {
				value = ansi.Truncate(value, valueWidth, "…")
			}
			lines = append(lines, mutedStyle.Render(fmt.Sprintf("%-8s", field.label))+value)
			continue
		}
		wrapped := strings.Split(ansi.Wrap(value, valueWidth, " /_·"), "\n")
		for index, line := range wrapped {
			label := strings.Repeat(" ", 8)
			if index == 0 {
				label = fmt.Sprintf("%-8s", field.label)
			}
			lines = append(lines, mutedStyle.Render(label)+line)
		}
	}
	if !compact && action != "" {
		lines = append(lines, "", dimStyle.Render(action))
	}
	if !compact && len(extra) > 0 {
		if len(lines)+2 < contentHeight {
			lines = append(lines, "")
		}
		lines = append(lines, accentStyle.Render("Screen"))
		for _, line := range extra {
			lines = append(lines, ansi.Truncate(line, max(8, panelWidth-2), "…"))
		}
	}
	if len(lines) > contentHeight {
		lines = lines[:contentHeight]
	}
	for len(lines) < contentHeight {
		lines = append(lines, "")
	}
	return lipgloss.NewStyle().
		Width(panelWidth).
		Height(contentHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("241")).
		Render(strings.Join(lines, "\n"))
}

func worktreeInfoView(worktree worktreeItem, width int, compact bool) string {
	height := 8
	if compact {
		height = 5
	}
	action := "No matching pull request"
	if worktree.prURL != "" {
		action = "o Open PR"
	}
	return renderDetailPanel("Worktree", []detailField{
		{label: "Branch", value: worktree.branch, middle: true},
		{label: "Path", value: displayPath(worktree.path, os.Getenv("HOME")), middle: true},
		{label: "PR", value: worktreePRSummary(worktree)},
	}, action, nil, width, height, compact)
}

func entryDirectoryField(entry entry) detailField {
	seen := map[string]bool{}
	directories := make([]string, 0)
	for _, tab := range entry.tabs {
		for _, window := range tab.windows {
			if window.cwd == "" {
				continue
			}
			directory := displayPath(window.cwd, os.Getenv("HOME"))
			if !seen[directory] {
				seen[directory] = true
				directories = append(directories, directory)
			}
		}
	}
	if len(directories) == 0 {
		directory := entry.path
		if directory == "" {
			directory = entry.detail
		} else {
			directory = displayPath(directory, os.Getenv("HOME"))
		}
		return detailField{label: "Path", value: directory, middle: true}
	}
	if len(directories) == 1 {
		return detailField{label: "Path", value: directories[0], middle: true}
	}
	visible := directories
	if len(visible) > 3 {
		visible = append([]string{}, visible[:3]...)
		visible = append(visible, fmt.Sprintf("…and %d more", len(directories)-3))
	}
	return detailField{label: "Paths", value: strings.Join(visible, "\n")}
}

func (m model) detailPanelView(width, height int, compact bool) string {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		if m.filter == filterWorktrees && m.worktreeLoading {
			return renderDetailPanel("Worktrees", []detailField{{value: "Loading repository worktrees…"}}, "", nil, width, height, compact)
		}
		return renderDetailPanel("Info", []detailField{{value: "No matching rows — change or clear the filter."}}, "", nil, width, height, compact)
	}
	selected := m.rows[m.cursor]
	entry := m.entries[selected.entryIndex]
	if selected.section == "wt-filter" && m.filter == filterWorktrees {
		if m.cursor < 0 || m.cursor >= len(m.worktreeFilterRows) {
			return renderDetailPanel("Worktree", []detailField{{value: "No worktrees"}}, "", nil, width, height, compact)
		}
		wtRow := m.worktreeFilterRows[m.cursor]
		wt := wtRow.worktree

		action := "No matching pull request"
		if wt.prURL != "" {
			action = "o Open PR"
		}

		fields := []detailField{
			{label: "Branch", value: wt.branch, middle: true},
			{label: "Path", value: displayPath(wt.path, os.Getenv("HOME")), middle: true},
			{label: "PR", value: worktreePRSummary(wt)},
			{label: "Commit", value: wt.head},
			{label: "Sync", value: worktreeSyncDetail(wt)},
		}
		if wt.dirty && len(wt.changes) > 0 {
			shown := wt.changes
			if len(shown) > 6 {
				shown = append(append([]string{}, shown[:6]...), fmt.Sprintf("…and %d more", len(wt.changes)-6))
			}
			fields = append(fields, detailField{label: "Changes", value: strings.Join(shown, "\n")})
		}

		if wt.current {
			fields = append([]detailField{{label: "Status", value: accentStyle.Render("★ Current worktree")}}, fields...)
		}

		return renderDetailPanel("Worktree", fields, action, nil, width, height, compact)
	}
	if selected.windowIndex >= 0 {
		window := entry.tabs[selected.tabIndex].windows[selected.windowIndex]
		fields := []detailField{
			{label: "Name", value: window.title},
			{label: "Project", value: entry.name},
			{label: "Path", value: displayPath(window.cwd, os.Getenv("HOME")), middle: true},
		}
		if window.pathPR.PullRequest.Number > 0 {
			fields = []detailField{
				{label: "Name", value: window.title},
				{label: "Path", value: displayPath(window.cwd, os.Getenv("HOME")), middle: true},
				{label: "PR", value: pathPRSummary(window.pathPR)},
				{label: "Branch", value: window.pathPR.Branch, middle: true},
			}
		}
		if m.filter == filterAgents {
			fields = []detailField{
				{label: "Name", value: window.title},
				{label: "Agent", value: window.agent},
			}
			if status := agentStatusLabel(window.agentStatus); status != "" {
				fields = append(fields, detailField{label: "Status", value: status})
			}
			fields = append(fields,
				detailField{label: "Project", value: entry.name},
				detailField{label: "Last active", value: relativeLastActive(window.lastFocused, m.lastFocusedReference())},
				detailField{label: "Path", value: displayPath(window.cwd, os.Getenv("HOME")), middle: true},
			)
			if window.pathPR.PullRequest.Number > 0 {
				for index := range fields {
					if fields[index].label == "Last active" {
						fields[index] = detailField{label: "PR", value: pathPRSummary(window.pathPR)}
						break
					}
				}
			}
		} else {
			if window.command != "" {
				fields = append(fields, detailField{label: "Command", value: window.command})
			}
			if window.agent != "" {
				fields = append(fields, detailField{label: "Agent", value: window.agent})
			}
		}
		var screen []string
		action := "Enter focus · r rename"
		if window.pathPR.PullRequest.URL != "" {
			action = "Enter focus · o PR · r rename"
		}
		title := "Window"
		if m.filter == filterAgents {
			title = "Agent screen"
		}
		if m.filter == filterAgents && m.showPreview {
			screen = strings.Split(m.preview, "\n")
			if m.previewBusy {
				screen = []string{"Loading preview…"}
			} else if m.previewErr != nil {
				screen = []string{"Preview unavailable: " + m.previewErr.Error()}
			} else if m.preview == "" {
				screen = []string{"No terminal content"}
			}
		}
		return renderDetailPanel(title, fields, action, screen, width, height, compact)
	}
	if selected.tabIndex >= 0 {
		tab := entry.tabs[selected.tabIndex]
		fields := []detailField{
			{label: "Name", value: tab.title},
			{label: "Project", value: entry.name},
			{label: "Windows", value: strconv.Itoa(len(tab.windows))},
		}
		action := "Enter focus · r rename"
		for _, window := range tab.windows {
			if window.pathPR.PullRequest.Number > 0 {
				fields = append(fields, detailField{label: "PR", value: pathPRSummary(window.pathPR)})
				action = "Enter focus · o PR · r rename"
				break
			}
		}
		return renderDetailPanel("Tab", fields, action, nil, width, height, compact)
	}
	directoryField := entryDirectoryField(entry)
	title := "Project"
	switch entry.kind {
	case "workspace":
		title = "Workspace"
	case "ssh":
		title = "SSH"
	}
	fields := []detailField{
		{label: "Name", value: entry.name},
		directoryField,
	}
	if entry.pathPR.PullRequest.Number > 0 {
		fields = []detailField{
			{label: "Name", value: entry.name},
			directoryField,
			{label: "PR", value: pathPRSummary(entry.pathPR)},
			{label: "Branch", value: entry.pathPR.Branch, middle: true},
		}
	}
	action := "Enter open"
	if entry.kind == "project" && !entry.open {
		action = "Enter layout · O plain · w worktrees"
	}
	if entry.pathPR.PullRequest.URL != "" {
		action += " · o PR"
	}
	return renderDetailPanel(title, fields, action, nil, width, height, compact)
}

// prCheckoutPreview renders the dim summary block under the PR input. Once gh
// resolves the PR head, it shows the exact worktree path that checkout uses. A
// bare PR number has no owner/repo to resolve until its selected project is
// inspected, so only that project is noted.
func destroyPrompt(plan destroyPlan) string {
	lines := []string{fmt.Sprintf("Destroy %q?", plan.entryName)}
	if plan.closeSession {
		lines = append(lines, "  • Close kitty session ("+strconv.Itoa(plan.tabCount)+" tab"+plural(plan.tabCount)+")")
	}
	if len(plan.worktrees) > 1 {
		lines = append(lines, fmt.Sprintf("  • Remove %d worktrees", len(plan.worktrees)))
		branchCount := 0
		for index, worktree := range plan.worktrees {
			if worktree.Branch != "" {
				branchCount++
			}
			if index >= 4 {
				if index == 4 {
					lines = append(lines, fmt.Sprintf("    …and %d more", len(plan.worktrees)-index))
				}
				continue
			}
			branch := worktree.Branch
			if branch == "" {
				branch = "detached HEAD"
			}
			lines = append(lines, "    "+branch+"  "+displayPath(worktree.Path, os.Getenv("HOME")))
		}
		if branchCount > 0 {
			branchLabel := "branch"
			if branchCount > 1 {
				branchLabel = "branches"
			}
			lines = append(lines, fmt.Sprintf("  • Delete %d local %s", branchCount, branchLabel))
		}
	} else if plan.worktreePath != "" {
		lines = append(lines, "  • Remove worktree  "+displayPath(plan.worktreePath, os.Getenv("HOME")))
		if plan.branch != "" {
			lines = append(lines, "  • Delete branch  "+plan.branch)
		}
	}
	if plan.saved {
		lines = append(lines, "  • Delete saved record")
	}
	return strings.Join(lines, "\n")
}

func (m model) closePrompt() string {
	selected := m.closeRow
	entry := m.entries[selected.entryIndex]
	if m.destroyPlan != nil {
		return destroyPrompt(*m.destroyPlan)
	}
	if len(m.mergedWorktrees) > 0 {
		lines := []string{fmt.Sprintf("Delete %d merged worktree%s?", len(m.mergedWorktrees), plural(len(m.mergedWorktrees)))}
		for i, worktree := range m.mergedWorktrees {
			if i == 4 {
				lines = append(lines, fmt.Sprintf("  …and %d more", len(m.mergedWorktrees)-i))
				break
			}
			lines = append(lines, "  "+worktree.branch)
		}
		return strings.Join(lines, "\n")
	}
	if selected.section == "wt-bulk" {
		targets := m.bulkWorktrees
		lines := []string{fmt.Sprintf("Delete %d worktree%s?", len(targets), plural(len(targets)))}
		for i, worktree := range targets {
			if i == 4 {
				lines = append(lines, fmt.Sprintf("  …and %d more", len(targets)-i))
				break
			}
			lines = append(lines, "  "+worktree.branch)
		}
		return strings.Join(lines, "\n")
	}
	if selected.section == "wt-filter" {
		wt, ok := m.worktreeForRow(selected)
		if !ok {
			return "Worktree is no longer available"
		}
		prefix := "Delete"
		if m.worktreeForcePrompt {
			prefix = "Force-delete"
		}
		return fmt.Sprintf("%s worktree?\n\nBranch: %s\nPath:   %s", prefix, wt.branch, displayPath(wt.path, os.Getenv("HOME")))
	}
	if m.unsave {
		return fmt.Sprintf("Unsave workspace %q?", entry.name)
	}
	if entry.saved && !entry.open {
		return fmt.Sprintf("Delete saved workspace %q?", entry.name)
	}
	if selected.windowIndex >= 0 {
		window := entry.tabs[selected.tabIndex].windows[selected.windowIndex]
		return fmt.Sprintf("Close window %q?", window.title)
	}
	if selected.tabIndex >= 0 {
		tab := entry.tabs[selected.tabIndex]
		return fmt.Sprintf("Close tab %q and its %d window%s?", tab.title, len(tab.windows), plural(len(tab.windows)))
	}
	return fmt.Sprintf("Close workspace %q and its %d tab%s?", entry.name, len(entry.tabs), plural(len(entry.tabs)))
}

func overlayPopup(lines []string, popup string, width int) []string {
	popupLines := strings.Split(popup, "\n")
	start := max(3, (len(lines)-len(popupLines))/2)
	if start+len(popupLines) > len(lines) {
		start = max(3, len(lines)-len(popupLines))
	}
	for index, popupLine := range popupLines {
		lineIndex := start + index
		if lineIndex >= len(lines) {
			break
		}
		popupWidth := min(width, lipgloss.Width(popupLine))
		left := max(0, (width-popupWidth)/2)
		right := left + popupWidth
		background := ansi.Truncate(lines[lineIndex], width, "")
		if padding := width - lipgloss.Width(background); padding > 0 {
			background += strings.Repeat(" ", padding)
		}
		lines[lineIndex] = ansi.Cut(background, 0, left) + ansi.Truncate(popupLine, popupWidth, "") + ansi.Cut(background, right, width)
	}
	return lines
}

func (m model) renderRow(r row, width int, focused bool) string {
	e := m.entries[r.entryIndex]
	switch r.section {
	case "wt-filter":
		// Worktree tab — flat list of the open project's worktrees.
		if r.wt < 0 || r.wt >= len(m.worktreeFilterRows) {
			return ""
		}
		wt := m.worktreeFilterRows[r.wt].worktree

		selected := " "
		if m.wtBulkSelected[wt.path] {
			selected = accentStyle.Render("✓")
		}
		current := " "
		if wt.current {
			current = accentStyle.Render("★")
		}

		prStatus := ""
		if wt.prNumber > 0 {
			prStatus = prStatusIcon(wt.prStatus) + " " + dimStyle.Render("#"+strconv.Itoa(wt.prNumber))
		}
		sync := worktreeSyncBadge(wt)

		statusWidth := max(8, width*20/100)
		statusPart := ""
		if prStatus != "" {
			segment := ansi.Truncate(prStatus, statusWidth, "…")
			if focused {
				segment = focusStyle.Render(ansi.Strip(segment))
			}
			statusPart += " " + segment
		}
		if sync != "" {
			segment := sync
			if focused {
				segment = focusStyle.Render(ansi.Strip(segment))
			}
			statusPart += " " + segment
		}
		statusPart = ansi.Truncate(statusPart, min(statusWidth, max(1, width/2-1)), "…")

		leftWidth := max(1, width/2)
		branchPrefix := selected + current + " "
		branchWidth := max(1, leftWidth-lipgloss.Width(statusPart)-1-lipgloss.Width(branchPrefix))
		branchPart := branchPrefix + truncate(wt.branch, branchWidth)

		pathPart := mutedStyle.Render(displayPath(wt.path, os.Getenv("HOME")))
		if wt.current {
			pathPart = dimStyle.Render("← here")
		}
		if focused {
			branchPart = focusStyle.Render(ansi.Strip(branchPart))
		}
		left := branchPart + statusPart
		right := pathPart
		return padColumnsAt(left, right, width, 50)
	}
	if r.windowIndex >= 0 {
		window := e.tabs[r.tabIndex].windows[r.windowIndex]
		if m.filter == filterAgents {
			return m.renderAgentRow(e, e.tabs[r.tabIndex], window, width)
		}
		branch := "├─"
		if r.windowIndex == len(e.tabs[r.tabIndex].windows)-1 {
			branch = "└─"
		}
		nameWidth := max(8, width*45/100-17)
		left := "           " + branch + " " + windowIcon(window) + " " + truncate(window.title, nameWidth)
		detail := window.detail
		if width >= 52 {
			return padColumns(left, dimStyle.Render(detail), width)
		}
		return ansi.Truncate(left, width, "…")
	}
	if r.tabIndex >= 0 {
		tab := e.tabs[r.tabIndex]
		branch := "├─"
		if r.tabIndex == len(e.tabs)-1 {
			branch = "└─"
		}
		arrow := " "
		if len(tab.windows) > 0 {
			arrow = "▸"
			if tab.expanded {
				arrow = "▾"
			}
		}
		nameWidth := max(8, width*45/100-17)
		left := fmt.Sprintf("       %s %s %s %s", branch, arrow, projectStyle.Render("󱂬"), truncate(tab.title, nameWidth))
		return ansi.Truncate(left, width, "…")
	}
	selection := " "
	if m.selected[e.key] {
		selection = accentStyle.Render("✓")
	}
	arrow := " "
	if len(e.tabs) > 0 {
		arrow = "▸"
		if e.expanded {
			arrow = "▾"
		}
	}
	nameWidth := max(8, width*45/100-18)
	iconGlyph := ""
	if e.kind == "workspace" {
		iconGlyph = ""
	}
	icon := projectStyle.Render(iconGlyph)
	if e.kind == "ssh" {
		iconGlyph = ""
		icon = sshStyle.Render(iconGlyph)
	}
	name := truncate(e.name, nameWidth)
	if e.open {
		icon = openStyle.Render(iconGlyph)
		name = openStyle.Bold(true).Render(name)
	}
	pin := "   "
	if e.pin != "" {
		pin = accentStyle.Render("[" + e.pin + "]")
	}
	if focused && e.open {
		selection = focusStyle.Render(ansi.Strip(selection))
		pin = focusStyle.Render(ansi.Strip(pin))
		arrow = focusStyle.Render(arrow)
	}
	left := fmt.Sprintf("%s   %s %s %s %s", selection, pin, arrow, icon, name)
	// A live session (tabs present) can span multiple tabs and windows across
	// different directories, so a single folder path on the row misrepresents
	// it. Saved snapshots can contain multiple folders too, so keep their rows
	// focused on the name; the Saved filter already identifies them. A PR is
	// useful metadata, though, and fills the otherwise empty column.
	if summary := entryPRColumn(e); summary != "" && width >= 52 {
		detail := dimStyle.Render(summary)
		if focused && e.open {
			detail = focusStyle.Render(ansi.Strip(detail))
		}
		return padColumns(left, detail, width)
	}
	if e.saved || (len(e.tabs) > 0 && e.kind != "ssh") {
		return ansi.Truncate(left, width, "…")
	}
	detailValue := e.detail
	detail := dimStyle.Render(detailValue)
	if focused && e.open {
		detail = focusStyle.Render(ansi.Strip(detail))
	}
	if width >= 52 {
		return padColumns(left, detail, width)
	}
	return ansi.Truncate(left, width, "…")
}

func agentStatusLabel(status string) string {
	switch status {
	case "working":
		return "Working"
	case "finished":
		return "Finished"
	case "errored":
		return "Error"
	case "idle":
		return "Idle"
	default:
		return ""
	}
}

var agentSpinnerFrames = []string{"◐", "◓", "◑", "◒"}

func (m model) agentStatusBadge(status string) string {
	switch status {
	case "working":
		return accentStyle.Render(agentSpinnerFrames[m.agentSpinnerFrame%len(agentSpinnerFrames)])
	case "finished":
		return openStyle.Render("✓")
	case "errored":
		return errorStyle.Render("!")
	case "idle":
		return mutedStyle.Render("•")
	default:
		return ""
	}
}

func prStatusIcon(status string) string {
	switch status {
	case "open":
		return prOpenStyle.Render("")
	case "merged":
		return prMergedStyle.Render("")
	case "closed":
		return prClosedStyle.Render("×")
	default:
		return ""
	}
}

func (m model) renderAgentRow(e entry, tab tabItem, window windowItem, width int) string {
	status := m.agentStatusBadge(window.agentStatus)
	if status == "" {
		status = " "
	}
	prefix := status + "  "

	title := window.title
	if title == "" {
		title = e.name
	}
	if title == "" {
		title = tab.title
	}
	metadata := agentLabel(window.agent) + " · " + compactLastActive(window.lastFocused, m.lastFocusedReference())
	right := dimStyle.Render(metadata)

	nameWidth := max(8, width-lipgloss.Width(prefix)-lipgloss.Width(right)-2)
	left := prefix + middleTruncate(title, nameWidth)
	gap := max(2, width-lipgloss.Width(left)-lipgloss.Width(right))
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, width, "…")
}

// Kitty reports last_focused_at as seconds since Kitty started, not a Unix
// timestamp. The most recently focused window is therefore our best available
// reference point when the picker opens.
func (m model) lastFocusedReference() float64 {
	var latest float64
	for _, entry := range m.entries {
		for _, tab := range entry.tabs {
			for _, window := range tab.windows {
				latest = max(latest, window.lastFocused)
			}
		}
	}
	return latest
}

func compactLastActive(lastFocused, reference float64) string {
	if lastFocused <= 0 || reference <= 0 {
		return "?"
	}
	elapsed := time.Duration(max(0, reference-lastFocused) * float64(time.Second))
	if elapsed < time.Minute {
		return "now"
	}
	if elapsed < time.Hour {
		return fmt.Sprintf("%dm", int(elapsed/time.Minute))
	}
	if elapsed < 24*time.Hour {
		return fmt.Sprintf("%dh", int(elapsed/time.Hour))
	}
	return fmt.Sprintf("%dd", int(elapsed/(24*time.Hour)))
}

func relativeLastActive(lastFocused, reference float64) string {
	if lastFocused <= 0 || reference <= 0 {
		return "unknown"
	}
	elapsed := time.Duration(max(0, reference-lastFocused) * float64(time.Second))
	if elapsed < time.Minute {
		return "just now"
	}
	if elapsed < time.Hour {
		return fmt.Sprintf("%dm ago", int(elapsed/time.Minute))
	}
	if elapsed < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(elapsed/time.Hour))
	}
	return fmt.Sprintf("%dd ago", int(elapsed/(24*time.Hour)))
}

func agentLabel(agent string) string {
	labels := map[string]string{"pi": "pi", "codex": "Codex", "claude": "Claude"}
	parts := strings.Split(agent, ",")
	for index, part := range parts {
		label, ok := labels[part]
		if !ok {
			return agent
		}
		parts[index] = label
	}
	return strings.Join(parts, "+")
}

const shellIcon = ""

func windowIcon(window windowItem) string {
	if icon := agentIcon(window.agent); icon != "" {
		return icon
	}
	if icon := processIcon(window.command); icon != "" {
		return icon
	}
	return shellIcon
}

func agentIcon(agent string) string {
	icons := map[string]string{"pi": "π", "codex": "󰚩", "claude": "✦"}
	parts := strings.Split(agent, ",")
	var icon strings.Builder
	for _, part := range parts {
		value, ok := icons[part]
		if !ok {
			return ""
		}
		icon.WriteString(value)
	}
	return icon.String()
}

func processIcon(command string) string {
	switch strings.TrimPrefix(command, "-") {
	case "nvim", "vim":
		return ""
	case "zsh", "bash", "sh", "fish":
		return shellIcon
	default:
		return ""
	}
}

func padColumns(left, right string, width int) string {
	space := width*45/100 - lipgloss.Width(left)
	if space < 2 {
		space = 2
	}
	return ansi.Truncate(left+strings.Repeat(" ", space)+right, width, "…")
}

func padColumnsAt(left, right string, width, splitPercent int) string {
	if width <= 0 {
		return ""
	}
	gap := 2
	leftWidth := max(1, min(width-gap, width*splitPercent/100))
	rightWidth := width - leftWidth - gap
	left = ansi.Truncate(left, leftWidth, "…")
	if rightWidth <= 0 {
		return left
	}
	right = ansi.Truncate(right, rightWidth, "…")
	return left + strings.Repeat(" ", max(0, leftWidth-lipgloss.Width(left))+gap) + right
}

func truncate(value string, width int) string {
	if width <= 1 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width-1]) + "…"
}
