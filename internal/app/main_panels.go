package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// mainRowKey keeps selection stable when asynchronous discovery adds groups.
func (m model) mainRowKey(index int) string {
	if index < 0 || index >= len(m.rows) {
		return ""
	}
	r := m.rows[index]
	if r.mainKey != "" {
		return r.mainKey
	}
	if group, wt, ok := m.wsRow(r); ok {
		if r.wi >= 0 {
			return "tree:" + wt.item.path
		}
		return "repo:" + group.dir
	}
	if r.entryIndex < 0 || r.entryIndex >= len(m.entries) {
		return ""
	}
	if r.windowIndex >= 0 {
		return fmt.Sprintf("agent:%d", m.entries[r.entryIndex].tabs[r.tabIndex].windows[r.windowIndex].id)
	}
	return "session:" + m.entries[r.entryIndex].key
}

// rebuildScopedProjectRows changes layout, not eligibility. Discovery can name
// a repository or add branch/PR data, but cannot add a session or checkout.
func (m *model) rebuildScopedProjectRows() {
	metadata := m.wsRepoSources()
	var sources []wsRepoSource
	byPath := map[string][]int{}
	var standalone []int
	for i, e := range m.entries {
		if !m.matchesFilter(e) {
			continue
		}
		if e.path == "" {
			standalone = append(standalone, i)
			continue
		}
		path := cleanWorktreePath(e.path)
		byPath[path] = append(byPath[path], i)
		if len(byPath[path]) > 1 {
			continue
		}
		source := wsRepoSource{dir: path, mainPath: path, name: filepath.Base(path)}
		item := worktreeItem{path: e.path, branch: e.name}
		best := 0
		for _, candidate := range metadata {
			for _, wt := range candidate.worktrees {
				base := cleanWorktreePath(wt.path)
				if (path == base || strings.HasPrefix(path, base+string(filepath.Separator))) && len(base) > best {
					best = len(base)
					source = candidate
					item = wt
					item.path = e.path
				}
			}
		}
		source.worktrees = []worktreeItem{item}
		merged := false
		for j := range sources {
			if cleanWorktreePath(sources[j].dir) == cleanWorktreePath(source.dir) {
				sources[j].worktrees = append(sources[j].worktrees, item)
				merged = true
				break
			}
		}
		if !merged {
			sources = append(sources, source)
		}
	}
	m.wsRepos = buildRepoGroups(sources, m.collectWsWindows())
	m.rows = nil
	matches := func(e entry, group wsRepo, wt wsWorktree) bool {
		return m.query == "" || strings.Contains(strings.ToLower(e.name+" "+e.originalName+" "+e.detail+" "+e.path+" "+group.name+" "+wt.item.branch), strings.ToLower(m.query))
	}
	for gi, group := range m.wsRepos {
		var children []row
		for wi, wt := range group.worktrees {
			for _, index := range byPath[cleanWorktreePath(wt.item.path)] {
				if matches(m.entries[index], group, wt) {
					children = append(children, row{entryIndex: index, tabIndex: -1, windowIndex: -1, section: "ws", gi: gi, wi: wi, mainKey: "session:" + m.entries[index].key})
				}
			}
		}
		if len(children) == 0 {
			continue
		}
		m.rows = append(m.rows, row{entryIndex: -1, tabIndex: -1, windowIndex: -1, section: "ws", gi: gi, wi: -1})
		if m.wsRepoExpanded(group) || m.query != "" {
			m.rows = append(m.rows, children...)
		}
	}
	for _, index := range standalone {
		if matches(m.entries[index], wsRepo{}, wsWorktree{}) {
			m.rows = append(m.rows, row{entryIndex: index, tabIndex: -1, windowIndex: -1, mainKey: "session:" + m.entries[index].key})
		}
	}
}

func (m *model) rebuildMainRows() {
	start, _ := m.mainPanelBounds()
	projectCursor, agentCursor := m.mainSessionCursor, m.mainAgentCursor
	if m.mainAgents {
		agentCursor = max(0, m.cursor-start)
	} else {
		projectCursor = m.cursor
	}
	projectKey, agentKey := m.mainRowKey(projectCursor), m.mainRowKey(start+agentCursor)
	projects := *m
	if m.mainAgents {
		projects.query = ""
	}
	projects.wsHunt = false
	projects.rebuildScopedProjectRows()
	m.wsRepos = projects.wsRepos
	agents := *m
	agents.rows = nil
	agents.cursor = 0
	if !m.mainAgents {
		agents.query = ""
	}
	agents.rebuildAgentRows()
	m.rows = append(append([]row(nil), projects.rows...), agents.rows...)
	split := len(projects.rows)
	m.mainSessionCursor = min(projectCursor, max(0, split-1))
	m.mainAgentCursor = min(agentCursor, max(0, len(agents.rows)-1))
	for i := range m.rows {
		key := m.mainRowKey(i)
		m.rows[i].mainKey = key
		if key != "" && key == projectKey && i < split {
			m.mainSessionCursor = i
		}
		if key != "" && key == agentKey && i >= split {
			m.mainAgentCursor = i - split
		}
	}
	m.cursor = m.mainSessionCursor
	if m.mainAgents {
		m.cursor = split + m.mainAgentCursor
	}
}

func (m model) mainPanelBounds() (int, int) {
	split := len(m.rows)
	for i, r := range m.rows {
		if r.windowIndex >= 0 {
			split = i
			break
		}
	}
	if m.mainAgents {
		return split, len(m.rows)
	}
	return 0, split
}

func (m *model) mainPanelKey(key string) bool {
	start, end := m.mainPanelBounds()
	switch key {
	case "tab", "shift+tab", "1", "2", "h", "l":
		if m.mainAgents {
			m.mainAgentCursor = max(0, m.cursor-start)
		} else {
			m.mainSessionCursor = m.cursor
		}
		active := !m.mainAgents
		if key == "1" || key == "h" {
			active = false
		}
		if key == "2" || key == "l" {
			active = true
		}
		m.mainAgents = active
		m.query = ""
		start, end = m.mainPanelBounds()
		cursor := m.mainSessionCursor
		if active {
			cursor = m.mainAgentCursor
		}
		m.cursor = start + min(cursor, max(0, end-start-1))
		m.rebuildRows()
	case "up", "ctrl+k", "k":
		m.cursor = max(start, m.cursor-1)
	case "down", "ctrl+j", "j":
		m.cursor = max(start, min(end-1, m.cursor+1))
	case "home", "g":
		m.cursor = start
	case "end", "G":
		m.cursor = max(start, end-1)
	case "pgup", "ctrl+u":
		m.cursor = max(start, m.cursor-m.listPageSize())
	case "pgdown", "ctrl+d":
		m.cursor = max(start, min(end-1, m.cursor+m.listPageSize()))
	default:
		return false
	}
	return true
}

func (m model) mainPanelsView() string {
	width := max(16, m.width-4)
	height := max(10, m.height-4)
	topHeight := max(4, height/2)
	bottomHeight := height - topHeight
	split := len(m.rows)
	for i, r := range m.rows {
		if r.windowIndex >= 0 {
			split = i
			break
		}
	}
	panel := func(agents bool, start, end, width, h int) string {
		active := m.mainAgents == agents
		title := fmt.Sprintf("[1]─Projects (%d)", end-start)
		if agents {
			title = fmt.Sprintf("[2]─Agents (%d)", end-start)
		}
		lines := []string{}
		if active && (m.mode == modeSearch || m.query != "") {
			value := m.query
			if m.mode == modeSearch {
				value += "█"
			}
			lines = append(lines, "Search: "+accentStyle.Render(value))
		}
		available := max(0, h-2-len(lines))
		cursor := m.mainSessionCursor
		if agents {
			cursor = m.mainAgentCursor
		}
		if active {
			cursor = m.cursor - start
		}
		first := max(start, start+cursor-available+1)
		first = min(first, max(start, end-available))
		if start == end {
			empty := "No projects — n create session"
			if agents {
				empty = "No agents"
			}
			if active && m.query != "" {
				empty = "No matches"
			}
			lines = append(lines, dimStyle.Render(empty))
		}
		for i := first; i < min(end, first+available); i++ {
			focused := active && i == m.cursor
			prefix := "  "
			if focused {
				prefix = projectStyle.Render("▌") + " "
			}
			lines = append(lines, prefix+m.renderRow(m.rows[i], max(8, width-4), focused))
		}
		color := lipgloss.Color("241")
		if active {
			color = lipgloss.Color("39")
		}
		for len(lines) < h-2 {
			lines = append(lines, "")
		}
		if len(lines) > h-2 {
			lines = lines[:h-2]
		}
		for i := range lines {
			lines[i] = ansi.Truncate(lines[i], width-2, "…")
		}
		rendered := lipgloss.NewStyle().Width(width - 2).Height(h - 2).Border(lipgloss.RoundedBorder()).BorderForeground(color).Render(strings.Join(lines, "\n"))
		rows := strings.Split(rendered, "\n")
		title = ansi.Truncate(title, width-3, "…")
		borderStyle := lipgloss.NewStyle().Foreground(color)
		rows[0] = borderStyle.Render("╭─") + title + borderStyle.Render(strings.Repeat("─", max(0, width-3-lipgloss.Width(title)))+"╮")
		return strings.Join(rows, "\n")
	}
	footer := "h/l panels  j/k move  / search  space expand  enter open  w worktree  n session  esc close"
	if m.err != nil {
		footer = "Error: " + m.err.Error()
	}
	bottom := panel(true, split, len(m.rows), width, bottomHeight)
	if m.mainAgents && m.showPreview && m.hasSelectedAgentWindow() && width >= 80 && bottomHeight >= 8 {
		listWidth := min(80, width/2)
		previewWidth := width - listWidth - 3
		divider := strings.TrimSuffix(strings.Repeat("│\n", bottomHeight), "\n")
		bottom = lipgloss.JoinHorizontal(lipgloss.Top,
			panel(true, split, len(m.rows), listWidth, bottomHeight), " ",
			dimStyle.Render(divider), " ", m.previewView(previewWidth, bottomHeight))
	}
	content := panel(false, 0, split, width, topHeight) + "\n" + bottom + "\n" + ansi.Truncate(footer, width, "…")
	if popup := m.popupView(width); popup != "" {
		content = strings.Join(overlayPopup(strings.Split(content, "\n"), popup, width), "\n")
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}
