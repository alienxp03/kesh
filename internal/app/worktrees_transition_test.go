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

// TestLaunchLayoutOpensRecipeWithoutWorktree drives the `o` flow end-to-end at
// the app layer: pressing `o` on a project opens the launch form (no branch
// field), and Enter dispatches workspace.Open — never workspace.Create — with
// the project folder as Cwd and no branch.
func TestLaunchLayoutOpensRecipeWithoutWorktree(t *testing.T) {
	directory := t.TempDir()
	repository := filepath.Join(directory, "repo")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}

	var captured workspace.OpenOptions
	original := workspaceOpen
	workspaceOpen = func(ctx context.Context, opts workspace.OpenOptions) error {
		captured = opts
		return nil
	}
	t.Cleanup(func() { workspaceOpen = original })

	m := model{
		filter: filterAll,
		entries: []entry{{
			key: repository, name: "repo", path: repository, kind: "project",
		}},
	}
	m.rebuildRows()

	// `o` opens the launch form on the cursor entry.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(model)
	if m.mode != modeWorktreeCreate || !m.launchOnFolder {
		t.Fatalf("o transition: mode=%d launchOnFolder=%t", m.mode, m.launchOnFolder)
	}
	if cmd != nil {
		// beginLaunchLayout schedules a recipe load; drain it so the form is
		// ready. The recipe is irrelevant to the dispatch assertion below.
		_ = cmd()
	}

	// Simulate the recipe arriving (template mode, single workspace recipe).
	updated, _ = m.Update(worktreeRecipeMsg{
		projectPath: repository,
		recipe:      &workspace.Config{WorkspaceMode: "single"},
		recipePath:  filepath.Join(repository, ".kesh.yaml"),
	})
	m = updated.(model)
	if m.worktreeRecipe == nil {
		t.Fatalf("recipe did not load into launch form")
	}

	// Enter must dispatch runLaunchLayout (workspace.Open), not create a worktree.
	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if command == nil || !m.worktreeBusy {
		t.Fatalf("launch transition: command=%v busy=%t", command, m.worktreeBusy)
	}
	message := command()
	if captured.Cwd != repository {
		t.Fatalf("workspace.Open Cwd = %q, want %q", captured.Cwd, repository)
	}
	if captured.Mode != "" && captured.Mode != "single" {
		t.Fatalf("workspace.Open Mode = %q", captured.Mode)
	}

	// Completion clears the form and quits (kesh hands off to Kitty).
	updated, post := m.Update(message)
	m = updated.(model)
	if m.mode != modeNormal || m.worktreeCreateForm != nil {
		t.Fatalf("launch completion: mode=%d form=%#v", m.mode, m.worktreeCreateForm)
	}
	if post == nil {
		t.Fatalf("launch completion should hand off (quit), got nil")
	}
	if _, ok := post().(tea.QuitMsg); !ok {
		t.Fatalf("launch completion should quit, got %T", post())
	}
}

// TestLaunchLayoutWithoutRecipeFallsBackToFolderOpen confirms `o` on a project
// with no .kesh.yaml degrades to opening the folder session directly rather
// than erroring.
func TestLaunchLayoutWithoutRecipeFallsBackToFolderOpen(t *testing.T) {
	directory := t.TempDir()
	repository := filepath.Join(directory, "repo")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}

	m := model{
		filter: filterAll,
		entries: []entry{{
			key: repository, name: "repo", path: repository, kind: "project",
		}},
		kitty: "kitty", zoxide: "zoxide",
	}
	m.rebuildRows()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(model)
	// No recipe arrives: the form stays in launch mode with a nil recipe.
	m.worktreeRecipe = nil

	updated, command := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if command == nil {
		t.Fatal("no command dispatched for recipe-less launch")
	}
	msg := command()
	action, ok := msg.(actionMsg)
	if !ok {
		t.Fatalf("recipe-less launch should dispatch actionMsg, got %T", msg)
	}
	if action.err != nil {
		t.Fatalf("folder-open fallback errored: %v", action.err)
	}
	if m.mode != modeNormal {
		t.Fatalf("form should be cancelled after fallback: mode=%d", m.mode)
	}
}
