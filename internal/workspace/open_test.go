package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOpen_Feature_LaunchesLayoutOnExistingFolders is the end-to-end check for
// the no-worktree launch: two real Git repos with a .kesh.yaml recipe are
// launched via Open, and the test asserts that NO worktree is created while the
// assembled Kitty session targets each repo's existing folder with its panes.
func TestOpen_Feature_LaunchesLayoutOnExistingFolders(t *testing.T) {
	root := t.TempDir()
	repoA := filepath.Join(root, "backend-repo")
	repoB := filepath.Join(root, "frontend-repo")
	initGitRepo(t, repoA)
	initGitRepo(t, repoB)

	writeFile(t, filepath.Join(repoA, ".kesh.yaml"), strings.Join([]string{
		"workspaces:",
		"  - name: backend",
		"    repo: .",
		"    panes:",
		"      - commands:",
		"          - nvim",
		"        focus: true",
		"      - commands:",
		"          - pnpm test",
		"        split: horizontal",
		"  - name: frontend",
		"    repo: ../frontend-repo",
		"    panes:",
		"      - commands:",
		"          - nvim",
		"        focus: true",
		"    worktree:",
		"      hooks:",
		"        post_create:",
		"          - false",
		"",
	}, "\n"))

	runner := &featureRunner{}
	err := Open(context.Background(), OpenOptions{
		Cwd:    repoA,
		Mode:   ModeAll,
		Runner: runner,
		Env:    map[string]string{"HOME": root},
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Open must not create any worktree: each repo still lists only itself.
	assertNoExtraWorktree(t, repoA)
	assertNoExtraWorktree(t, repoB)

	if n := len(runner.kittyActions); n != 1 {
		t.Fatalf("kitty goto_session calls = %d, want 1", n)
	}
	action := runner.kittyActions[0]
	if action[1] != "action" || action[2] != "goto_session" {
		t.Fatalf("unexpected kitty action: %#v", action)
	}
	sessionPath := action[len(action)-1]
	sessionBytes, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	session := string(sessionBytes)
	t.Logf("assembled kitty session:\n%s", session)

	// The session cds into the existing repo folders, not into a worktree path.
	for _, want := range []string{
		"--title=backend", "new_tab frontend", "nvim", "pnpm test", "layout splits",
		repoA, repoB,
	} {
		if !strings.Contains(session, want) {
			t.Fatalf("session missing %q:\n%s", want, session)
		}
	}
	// No worktree directory path should appear (worktrees live under a Home
	// root, which Open never provides).
	if strings.Contains(session, "/worktrees/") {
		t.Fatalf("session targets a worktree path:\n%s", session)
	}
}

// assertNoExtraWorktree confirms a repo's worktree list contains exactly one
// entry — the repo's own checkout — proving Open created nothing.
func assertNoExtraWorktree(t *testing.T, repo string) {
	t.Helper()
	out := mustOutput(t, "git", "-C", repo, "worktree", "list")
	lines := strings.Count(strings.TrimSpace(out), "\n") + 1
	if lines != 1 {
		t.Fatalf("repo %s has unexpected worktrees:\n%s", filepath.Base(repo), out)
	}
}

func TestOpenFolderLaunchesNamedPlainSession(t *testing.T) {
	root := t.TempDir()
	folder := filepath.Join(root, "plain")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionFile := filepath.Join(root, "state", "plain.kitty-session")
	runner := &featureRunner{}
	if err := OpenFolder(context.Background(), folder, "plain", sessionFile, map[string]string{"HOME": root}, runner); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(sessionFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"KESH_KITTY_SESSION=plain", folder, "layout splits"} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("plain session missing %q:\n%s", want, content)
		}
	}
}

func TestOpenFoldersUsesConfiguredSubdirectoryOnce(t *testing.T) {
	repoRoot := t.TempDir()
	workspaceRoot := filepath.Join(repoRoot, "projects", "frontier")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	folders, err := openFolders(selection{Workspaces: []worktreeSpec{{
		Name:             "frontier",
		RepoRoot:         repoRoot,
		WorkspaceRoot:    workspaceRoot,
		WorkspaceRelPath: filepath.Join("projects", "frontier"),
	}}})
	if err != nil {
		t.Fatalf("openFolders failed: %v", err)
	}
	if len(folders) != 1 || folders[0].Path != workspaceRoot {
		t.Fatalf("folders = %#v, want %q", folders, workspaceRoot)
	}
}

// TestSessionNameForOpen covers the launch session-name override: a non-empty
// user name wins (sanitized); empty or sanitized-away falls back to the derived
// repo name so the session always has a valid identifier.
func TestWorktreeSessionUsesBranchName(t *testing.T) {
	sel := selection{
		ConfigRepoSlug: "owner/dotfiles",
		Workspaces: []worktreeSpec{
			{Name: "dotfiles"},
			{Name: "kesh"},
		},
	}
	if got := sessionName(sel, "worktree-1"); got != "worktree-1" {
		t.Fatalf("worktree session name = %q, want %q", got, "worktree-1")
	}
}

func TestSessionNameForOpen(t *testing.T) {
	sel := selection{ConfigRepoSlug: "owner/repo"}

	if got := sessionNameForOpen(sel, ""); got != "repo" {
		t.Fatalf("empty requested = %q, want derived %q", got, "repo")
	}
	if got := sessionNameForOpen(sel, "   "); got != "repo" {
		t.Fatalf("blank requested = %q, want derived %q", got, "repo")
	}
	if got := sessionNameForOpen(sel, "my session"); got != "my-session" {
		t.Fatalf("user name not sanitized = %q, want %q", got, "my-session")
	}
	if got := sessionNameForOpen(sel, "feat/work"); got != "feat/work" {
		t.Fatalf("user name with slash = %q, want %q", got, "feat/work")
	}
	// A name made only of unsafe chars sanitizes to nothing -> derived fallback.
	if got := sessionNameForOpen(sel, "!!!"); got != "repo" {
		t.Fatalf("sanitized-away = %q, want derived %q", got, "repo")
	}
}
