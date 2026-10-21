package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func lipglossWidth(s string) int { return lipgloss.Width(s) }

func wsTestSource(dir, name string, worktrees ...worktreeItem) wsRepoSource {
	return wsRepoSource{dir: dir, mainPath: dir, name: name, worktrees: worktrees}
}

func wsWindows(specs ...[4]string) []wsWindow {
	windows := make([]wsWindow, 0, len(specs))
	for i, s := range specs {
		windows = append(windows, wsWindow{
			windowID: i + 1, cwd: s[0], agent: s[1], agentStatus: s[2],
			since: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		})
	}
	return windows
}

func TestBuildRepoGroupsStatesAndOrder(t *testing.T) {
	sources := []wsRepoSource{
		wsTestSource("/root/oweb/kesh", "kesh",
			worktreeItem{path: "/wt/kesh/main", branch: "main", isDefault: true},
			worktreeItem{path: "/wt/kesh/fix", branch: "fix/auth", head: "h1"},
			worktreeItem{path: "/wt/kesh/run", branch: "feat/run"}),
		wsTestSource("/root/oweb/dot", "dot",
			worktreeItem{path: "/wt/dot/chore", branch: "chore/deps"}),
	}
	windows := wsWindows(
		[4]string{"/wt/kesh/main", "", ""},
		[4]string{"/wt/kesh/fix", "codex", "finished"},
		[4]string{"/wt/kesh/run", "pi", "working"},
	)

	groups := buildRepoGroups(sources, windows)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	// kesh has a blocked agent, so it sorts first.
	if groups[0].name != "kesh" {
		t.Fatalf("first group = %s, want kesh", groups[0].name)
	}
	if groups[0].blocked != 1 || groups[0].working != 1 {
		t.Fatalf("kesh rollups = %d blocked, %d working; want 1, 1", groups[0].blocked, groups[0].working)
	}
	// Inside kesh: default branch first, then blocked, then working, then open/idle.
	byBranch := map[string]wsWorktree{}
	for _, wt := range groups[0].worktrees {
		byBranch[wt.item.branch] = wt
	}
	if byBranch["fix/auth"].state != "blocked" {
		t.Fatalf("fix/auth state = %q, want blocked", byBranch["fix/auth"].state)
	}
	if byBranch["feat/run"].state != "working" {
		t.Fatalf("feat/run state = %q, want working", byBranch["feat/run"].state)
	}
	if byBranch["main"].state != "open" {
		t.Fatalf("main state = %q, want open (window cwd inside)", byBranch["main"].state)
	}
	if byBranch["main"].windowID == 0 {
		t.Fatalf("main should carry its live window id")
	}
	if got := groups[1].worktrees[0].state; got != "idle" {
		t.Fatalf("dot/chore state = %q, want idle (no windows at all)", got)
	}
}

func TestBuildRepoGroupsOpenWithoutAgent(t *testing.T) {
	sources := []wsRepoSource{wsTestSource("/r/o/dot", "dot",
		worktreeItem{path: "/wt/dot/chore", branch: "chore/deps"})}
	groups := buildRepoGroups(sources, wsWindows([4]string{"/wt/dot/chore", "", ""}))
	if got := groups[0].worktrees[0].state; got != "open" {
		t.Fatalf("state = %q, want open", got)
	}
}

func TestWsAttributionDeepestPathWins(t *testing.T) {
	worktrees := []worktreeItem{
		{path: "/wt/repo"}, {path: "/wt/repo/nested"},
	}
	owner := wsAttribution(worktrees, wsWindows([4]string{"/wt/repo/nested/deep", "", ""}))
	if owner[1] != 1 {
		t.Fatalf("attribution = %v, want window 1 in worktree 1", owner)
	}
}

func TestFlattenAgentsBlockedFirstOldest(t *testing.T) {
	old := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	recent := old.Add(5 * time.Minute)
	sources := []wsRepoSource{
		wsTestSource("/r/a", "a", worktreeItem{path: "/wt/a/one", branch: "one"}, worktreeItem{path: "/wt/a/two", branch: "two"}),
	}
	windows := []wsWindow{
		{windowID: 1, cwd: "/wt/a/one", agent: "codex", agentStatus: "finished", since: old},
		{windowID: 2, cwd: "/wt/a/two", agent: "claude", agentStatus: "finished", since: recent},
	}
	groups := buildRepoGroups(sources, windows)
	flat := flattenAgents(groups)
	if len(flat) != 2 {
		t.Fatalf("flat = %d rows, want 2", len(flat))
	}
	if flat[0].worktree != "one" {
		t.Fatalf("first flat row = %s, want the older blocked worktree (one)", flat[0].worktree)
	}
}

func TestRebuildWorkRowsSectionsAndHunt(t *testing.T) {
	m := model{
		filter:        filterWork,
		wsScanDirs:    []scanRepo{{dir: "/root/oweb/kesh", owner: "oweb", repo: "kesh"}},
		wsScan:        map[string][]worktreeItem{"/root/oweb/kesh": {}},
		showAgentPane: true,
	}
	// Seed the scan with the worktrees a porcelain fetch would return.
	m.wsScan["/root/oweb/kesh"] = []worktreeItem{
		{path: "/wt/kesh/main", branch: "main", isDefault: true, current: true},
		{path: "/wt/kesh/fix", branch: "fix/auth"},
	}
	m.entries = []entry{
		{key: "ssh-1", kind: "ssh", name: "bastion"},
	}
	// No windows yet: the idle repo starts collapsed. Force it open to verify
	// the row assembly, then exercise hunt below.
	m.wsExpanded = map[string]bool{"/wt/kesh/main": true}
	m.rebuildRows()

	var repoRows, wtRows, otherRows int
	for _, r := range m.rows {
		switch {
		case r.section == "ws" && r.wi < 0:
			repoRows++
		case r.section == "ws":
			wtRows++
		case r.section == "ws-other":
			otherRows++
		}
	}
	if repoRows != 1 || wtRows != 2 || otherRows != 1 {
		t.Fatalf("rows = %d repo, %d worktree, %d other; want 1, 2, 1", repoRows, wtRows, otherRows)
	}

	// Hunt flattens to blocked-only rows once an agent is blocked.
	m.agentSince = map[int]time.Time{}
	m.entries = append(m.entries, entry{
		key: "s", kind: "workspace", open: true, path: "/wt/kesh/fix",
		tabs: []tabItem{{windows: []windowItem{{id: 9, cwd: "/wt/kesh/fix", agent: "codex", agentStatus: "finished"}}}},
	})
	m.rebuildRows()
	m.wsHunt = true
	m.rebuildRows()
	if len(m.rows) != 1 || m.rows[0].section != "ws" || m.rows[0].wi < 0 {
		t.Fatalf("hunt rows = %#v, want a single blocked worktree row", m.rows)
	}
	group, wt, ok := m.wsRow(m.rows[0])
	if !ok || group.name != "kesh" || wt.item.branch != "fix/auth" {
		t.Fatalf("hunt row resolves %#v/%#v, want kesh/fix/auth", group.name, wt.item.branch)
	}
}

func TestRebuildWorkRowsQueryFiltersAndHidesOther(t *testing.T) {
	m := model{filter: filterWork}
	m.wsScanDirs = []scanRepo{{dir: "/root/o/kesh", owner: "o", repo: "kesh"}}
	m.wsScan = map[string][]worktreeItem{
		"/root/o/kesh": {
			{path: "/wt/kesh/main", branch: "main", isDefault: true},
			{path: "/wt/kesh/feat", branch: "feat/panel"},
		},
	}
	m.entries = []entry{{key: "ssh-1", kind: "ssh", name: "bastion"}}
	m.rebuildRows()

	m.query = "panel"
	m.rebuildRows()
	if len(m.rows) != 2 {
		t.Fatalf("query rows = %d (repo + match), want 2: %#v", len(m.rows), m.rows)
	}
	_, wt, ok := m.wsRow(m.rows[1])
	if !ok || wt.item.branch != "feat/panel" {
		t.Fatalf("query match = %#v, want feat/panel", wt.item)
	}
}

func TestRenderWorkRowIsWidthSafe(t *testing.T) {
	m := model{}
	m.wsRepos = []wsRepo{{
		dir: "/r", mainPath: "/r", name: "kesh",
		worktrees: []wsWorktree{
			{item: worktreeItem{path: "/wt/fix", branch: "fix/a-very-long-branch-name-here", prNumber: 342, prStatus: "open", behind: 2}, state: "blocked"},
			{item: worktreeItem{path: "/wt/main", branch: "main", current: true, isDefault: true}, state: "open"},
		},
		blocked: 1,
	}}
	repo := m.renderWorkRow(row{section: "ws", gi: 0, wi: -1}, 80, true)
	wt := m.renderWorkRow(row{section: "ws", gi: 0, wi: 0}, 80, false)
	for _, rendered := range []string{repo, wt} {
		if width := lipglossWidth(rendered); width > 80 {
			t.Fatalf("row overflows: %d cells: %q", width, rendered)
		}
	}
	if !strings.Contains(ansi.Strip(wt), "fix/a-very-long-branch") || !strings.Contains(ansi.Strip(wt), "#342") {
		t.Fatalf("worktree row missing branch/PR: %q", ansi.Strip(wt))
	}
	if !strings.Contains(ansi.Strip(repo), "2 trees") {
		t.Fatalf("repo row missing tree count: %q", ansi.Strip(repo))
	}
}

func TestAgentPaneViewBlockedFirstAndDense(t *testing.T) {
	m := model{}
	m.wsRepos = []wsRepo{{
		dir: "/r", mainPath: "/r", name: "aurora",
		worktrees: []wsWorktree{
			{item: worktreeItem{path: "/wt/auth", branch: "auth"}, state: "blocked",
				agents: []wsAgent{{windowID: 1, tool: "codex", status: "finished", since: time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)}}},
			{item: worktreeItem{path: "/wt/prev", branch: "preview"}, state: "working",
				agents: []wsAgent{{windowID: 2, tool: "pi", status: "working", since: time.Date(2024, 1, 1, 9, 30, 0, 0, time.UTC)}}},
		},
	}}
	pane := ansi.Strip(m.agentPaneView(80, 8))
	if !strings.Contains(pane, "AGENTS · 1 blocked") {
		t.Fatalf("pane header wrong: %q", pane)
	}
	if strings.Index(pane, "auth · aurora") > strings.Index(pane, "prev · aurora") {
		t.Fatalf("blocked agent must sort first:\n%s", pane)
	}
	if !strings.Contains(pane, "codex") || !strings.Contains(pane, "pi") {
		t.Fatalf("pane missing agent names:\n%s", pane)
	}
}

func TestWorkRowActionsResolveByPath(t *testing.T) {
	m := model{}
	m.wsRepos = []wsRepo{{
		dir: "/root/o/kesh", mainPath: "/main/kesh", name: "kesh",
		worktrees: []wsWorktree{{item: worktreeItem{path: "/wt/fix", branch: "fix"}, state: "blocked"}},
	}}
	r := row{section: "ws", gi: 0, wi: 0}
	if dir := m.worktreeDirectory(r); dir != "/main/kesh" {
		t.Fatalf("worktreeDirectory = %q, want the repo main path", dir)
	}
	wt, ok := m.worktreeForRow(r)
	if !ok || wt.path != "/wt/fix" {
		t.Fatalf("worktreeForRow = %#v ok=%v", wt, ok)
	}
}

func TestToggleWsRowPersistsCollapse(t *testing.T) {
	m := model{filter: filterWork}
	m.wsScanDirs = []scanRepo{{dir: "/root/o/kesh", owner: "o", repo: "kesh"}}
	m.wsScan = map[string][]worktreeItem{
		"/root/o/kesh": {{path: "/wt/kesh/main", branch: "main", isDefault: true}},
	}
	// An open window inside the worktree makes the repo active, so it
	// auto-expands until the user explicitly collapses it.
	m.entries = []entry{{
		key: "s", kind: "workspace", open: true, path: "/wt/kesh/main",
		tabs: []tabItem{{windows: []windowItem{{id: 3, cwd: "/wt/kesh/main"}}}},
	}}
	m.rebuildRows()
	if !m.wsRepoExpanded(m.wsRepos[0]) {
		t.Fatalf("active repo should auto-expand")
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := updated.(model)
	if m2.wsRepoExpanded(m2.wsRepos[0]) {
		t.Fatalf("enter on repo row should collapse it")
	}
	if v, ok := m2.wsExpanded["/wt/kesh/main"]; !ok || v {
		t.Fatalf("collapse state should be recorded as explicit false (got %v, present %v)", v, ok)
	}
	updated, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m3 := updated.(model)
	if !m3.wsRepoExpanded(m3.wsRepos[0]) {
		t.Fatalf("second enter should re-expand")
	}
}

func TestScanRootDiscoversOwnerRepoDirs(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"oweb/kesh", "oweb/dotfiles", "other/web", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	msg := scanWorktreeRoot(root)()
	got := msg.(scanReposMsg)
	if got.err != nil {
		t.Fatalf("scan error: %v", got.err)
	}
	if len(got.repos) != 3 {
		t.Fatalf("repos = %#v, want 3 (hidden skipped)", got.repos)
	}
	names := map[string]bool{}
	for _, repo := range got.repos {
		names[repo.owner+"/"+repo.repo] = true
	}
	if !names["oweb/kesh"] || !names["other/web"] {
		t.Fatalf("missing repos in %#v", names)
	}
}
