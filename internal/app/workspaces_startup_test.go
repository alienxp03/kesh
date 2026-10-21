package app

import (
	"errors"
	"strings"
	"testing"
)

func TestWorkSurfacePrefetchSkipsNonGitFolder(t *testing.T) {
	dir := t.TempDir()
	m := model{
		filter:  filterWork,
		entries: []entry{{kind: "project", path: dir, open: true, session: "test"}},
	}
	cmd := prefetchEntryWorktrees(m.entries)
	if cmd == nil {
		t.Fatal("expected a background worktree check")
	}
	msg, ok := cmd().(prefetchedWorktreesMsg)
	if !ok || msg.result.err == nil {
		t.Fatalf("expected a failed background worktree check, got %#v", msg)
	}
	updated, _ := m.Update(msg)
	if got := updated.(model).err; got != nil {
		t.Fatalf("background check showed an error: %v", got)
	}

	// Background discovery must not replace errors from user actions.
	actionErr := errors.New("action failed")
	m.err = actionErr
	updated, _ = m.Update(msg)
	if got := updated.(model).err; got != actionErr {
		t.Fatalf("background check changed the action error: %v", got)
	}

	// The same failure from an explicit worktree action must remain visible.
	updated, _ = m.Update(msg.result)
	if got := updated.(model).err; got != msg.result.err {
		t.Fatalf("explicit check error = %v, want %v", got, msg.result.err)
	}
}

func TestWorkSurfacePrefetchLoadsWorktrees(t *testing.T) {
	m := model{
		filter:  filterWork,
		entries: []entry{{kind: "project", path: "/repo", open: true}},
	}
	updated, _ := m.Update(prefetchedWorktreesMsg{result: worktreeListMsg{
		dir: "/repo", worktrees: []worktreeItem{{path: "/repo", branch: "main"}},
	}})
	got := updated.(model)
	if !got.entries[0].worktreesLoaded || len(got.wsRepos) != 1 {
		t.Fatal("successful background check did not populate repository groups")
	}
}

func TestWorkSurfaceStartupSelection(t *testing.T) {
	const prURL = "https://github.com/alienxp03/kesh/pull/1"
	for _, tt := range []struct {
		name   string
		row    row
		path   string
		prURL  string
		branch string
	}{
		{
			name: "repository header",
			row:  row{section: "ws", entryIndex: -1, tabIndex: -1, windowIndex: -1, gi: 0, wi: -1},
			path: "/repo",
		},
		{
			name:   "worktree",
			row:    row{section: "ws", entryIndex: -1, tabIndex: -1, windowIndex: -1, gi: 0, wi: 0},
			path:   "/worktree",
			prURL:  prURL,
			branch: "fix/startup",
		},
		{
			name: "other header",
			row:  row{section: "ws-other", entryIndex: -1, tabIndex: -1, windowIndex: -1, wi: -1},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				filter: filterWork, width: 100, height: 30,
				showPreview: true, showAgentPane: true,
				rows: []row{tt.row},
				wsRepos: []wsRepo{{
					name: "kesh", mainPath: "/repo",
					worktrees: []wsWorktree{{item: worktreeItem{
						path: "/worktree", branch: "fix/startup", prURL: prURL,
					}}},
				}},
			}
			if got := m.selectedDetailPath(); got != tt.path {
				t.Fatalf("selectedDetailPath() = %q, want %q", got, tt.path)
			}
			if url, branch := m.selectedPullRequest(); url != tt.prURL || branch != tt.branch {
				t.Fatalf("selectedPullRequest() = (%q, %q), want (%q, %q)", url, branch, tt.prURL, tt.branch)
			}
			// Startup queues preview/PR work before the first terminal render.
			_ = m.queuePreview()
			if tt.path != "" && !m.pathPRChecked[tt.path] {
				t.Fatalf("startup did not queue PR lookup for %q", tt.path)
			}
			if m.hasSelectedAgentWindow() {
				t.Fatal("work surface row must not be treated as an agent window")
			}
			if view := m.View(); !strings.Contains(view, "Kesh") {
				t.Fatalf("startup view missing header: %q", view)
			}
		})
	}
}
