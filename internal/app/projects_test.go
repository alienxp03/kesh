package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/alienxp03/kesh/internal/system"
)

func projectTreeModel() model {
	return model{filter: filterSessions, width: 120, height: 36,
		entries: []entry{
			{key: "linked", kind: "project", open: true, session: "fix-preview", name: "fix-preview", path: "/trees/kesh/fix-preview", worktreesLoaded: true, worktrees: []worktreeItem{
				{path: "/trees/kesh/fix-preview", branch: "fix-preview", current: true},
				{path: "/src/kesh", branch: "main", primary: true, isDefault: true},
			}},
			{key: "main", kind: "project", saved: true, name: "main", path: "/src/kesh", worktreesLoaded: true, worktrees: []worktreeItem{
				{path: "/src/kesh", branch: "main", primary: true}, {path: "/trees/kesh/fix-preview", branch: "fix-preview"},
			}},
			{key: "notes", kind: "project", open: true, session: "notes", name: "notes", path: "/notes"},
			{key: "dotfiles", kind: "project", open: true, session: "dotfiles", name: "dotfiles", path: "/src/dotfiles", worktreesLoaded: true, worktrees: []worktreeItem{{path: "/src/dotfiles", branch: "main", primary: true}}},
		}, wsExpanded: map[string]bool{"/src/kesh": true}, worktreeRoot: "/trees"}
}

func TestProjectsTreeGroupingSearchAndToggle(t *testing.T) {
	m := projectTreeModel()
	m.rebuildRows()
	if len(m.wsRepos) != 3 || len(m.rows) != 5 {
		t.Fatalf("groups=%#v rows=%#v", m.wsRepos, m.rows)
	}
	view := ansi.Strip(m.View())
	for _, want := range []string{"▾ kesh", "▸ dotfiles", "main", "fix-preview", "notes", "Projects"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q:\n%s", want, view)
		}
	}
	// Space on a child collapses its project, not the Agents pane.
	for i, r := range m.rows {
		if r.section == "ws" && r.wi >= 0 {
			m.cursor = i
			break
		}
	}
	m = sessionKey(t, m, tea.KeyMsg{Type: tea.KeySpace})
	if len(m.rows) != 3 || m.rows[m.cursor].wi >= 0 {
		t.Fatalf("collapse rows=%#v cursor=%d", m.rows, m.cursor)
	}
	m.query = "fix-preview"
	m.rebuildRows()
	if len(m.rows) != 2 {
		t.Fatalf("search must reveal collapsed matching child: %#v", m.rows)
	}
	_, wt, _ := m.wsRow(m.rows[1])
	if wt.item.branch != "fix-preview" {
		t.Fatal("wrong search result")
	}
	m.query = "notes"
	m.rebuildRows()
	if len(m.rows) != 2 || m.entries[m.rows[1].entryIndex].name != "notes" {
		t.Fatal("standalone folder search failed")
	}
}

func TestProjectsWorktreeCreateTargetsPrimaryCheckout(t *testing.T) {
	for _, child := range []bool{false, true} {
		m := projectTreeModel()
		m.rebuildRows()
		for i, r := range m.rows {
			if g, _, ok := m.wsRow(r); ok && g.name == "kesh" && (r.wi >= 0) == child {
				m.cursor = i
				break
			}
		}
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
		got := updated.(model)
		if got.mode != modeWorktreeCreate || got.wsCreateRepo != "/src/kesh" || cmd != nil {
			t.Fatalf("create did not reuse form: %#v", got)
		}
		got = sessionKey(t, got, tea.KeyMsg{Type: tea.KeyEsc})
		if got.filter != filterSessions || got.wsCreateRepo != "" {
			t.Fatal("cancel must return to Projects")
		}
	}
	m := projectTreeModel()
	m.rebuildRows()
	for i, r := range m.rows {
		if g, _, ok := m.wsRow(r); ok && g.name == "notes" {
			m.cursor = i
		}
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if updated.(model).err == nil {
		t.Fatal("non-Git folder must reject worktree creation")
	}
}

func TestProjectsAsyncScanPreservesBothSelections(t *testing.T) {
	m := projectTreeModel()
	m.entries[0].tabs = []tabItem{{windows: []windowItem{{id: 12, agent: "pi", cwd: "/trees/kesh/fix-preview"}}}}
	m.rebuildRows()
	for i, r := range m.rows {
		if _, wt, ok := m.wsRow(r); ok && wt.item.branch == "fix-preview" {
			m.cursor = i
		}
	}
	projectKey := m.mainRowKey(m.cursor)
	m.mainPanelKey("l")
	agentKey := m.mainRowKey(m.cursor)
	m.wsScanDirs = []scanRepo{{dir: "/trees/o/aaa", repo: "aaa"}}
	updated, _ := m.Update(scanWorktreeMsg{dir: "/trees/o/aaa", worktrees: []worktreeItem{{path: "/src/aaa", branch: "main", primary: true}}})
	m = updated.(model)
	if m.mainRowKey(m.cursor) != agentKey || !m.mainAgents {
		t.Fatal("scan moved agent selection")
	}
	m.mainPanelKey("h")
	if m.mainRowKey(m.cursor) != projectKey {
		t.Fatal("scan moved project selection")
	}
	// Zoxide reorders entries while scans are in flight. Results resolve by path.
	updated, cmd := m.Update(zoxideEntriesMsg{entries: []entry{{key: "new", name: "new", kind: "project", path: "/new"}}})
	m = updated.(model)
	if cmd != nil || m.mainRowKey(m.cursor) != projectKey {
		t.Fatal("zoxide merge lost selection or scheduled source-only discovery")
	}
	updated, _ = m.Update(prefetchedWorktreesMsg{result: worktreeListMsg{dir: "/new", entryIndex: 99, worktrees: []worktreeItem{{path: "/new", branch: "main", primary: true}}}})
	m = updated.(model)
	found := false
	for _, g := range m.wsRepos {
		if g.mainPath == "/new" {
			found = true
		}
	}
	if found {
		t.Fatal("late scan introduced a source-only folder into Projects")
	}
}

func TestProjectsEnterFocusesCheckoutNotSession(t *testing.T) {
	for _, child := range []bool{false, true} {
		m := projectTreeModel()
		m.kitty = "kitty"
		m.entries[1].saved = false
		m.entries[1].open = true
		m.entries[1].session = "main"
		m.entries[0].tabs = []tabItem{{windows: []windowItem{
			{id: 21, cwd: "/src/kesh/src"}, {id: 22, cwd: "/trees/kesh/fix-preview"},
		}}}
		m.rebuildRows()
		want := "id:21"
		for i, r := range m.rows {
			if g, wt, ok := m.wsRow(r); ok && g.name == "kesh" {
				if !child && r.wi < 0 {
					m.cursor = i
					break
				}
				if child && r.wi >= 0 && wt.item.branch == "fix-preview" {
					m.cursor = i
					want = "id:22"
					break
				}
			}
		}
		recorder := &appGitRunner{}
		restore := system.SetRunner(recorder)
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if cmd == nil {
			restore()
			t.Fatal("enter did not schedule connection")
		}
		msg := cmd().(actionMsg)
		restore()
		if msg.err != nil || len(recorder.specs) != 1 || !strings.Contains(strings.Join(recorder.specs[0].Args, " "), want) {
			t.Fatalf("enter must focus %s: calls=%#v err=%v", want, recorder.specs, msg.err)
		}
	}
}

func TestProjectsRealDiscoveryEnrichesOnlySavedCheckout(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "src", "kesh")
	managed := filepath.Join(root, "trees", "owner", "kesh")
	linked := filepath.Join(managed, "fix-preview")
	if err := os.MkdirAll(main, 0755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", main}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	git("init", "-b", "main")
	git("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
	git("worktree", "add", "-b", "fix-preview", linked)
	m := model{filter: filterSessions, entries: []entry{{key: "closed", kind: "project", saved: true, path: linked, name: "fix-preview"}}}
	cmd := prefetchEntryWorktrees(m.entries)
	if cmd == nil {
		t.Fatal("closed folders must be scanned")
	}
	// tea.Batch returns its single command directly.
	updated, _ := m.Update(cmd())
	m = updated.(model)
	if len(m.wsRepos) != 1 || !sameWorktreePath(m.wsRepos[0].mainPath, main) || len(m.wsRepos[0].worktrees) != 1 {
		t.Fatalf("closed checkout discovery: %#v", m.wsRepos)
	}
	repos := scanWorktreeRoot(filepath.Join(root, "trees"))().(scanReposMsg)
	if len(repos.repos) != 1 {
		t.Fatalf("root discovery: %#v", repos)
	}
	result := fetchScanWorktrees(managed)().(scanWorktreeMsg)
	if result.err != nil || len(result.worktrees) != 2 {
		t.Fatalf("managed container scan: %#v", result)
	}
	m.wsScanDirs = repos.repos
	updated, _ = m.Update(result)
	m = updated.(model)
	if len(m.wsRepos) != 1 || len(m.wsRepos[0].worktrees) != 1 || !sameWorktreePath(m.wsRepos[0].worktrees[0].item.path, linked) {
		t.Fatalf("root discovery widened saved-session scope: %#v", m.wsRepos)
	}
}
