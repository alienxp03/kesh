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
		"      - command: nvim",
		"        focus: true",
		"      - command: pnpm test",
		"        split: horizontal",
		"  - name: frontend",
		"    repo: ../frontend-repo",
		"    panes:",
		"      - command: nvim",
		"        focus: true",
		"    hooks:",
		"      post_create:",
		"        - echo setup-done",
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
	count := strings.Count(out, "\n")
	if count != 1 && !strings.HasSuffix(strings.TrimSpace(out), repo) {
		// "worktree list" prints one line per worktree. A single line whose
		// path is the repo itself means no extra worktree was created.
		lines := strings.Count(strings.TrimSpace(out), "\n") + 1
		if lines != 1 {
			t.Fatalf("repo %s has unexpected worktrees:\n%s", filepath.Base(repo), out)
		}
	}
}
