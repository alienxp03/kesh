package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func sessionListModel() model {
	return model{
		filter: filterSessions, width: 100, height: 30,
		selected: map[string]bool{},
		entries: []entry{
			{key: "open", name: "active", kind: "workspace", session: "active", open: true},
			{key: "saved", name: "saved", kind: "workspace", saved: true},
			{key: "project", name: "kesh", kind: "project", path: "/repo/kesh"},
			{key: "another", name: "dotfiles", kind: "project", path: "/repo/dotfiles"},
			{key: "ssh", name: "server", kind: "ssh"},
			{key: "unscoped", name: "unscoped-agent", kind: "workspace", open: true, agent: "pi"},
		},
	}
}

func sessionKey(t *testing.T, m model, key tea.KeyMsg) model {
	t.Helper()
	updated, _ := m.Update(key)
	return updated.(model)
}

func TestMainListShowsOnlyOpenNamedAndSavedSessions(t *testing.T) {
	m := sessionListModel()
	m.rebuildRows()
	if len(m.rows) != 2 {
		t.Fatalf("session rows = %d, want 2", len(m.rows))
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"active", "saved", "Projects (2)"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
	for _, unwanted := range []string{"kesh", "dotfiles", "server", "unscoped-agent", "Work (", "other", "AGENTS", "[All]"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("session view contains %q", unwanted)
		}
	}
}

func TestEmptySessionList(t *testing.T) {
	m := sessionListModel()
	m.entries = nil
	m.zoxidePending = true
	m.rebuildRows()
	if len(m.rows) != 0 {
		t.Fatal("empty catalog must have no project rows")
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"No projects", "n create session"} {
		if !strings.Contains(view, want) {
			t.Errorf("empty state missing %q", want)
		}
	}
	for _, unwanted := range []string{"Work", "other", "AGENTS", "Search", "loading", "kesh"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("empty state contains %q", unwanted)
		}
	}
}

func TestSessionCreationStartsProjectSearch(t *testing.T) {
	m := sessionListModel()
	m.rebuildRows()
	m = sessionKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.filter != filterProjects || m.mode != modeSearch || len(m.rows) != 2 {
		t.Fatalf("n did not open project search: filter=%d mode=%d rows=%d", m.filter, m.mode, len(m.rows))
	}
	m = sessionKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("kesh")})
	if len(m.rows) != 1 || m.entries[m.rows[0].entryIndex].key != "project" {
		t.Fatal("search did not select the matching zoxide project")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.mode == modeCreateSession || !m.selected["project"] || cmd == nil {
		t.Fatal("enter must create the session without asking for a name")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("escape must close the picker")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("escape must quit Kesh")
	}
}

func TestEscapeClosesSessionListAndProjectPicker(t *testing.T) {
	for _, filter := range []int{filterSessions, filterProjects} {
		for _, mode := range []modeKind{modeNormal, modeSearch} {
			m := sessionListModel()
			m.filter = filter
			m.activateMode(mode)
			m.rebuildRows()
			_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			if cmd == nil {
				t.Fatalf("escape did not quit: filter=%d mode=%d", filter, mode)
			}
			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatalf("escape command did not quit: filter=%d mode=%d", filter, mode)
			}
		}
	}
}

func TestProjectPickerSearchIsInsideList(t *testing.T) {
	m := sessionListModel()
	m.filter = filterProjects
	m.activateMode(modeSearch)
	m.query = "kesh"
	m.rebuildRows()
	view := ansi.Strip(m.View())
	title := strings.Index(view, "Select projects")
	search := strings.Index(view, "Search: kesh")
	folder := strings.Index(view, "/repo/kesh")
	if title < 0 || search <= title || folder <= search {
		t.Fatalf("search must sit between picker title and folders:\n%s", view)
	}
	if !strings.Contains(view, "▌") || strings.Contains(view, ">") {
		t.Fatalf("picker must use a cursor bar, not a tree-like pointer:\n%s", view)
	}
	if strings.Count(view, "Search") != 1 {
		t.Fatalf("search must appear only once:\n%s", view)
	}
}

func TestSessionListSearchIsInsideList(t *testing.T) {
	for _, width := range []int{60, 100, 180} {
		for _, searching := range []bool{false, true} {
			m := sessionListModel()
			m.width = width
			if searching {
				m.activateMode(modeSearch)
				m.query = "active"
			}
			m.rebuildRows()
			view := ansi.Strip(m.View())
			title := strings.Index(view, "Projects (")
			search := strings.Index(view, "Search:")
			if !searching {
				if search >= 0 || !strings.Contains(view, "/ search") {
					t.Fatalf("idle session list must show only the footer search hint:\n%s", view)
				}
				continue
			}
			if title < 0 || search <= title || strings.Count(view, "Search") != 1 {
				t.Fatalf("search outside session panel (width=%d searching=%v):\n%s", width, searching, view)
			}
			if !strings.Contains(view, "▌") || strings.Contains(view, ">") {
				t.Fatalf("session selection must use a cursor bar:\n%s", view)
			}
			titleColumn, searchColumn := -1, -1
			for _, line := range strings.Split(view, "\n") {
				if column := strings.Index(line, "[1]─Projects ("); column >= 0 {
					titleColumn = column
				}
				if column := strings.Index(line, "Search:"); column >= 0 {
					searchColumn = column
				}
			}
			if titleColumn < 0 || searchColumn < 0 || searchColumn > titleColumn {
				t.Fatalf("search must align with panel title:\n%s", view)
			}
		}
	}
}

func TestProjectPickerShowsFoldersOnly(t *testing.T) {
	m := sessionListModel()
	m.filter = filterProjects
	m.entries = []entry{{
		key: "project", name: "aurora", path: "/workspace/aurora", kind: "project",
		open: true, expanded: true,
		tabs: []tabItem{{title: "terminal-tab", expanded: true, windows: []windowItem{{title: "agent-window", agent: "pi", agentStatus: "done"}}}},
	}}
	m.rebuildRows()
	if len(m.rows) != 1 {
		t.Fatalf("picker rows = %d, want one folder", len(m.rows))
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"aurora", "/workspace/aurora"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker missing %q", want)
		}
	}
	for _, unwanted := range []string{"▸", "▾", "Done", "terminal-tab", "agent-window"} {
		if strings.Contains(view, unwanted) {
			t.Errorf("picker contains session detail %q", unwanted)
		}
	}
}

func TestMainViewIncludesSessionsAndUnscopedAgents(t *testing.T) {
	m := sessionListModel()
	m.entries[0].tabs = []tabItem{{id: 1, windows: []windowItem{{id: 11, agent: "pi", agentStatus: "working", title: "Fix search"}}}}
	m.entries[0].expanded = true
	m.entries[5].tabs = []tabItem{{id: 2, windows: []windowItem{{id: 22, agent: "claude", agentStatus: "done", title: "Update config"}}}}
	m.rebuildRows()
	if len(m.rows) != 4 || m.rows[2].windowIndex < 0 || m.rows[3].windowIndex < 0 {
		t.Fatalf("want two sessions then two flat agent rows: %#v", m.rows)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"Projects (2)", "Agents (2)", "Fix search", "Update config"} {
		if !strings.Contains(view, want) {
			t.Errorf("main view missing %q:\n%s", want, view)
		}
	}
	m.mainPanelKey("2")
	m.query = "Fix search"
	m.rebuildRows()
	if len(m.rows) != 3 || m.rows[2].windowIndex < 0 {
		t.Fatalf("search must find agent task: %#v", m.rows)
	}
	m.query = ""
	m.rebuildRows()
	m = sessionKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.filter != filterAgents || len(m.rows) != 2 {
		t.Fatal("a must retain the dedicated Agents view")
	}
}

func TestSessionAgentColumnsStayCompact(t *testing.T) {
	m := sessionListModel()
	e := entry{name: "parent-session", session: "parent-session"}
	window := windowItem{agent: "pi", agentStatus: "working", title: "Fix search"}
	for _, width := range []int{100, 180, 300} {
		line := ansi.Strip(m.renderAgentRow(e, tabItem{}, window, width))
		if got := ansi.StringWidth(strings.SplitN(line, "Fix search", 2)[0]); got != 14 {
			t.Errorf("width %d: conversation starts at %d, want 14", width, got)
		}
		if got := ansi.StringWidth(strings.SplitN(line, "parent-session", 2)[0]); got != 56 {
			t.Errorf("width %d: session starts at %d, want 56", width, got)
		}
	}
	window.title = strings.Repeat("x", 100)
	e.name = strings.Repeat("y", 100)
	for _, width := range []int{20, 40, 80, 180} {
		line := m.renderAgentRow(e, tabItem{}, window, width)
		if got := ansi.StringWidth(line); got > min(width, 80) {
			t.Errorf("width %d: row width %d exceeds column caps", width, got)
		}
	}
}

func TestProjectSearchAllowsMultipleProjects(t *testing.T) {
	m := sessionListModel()
	m.rebuildRows()
	m = sessionKey(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = sessionKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	m = sessionKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = sessionKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.mode == modeCreateSession || len(m.selectedEntries()) != 2 || cmd == nil {
		t.Fatal("selected projects must be kept for session creation")
	}
}
