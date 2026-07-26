package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alienxp03/kesh/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
)

func TestWorktreeSearchAndReturnToOriginatingFilter(t *testing.T) {
	m := model{
		filter:                   filterWorktrees,
		previousFilter:           filterSaved,
		worktreeFilterEntryIndex: 0,
		entries: []entry{{
			name:            "repo",
			path:            "/repo",
			kind:            "project",
			worktreesLoaded: true,
			worktrees: []worktreeItem{
				{path: "/trees/main", branch: "main", current: true},
				{path: "/trees/feature", branch: "feat/search"},
			},
		}},
	}
	m.rebuildWorktreeRows()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("search")})
	m = updated.(model)
	if m.mode != modeSearch || len(m.worktreeFilterRows) != 1 || m.worktreeFilterRows[0].worktree.path != "/trees/feature" {
		t.Fatalf("worktree search result: mode=%d rows=%#v", m.mode, m.worktreeFilterRows)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.mode != modeNormal || m.filter != filterWorktrees {
		t.Fatalf("accepting search should remain in Worktrees: mode=%d filter=%d", m.mode, m.filter)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.filter != filterSaved || m.query != "" || m.worktreeFilterEntryIndex != -1 {
		t.Fatalf("return context = filter:%d query:%q entry:%d", m.filter, m.query, m.worktreeFilterEntryIndex)
	}
}

func TestWorktreeCreateRunsRecipeAndRefreshesPrimarySurface(t *testing.T) {
	directory := t.TempDir()
	repository := filepath.Join(directory, "repo")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}

	var captured workspace.CreateOptions
	original := worktreeCreate
	worktreeCreate = func(ctx context.Context, opts workspace.CreateOptions) error {
		captured = opts
		return nil
	}
	t.Cleanup(func() { worktreeCreate = original })

	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries: []entry{{
			key: repository, name: "repo", path: repository, kind: "project",
			worktreesLoaded: true,
		}},
	}
	m.activateMode(modeWorktreeCreate)
	m.worktreeBranch = "feat/create"
	m.worktreeRecipe = &workspace.Config{WorkspaceMode: "single"}
	m.worktreeRecipePath = filepath.Join(repository, ".kesh.yaml")
	m.worktreeRecipeMode = "single"

	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if command == nil || !m.worktreeBusy {
		t.Fatalf("create transition: command=%v busy=%t", command, m.worktreeBusy)
	}
	message := command()
	if captured.Cwd != repository || captured.Branch != "feat/create" || captured.Mode != "single" {
		t.Fatalf("workspace.Create opts = %#v", captured)
	}

	updated, refresh := m.Update(message)
	m = updated.(model)
	if m.mode != modeNormal || m.filter != filterWorktrees || m.worktreeCreateForm != nil || refresh == nil {
		t.Fatalf("create completion: mode=%d filter=%d form=%#v refresh=%v", m.mode, m.filter, m.worktreeCreateForm, refresh)
	}
}

func TestLateWorktreeRecipeResultIsIgnoredAfterCancel(t *testing.T) {
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries:                  []entry{{key: "/repo", path: "/repo", kind: "project"}},
	}
	m.activateMode(modeWorktreeCreate)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	updated, command := m.Update(worktreeRecipeMsg{
		projectPath:  "/repo",
		recipe:       &workspace.Config{WorkspaceMode: "all"},
		recipePath:   "/repo/.kesh.yaml",
		repositories: map[string]repoIdentity{"/repo": {owner: "owner", repo: "repo"}},
	})
	m = updated.(model)
	if command != nil || m.mode != modeNormal || m.worktreeCreateForm != nil {
		t.Fatalf("late recipe resurrected cancelled mode: mode=%d form=%#v command=%v", m.mode, m.worktreeCreateForm, command)
	}
	if strings.Contains(m.View(), ".kesh.yaml") {
		t.Fatal("late recipe leaked into the normal view")
	}
}
