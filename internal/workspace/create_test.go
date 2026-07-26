package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alienxp03/kesh/internal/workspace/run"
)

// featureRunner executes git and other commands for real but intercepts every
// kitty invocation so the test never touches the user's running Kitty. It
// records the remote-control calls so the assembled session can be asserted.
type featureRunner struct {
	kittyActions [][]string
}

func (r *featureRunner) Run(ctx context.Context, command string, args []string, options run.Options) run.Result {
	if command == "kitty" {
		// Remote-control calls begin with "@"; `kitty --version` does not.
		if len(args) > 0 && args[0] == "@" {
			if len(args) > 1 && args[1] == "action" {
				r.kittyActions = append(r.kittyActions, append([]string(nil), args...))
			}
			if len(args) > 1 && args[1] == "ls" {
				return run.Result{ExitCode: 0, Stdout: "[]"} // no existing session windows
			}
			return run.Result{ExitCode: 0}
		}
		return run.Result{ExitCode: 0, Stdout: "kitty 0.0.0-feature"} // --version
	}
	return run.DefaultRunner{}.Run(ctx, command, args, options)
}

// TestCreate_Feature_RealGitAndKittySession is an end-to-end feature test: it
// stands up two real Git repositories with a .kesh.yaml recipe, runs Create
// with a real runner (so `git worktree add` executes on disk), and asserts that
// the worktrees are created and the Kitty session file is assembled correctly.
func TestCreate_Feature_RealGitAndKittySession(t *testing.T) {
	root := t.TempDir()
	repoA := filepath.Join(root, "backend-repo")
	repoB := filepath.Join(root, "frontend-repo")
	initGitRepo(t, repoA)
	initGitRepo(t, repoB)

	// Recipe: two workspaces pointing at the two repos, with pane layouts.
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
		"",
	}, "\n"))

	runner := &featureRunner{}
	worktreeHome := filepath.Join(root, "worktrees")
	err := Create(context.Background(), CreateOptions{
		Cwd:    repoA,
		Branch: "feat/integration",
		Home:   worktreeHome,
		Mode:   ModeAll,
		Runner: runner,
		Env:    map[string]string{"HOME": root},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Both repositories gained a real worktree on the new branch.
	assertWorktreeBranch(t, repoA, "feat/integration")
	assertWorktreeBranch(t, repoB, "feat/integration")

	// Kitty was asked to open exactly one session, and its file renders both
	// workspaces as tabs with their pane commands.
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
	// The first workspace is the implicit initial tab (no `new_tab` line); the
	// second gets an explicit `new_tab frontend`. Both contribute their panes.
	for _, want := range []string{"--title=backend", "new_tab frontend", "nvim", "pnpm test", "layout splits"} {
		if !strings.Contains(session, want) {
			t.Fatalf("session missing %q:\n%s", want, session)
		}
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "-C", dir, "init")
	mustRun(t, "-C", dir, "config", "user.name", "feature-tester")
	mustRun(t, "-C", dir, "config", "user.email", "tester@example.com")
	writeFile(t, filepath.Join(dir, "README"), "initial\n")
	mustRun(t, "-C", dir, "add", "README")
	mustRun(t, "-C", dir, "commit", "-m", "initial")
}

func assertWorktreeBranch(t *testing.T, repo, branch string) {
	t.Helper()
	out := mustOutput(t, "git", "-C", repo, "worktree", "list", "--porcelain")
	if !strings.Contains(out, "branch refs/heads/"+branch) && !strings.Contains(out, "branch "+branch) {
		t.Fatalf("repo %s has no worktree on %q:\n%s", filepath.Base(repo), branch, out)
	}
}

func mustRun(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func mustOutput(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
