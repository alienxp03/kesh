package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestMainPanelsNavigationAndSearch(t *testing.T) {
	m := sessionListModel()
	m.entries[0].tabs = []tabItem{{windows: []windowItem{
		{id: 11, agent: "pi", agentStatus: "working", title: "Fix search"},
		{id: 12, agent: "claude", agentStatus: "done", title: "Update config"},
	}}}
	m.rebuildRows()
	m.mainPanelKey("down")
	m.mainPanelKey("down")
	if m.cursor != 1 {
		t.Fatal("navigation left Sessions panel")
	}
	m.mainPanelKey("tab")
	if !m.mainAgents || m.cursor != 2 {
		t.Fatal("Tab did not focus Agents")
	}
	m.mainPanelKey("down")
	m.mainPanelKey("1")
	if m.mainAgents || m.cursor != 1 {
		t.Fatal("Sessions cursor was not restored")
	}
	m.mainPanelKey("2")
	if m.cursor != 3 {
		t.Fatal("Agents cursor was not restored")
	}
	before := ansi.Strip(m.View())
	m.activateMode(modeSearch)
	m.query = "Fix search"
	m.rebuildRows()
	if len(m.rows) != 3 {
		t.Fatal("agent search must leave both sessions visible")
	}
	after := ansi.Strip(m.View())
	lineOf := func(view, text string) int {
		for i, line := range strings.Split(view, "\n") {
			if strings.Contains(line, text) {
				return i
			}
		}
		return -1
	}
	if lineOf(before, "[2]─Agents") != lineOf(after, "[2]─Agents") {
		t.Fatal("Agents panel moved during search")
	}
	if !strings.Contains(after, "Search: Fix search") {
		t.Fatal("active panel lacks search field")
	}
}

func TestMainPanelsHLNavigation(t *testing.T) {
	m := sessionListModel()
	m.showPreview = true
	m.entries[0].tabs = []tabItem{{windows: []windowItem{
		{id: 11, agent: "pi", title: "First"},
		{id: 12, agent: "pi", title: "Second"},
	}}}
	m.rebuildRows()
	press := func(key string) tea.Cmd {
		t.Helper()
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(model)
		return cmd
	}
	press("j")
	if cmd := press("l"); !m.mainAgents || m.cursor != 2 || cmd == nil || m.previewID != 11 {
		t.Fatal("l must select Agents and queue its preview")
	}
	press("j")
	press("h")
	if m.mainAgents || m.cursor != 1 {
		t.Fatal("h must restore Sessions cursor")
	}
	press("h")
	if m.mainAgents || m.cursor != 1 {
		t.Fatal("h in Sessions must keep focus and cursor")
	}
	press("l")
	if !m.mainAgents || m.cursor != 3 || m.previewID != 12 {
		t.Fatal("l must restore Agents cursor and preview")
	}
	press("l")
	if !m.mainAgents || m.cursor != 3 {
		t.Fatal("l in Agents must keep focus and cursor")
	}
}

func TestMainPanelTitlesAreInBorders(t *testing.T) {
	m := sessionListModel()
	m.rebuildRows()
	view := ansi.Strip(m.View())
	for _, title := range []string{"╭─[1]─Projects (2)", "╭─[2]─Agents (0)"} {
		if !strings.Contains(view, title) {
			t.Fatalf("missing border title %q:\n%s", title, view)
		}
	}
}

func TestMainAgentRowShowsStatusAndToolOnLeft(t *testing.T) {
	m := sessionListModel()
	line := ansi.Strip(m.renderAgentRow(entry{name: "aurora", session: "kesh-aurora"}, tabItem{}, windowItem{
		agent: "pi", agentStatus: "finished", title: "Fix search",
	}, 100))
	if !strings.HasPrefix(line, "✓ pi") || !strings.Contains(line, "Fix search") || !strings.HasSuffix(line, "aurora") || strings.Contains(line, "Done") || strings.Contains(line, "\n") {
		t.Fatalf("want status/tool, agent title, and parent session columns: %q", line)
	}
	unnamed := ansi.Strip(m.renderAgentRow(entry{name: "folder-name"}, tabItem{}, windowItem{agent: "pi", title: "Conversation"}, 100))
	if strings.Contains(unnamed, "folder-name") || !strings.HasSuffix(unnamed, "—") {
		t.Fatalf("unnamed windows must not use a folder as session name: %q", unnamed)
	}
}

func TestSessionRowHasNoHierarchyIndent(t *testing.T) {
	m := sessionListModel()
	m.rebuildRows()
	line := ansi.Strip(m.renderRow(m.rows[0], 80, true))
	if strings.HasPrefix(line, " ") || strings.Contains(line, "▸") || strings.Contains(line, "▾") {
		t.Fatalf("session row contains reserved hierarchy spacing: %q", line)
	}
}

func TestMainAgentNavigationQueuesPreview(t *testing.T) {
	m := sessionListModel()
	m.showPreview = true
	m.entries[0].tabs = []tabItem{{windows: []windowItem{
		{id: 11, agent: "pi", title: "First"},
		{id: 12, agent: "pi", title: "Second"},
	}}}
	m.rebuildRows()
	for i, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("2")},
		{Type: tea.KeyDown},
	} {
		updated, cmd := m.Update(key)
		m = updated.(model)
		if cmd == nil || !m.previewBusy || m.previewID != 11+i {
			t.Fatalf("navigation %d did not queue selected agent capture: id=%d busy=%v", i, m.previewID, m.previewBusy)
		}
	}
}

func TestMainAgentPreviewSplitsBottomOnly(t *testing.T) {
	m := sessionListModel()
	m.width, m.height = 180, 40
	m.showPreview = true
	m.preview = "live-terminal-marker"
	m.entries[0].tabs = []tabItem{{windows: []windowItem{{
		id: 11, agent: "pi", title: "Fix search", cwd: "/repo/kesh",
	}}}}
	m.rebuildRows()
	sessionsView := ansi.Strip(m.View())
	m.mainPanelKey("2")
	agentsView := ansi.Strip(m.View())
	before := strings.Split(sessionsView, "\n")
	after := strings.Split(agentsView, "\n")
	bottomStart := -1
	for i, line := range after {
		if ansi.StringWidth(line) > m.width {
			t.Fatalf("preview exceeds terminal width: %q", line)
		}
		if strings.Contains(line, "[2]─Agents") {
			bottomStart = i
		}
		if strings.Contains(line, "Agent screen") || strings.Contains(line, "live-terminal-marker") {
			if bottomStart < 0 || i <= bottomStart {
				t.Fatal("preview must stay inside bottom pane")
			}
		}
	}
	if bottomStart < 0 || !strings.Contains(agentsView, "live-terminal-marker") {
		t.Fatal("selected agent preview missing")
	}
	for i := 0; i < bottomStart; i++ {
		if strings.ReplaceAll(before[i], "▌", " ") != after[i] {
			t.Fatalf("Sessions layout changed on line %d", i)
		}
	}
	if strings.Contains(sessionsView, "live-terminal-marker") {
		t.Fatal("Sessions selection must not show an agent preview")
	}
	m.width = 60
	if strings.Contains(ansi.Strip(m.View()), "live-terminal-marker") {
		t.Fatal("narrow terminal must keep the list only")
	}
	m.width = 180
	m.showPreview = false
	if strings.Contains(ansi.Strip(m.View()), "live-terminal-marker") {
		t.Fatal("disabled preview must stay hidden")
	}
}

func TestAgentPreviewShowsLatestLines(t *testing.T) {
	m := sessionListModel()
	m.preview = "old-first\nold-second\nrecent-first\nrecent-second\nlatest-reply"
	view := ansi.Strip(m.previewView(40, 6))
	if strings.Contains(view, "old-first") || strings.Contains(view, "old-second") {
		t.Fatalf("preview shows old lines instead of the tail:\n%s", view)
	}
	for _, want := range []string{"recent-first", "recent-second", "latest-reply"} {
		if !strings.Contains(view, want) {
			t.Errorf("preview missing %q:\n%s", want, view)
		}
	}
	if got := len(strings.Split(view, "\n")); got != 6 {
		t.Errorf("preview height = %d, want 6", got)
	}
}

func TestMainProjectTreeShowsBranchWithoutRightColumn(t *testing.T) {
	m := sessionListModel()
	m.wsRepos = []wsRepo{{
		dir: "/repo/kesh", name: "kesh", blocked: 1,
		worktrees: []wsWorktree{{
			item:  worktreeItem{path: "/repo/kesh", branch: "feat/preview", prNumber: 123, behind: 2},
			state: "open",
		}},
	}}
	for _, width := range []int{40, 120} {
		header := ansi.Strip(m.renderWorkRow(row{section: "ws", gi: 0, wi: -1}, width, false))
		child := ansi.Strip(m.renderWorkRow(row{section: "ws", gi: 0, wi: 0, entryIndex: 0}, width, false))
		if !strings.Contains(child, "feat/preview") || strings.Contains(child, "active") {
			t.Fatalf("child must show branch, not session name: %q", child)
		}
		if strings.Contains(header, "tree") || strings.Contains(child, "#123") || strings.Contains(child, "  "+" ") {
			t.Fatalf("tree must not reserve a right column: header=%q child=%q", header, child)
		}
	}
}

func TestWorktreePopupShowsSelectedDirectory(t *testing.T) {
	m := sessionListModel()
	m.wsCreateRepo = "/repo/selected-project"
	m.beginWorktreeCreate()
	m.worktreeBranch = "fix/preview"
	view := ansi.Strip(m.popupView(100))
	if !strings.Contains(view, "Branch: fix/preview") || !strings.Contains(view, "Dir:    /repo/selected-project") {
		t.Fatalf("worktree popup must show branch and selected directory:\n%s", view)
	}
}

func TestMainPanelEmptyEnterIsSafe(t *testing.T) {
	m := sessionListModel()
	m.rebuildRows()
	m.mainPanelKey("2")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("empty Agents panel must not open a session")
	}
}
