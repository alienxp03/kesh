package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/alienxp03/kesh/internal/system"
)

func TestProjectsDiscoveryNeverWidensSessionScope(t *testing.T) {
	m := projectTreeModel()
	m.entries = append(m.entries,
		entry{key: "history", kind: "project", name: "history-only", path: "/history-only", worktreesLoaded: true, worktrees: []worktreeItem{{path: "/history-only", primary: true}}},
		entry{key: "unnamed", kind: "project", name: "unnamed-open", path: "/unnamed-open", open: true},
		entry{key: "closed", kind: "project", name: "closed-unsaved", path: "/closed-unsaved", session: "closed"},
		entry{key: "saved-copy", kind: "project", name: "saved-copy", path: "/src/kesh", saved: true},
	)
	m.entries[0].worktrees = append(m.entries[0].worktrees, worktreeItem{path: "/trees/kesh/unrelated-branch", branch: "unrelated-branch"})
	m.wsScanDirs = []scanRepo{{dir: "/scan-only", repo: "scan-only"}}
	m.wsScan = map[string][]worktreeItem{"/scan-only": {{path: "/scan-only", branch: "scan-only", primary: true}}}
	m.rebuildRows()
	view := ansi.Strip(m.View())
	for _, absent := range []string{"history-only", "unnamed-open", "closed-unsaved", "scan-only", "unrelated-branch"} {
		if strings.Contains(view, absent) {
			t.Errorf("unexpected %q in Projects:\n%s", absent, view)
		}
	}
	seen := map[string]bool{}
	for _, r := range m.rows {
		if r.entryIndex >= 0 {
			seen[m.entries[r.entryIndex].key] = true
		}
	}
	for _, key := range []string{"linked", "main", "saved-copy"} {
		if !seen[key] {
			t.Errorf("eligible session %q missing: %#v", key, m.rows)
		}
	}
	for _, group := range m.wsRepos {
		for _, wt := range group.worktrees {
			if strings.Contains(wt.item.path, "unrelated") || strings.Contains(wt.item.path, "scan-only") || strings.Contains(wt.item.path, "history-only") {
				t.Errorf("unscoped child: %#v", wt)
			}
		}
	}
}

func TestProjectsEnterRestoresSavedInstance(t *testing.T) {
	m := projectTreeModel()
	m.kitty = "kitty"
	m.entries[1].sessionFile = "/saved/main.kitty-session"
	m.rebuildRows()
	for i, r := range m.rows {
		if r.entryIndex == 1 {
			m.cursor = i
			break
		}
	}
	recorder := &appGitRunner{}
	restore := system.SetRunner(recorder)
	defer restore()
	_, cmd := m.openSelected()
	if cmd == nil {
		t.Fatal("saved instance did not schedule restore")
	}
	msg := cmd().(actionMsg)
	if msg.err != nil || len(recorder.specs) != 1 || !strings.Contains(strings.Join(recorder.specs[0].Args, " "), m.entries[1].sessionFile) {
		t.Fatalf("saved instance must restore its snapshot: %#v, %v", recorder.specs, msg.err)
	}
}

func TestProjectsPrefetchSkipsHistoryAndUnnamedSessions(t *testing.T) {
	entries := []entry{
		{kind: "project", path: "/history"},
		{kind: "project", path: "/unnamed", open: true},
		{kind: "project", path: "/closed", session: "closed"},
	}
	if prefetchEntryWorktrees(entries) != nil {
		t.Fatal("source-only folders must not schedule discovery")
	}
}
