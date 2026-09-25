package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alienxp03/kesh/internal/workspace/run"
)

func TestParseTreeCommand(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		operation string
		branch    string
		from      string
		force     bool
		yes       bool
		session   bool
		window    bool
		json      bool
		wantError bool
	}{
		{name: "new", args: []string{"new", "feature/x"}, operation: "new", branch: "feature/x"},
		{name: "new from", args: []string{"new", "--from", "main", "feature/x", "--json"}, operation: "new", branch: "feature/x", from: "main", json: true},
		{name: "new session", args: []string{"new", "feature/x", "--session", "--json"}, operation: "new", branch: "feature/x", session: true, json: true},
		{name: "new session shortcut", args: []string{"new", "feature/x", "-s", "--json"}, operation: "new", branch: "feature/x", session: true, json: true},
		{name: "new window", args: []string{"new", "feature/x", "--window", "--json"}, operation: "new", branch: "feature/x", window: true, json: true},
		{name: "new window shortcut", args: []string{"new", "feature/x", "-w", "--json"}, operation: "new", branch: "feature/x", window: true, json: true},
		{name: "destroy force yes", args: []string{"destroy", "feature/x", "--force", "-y", "--json"}, operation: "destroy", branch: "feature/x", force: true, yes: true, json: true},
		{name: "merge", args: []string{"merge", "feature/x", "--yes"}, operation: "merge", branch: "feature/x", yes: true},
		{name: "new rejects force", args: []string{"new", "feature/x", "--force"}, wantError: true},
		{name: "new rejects both open modes", args: []string{"new", "feature/x", "--session", "--window"}, wantError: true},
		{name: "destroy requires branch", args: []string{"destroy", "--force"}, wantError: true},
		{name: "unknown command", args: []string{"switch", "feature/x"}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseTreeCommand(test.args)
			if (err != nil) != test.wantError {
				t.Fatalf("parseTreeCommand(%q) error = %v, wantError=%t", test.args, err, test.wantError)
			}
			if err != nil {
				return
			}
			if got.Operation != test.operation || got.Branch != test.branch || got.From != test.from || got.Force != test.force || got.Yes != test.yes || got.Session != test.session || got.Window != test.window || got.JSON != test.json {
				t.Fatalf("parseTreeCommand(%q) = %#v", test.args, got)
			}
		})
	}
}

func TestTreeNewSession(t *testing.T) {
	repo := initTreeTestRepo(t, false)
	t.Setenv("HOME", t.TempDir())
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	kittyCalls := 0
	options := treeCommandOptions{Cwd: repo, Stdout: stdout, Stderr: stderr, Runner: treeTestRunner{kittyCalls: &kittyCalls}}

	if err := runTreeCommandWithOptions([]string{"new", "feature/session", "--session", "--json"}, options); err != nil {
		t.Fatal(err)
	}
	opened := decodeTreeResponse(t, stdout.Bytes())
	if !opened.OK || !opened.Opened || opened.Operation != "new" || opened.Branch != "feature/session" {
		t.Fatalf("open response = %#v", opened)
	}
	if kittyCalls == 0 {
		t.Fatal("--session did not invoke Kitty")
	}
}

func TestTreeNewWindow(t *testing.T) {
	repo := initTreeTestRepo(t, false)
	t.Setenv("HOME", t.TempDir())
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	kittyCalls := 0
	options := treeCommandOptions{Cwd: repo, Stdout: stdout, Stderr: stderr, Runner: treeTestRunner{kittyCalls: &kittyCalls}}

	if err := runTreeCommandWithOptions([]string{"new", "feature/window", "--window", "--json"}, options); err != nil {
		t.Fatal(err)
	}
	opened := decodeTreeResponse(t, stdout.Bytes())
	if !opened.OK || !opened.Opened || opened.Operation != "new" || opened.Branch != "feature/window" {
		t.Fatalf("window response = %#v", opened)
	}
	if kittyCalls == 0 {
		t.Fatal("--window did not invoke Kitty")
	}
}

func TestTreeNewAndDestroyJSON(t *testing.T) {
	repo := initTreeTestRepo(t, false)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	options := treeCommandOptions{Cwd: repo, Stdout: stdout, Stderr: stderr, Runner: treeTestRunner{}}

	if err := runTreeCommandWithOptions([]string{"new", "feature/agent", "--json"}, options); err != nil {
		t.Fatal(err)
	}
	created := decodeTreeResponse(t, stdout.Bytes())
	if !created.OK || created.Operation != "new" || created.Branch != "feature/agent" {
		t.Fatalf("create response = %#v", created)
	}
	if _, err := os.Stat(created.Worktree); err != nil {
		t.Fatalf("created worktree %q: %v", created.Worktree, err)
	}

	stdout.Reset()
	stderr.Reset()
	if err := runTreeCommandWithOptions([]string{"destroy", "feature/agent", "--json", "--yes"}, options); err != nil {
		t.Fatal(err)
	}
	destroyed := decodeTreeResponse(t, stdout.Bytes())
	if !destroyed.OK || !destroyed.Destroyed || destroyed.Operation != "destroy" {
		t.Fatalf("destroy response = %#v", destroyed)
	}
	if _, err := os.Stat(created.Worktree); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %q, err=%v", created.Worktree, err)
	}
	if branchExists(t, repo, "feature/agent") {
		t.Fatal("destroy left the local branch behind")
	}
}

func TestTreeNewJSONKeepsHookOutputOffStdout(t *testing.T) {
	repo := initTreeTestRepo(t, true)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	options := treeCommandOptions{Cwd: repo, Stdout: stdout, Stderr: stderr, Runner: treeTestRunner{}}
	if err := runTreeCommandWithOptions([]string{"new", "feature/hook", "--json"}, options); err != nil {
		t.Fatal(err)
	}
	response := decodeTreeResponse(t, stdout.Bytes())
	if !response.OK {
		t.Fatalf("create response = %#v", response)
	}
	if !strings.Contains(stderr.String(), "hook-output") {
		t.Fatalf("hook output did not reach stderr: %q", stderr.String())
	}
}

func TestTreeMergeJSONMergesThenDestroys(t *testing.T) {
	repo := initTreeTestRepo(t, false)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	options := treeCommandOptions{Cwd: repo, Stdout: stdout, Stderr: stderr, Runner: treeTestRunner{}}
	if err := runTreeCommandWithOptions([]string{"new", "feature/merge", "--json"}, options); err != nil {
		t.Fatal(err)
	}
	created := decodeTreeResponse(t, stdout.Bytes())
	appendTreeTestFile(t, created.Worktree, "merged\n")

	stdout.Reset()
	stderr.Reset()
	if err := runTreeCommandWithOptions([]string{"merge", "feature/merge", "--json", "--yes"}, options); err != nil {
		t.Fatal(err)
	}
	merged := decodeTreeResponse(t, stdout.Bytes())
	if !merged.OK || !merged.Merged || !merged.Destroyed || merged.Target == "" {
		t.Fatalf("merge response = %#v", merged)
	}
	if _, err := os.Stat(created.Worktree); !os.IsNotExist(err) {
		t.Fatalf("merged worktree still exists: %q, err=%v", created.Worktree, err)
	}
	if branchExists(t, repo, "feature/merge") {
		t.Fatal("merge left the local branch behind")
	}
	if output := mustTreeOutput(t, repo, "show", "HEAD:README.md"); !strings.Contains(output, "merged") {
		t.Fatalf("merged content missing from primary repository: %q", output)
	}
}

func initTreeTestRepo(t *testing.T, withHook bool) string {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	mustTreeRun(t, repo, "init", "-q")
	mustTreeRun(t, repo, "config", "user.name", "tree-test")
	mustTreeRun(t, repo, "config", "user.email", "tree-test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf("worktree:\n  dir: %s\nworkspaces:\n  - name: app\n    repo: .\n", filepath.Join(root, "worktrees"))
	if withHook {
		config += "    worktree:\n      hooks:\n        post_create:\n          - printf hook-output\n"
	}
	if err := os.WriteFile(filepath.Join(repo, ".kesh.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	mustTreeRun(t, repo, "add", "README.md", ".kesh.yaml")
	mustTreeRun(t, repo, "commit", "-qm", "initial")
	return repo
}

func appendTreeTestFile(t *testing.T, worktree, content string) {
	t.Helper()
	path := filepath.Join(worktree, "README.md")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	mustTreeRun(t, worktree, "add", "README.md")
	mustTreeRun(t, worktree, "commit", "-qm", "feature change")
}

func decodeTreeResponse(t *testing.T, content []byte) treeResponse {
	t.Helper()
	var response treeResponse
	if err := json.Unmarshal(content, &response); err != nil {
		t.Fatalf("invalid JSON %q: %v", content, err)
	}
	return response
}

func branchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	command := exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return command.Run() == nil
}

func mustTreeRun(t *testing.T, cwd string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func mustTreeOutput(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

type treeTestRunner struct {
	kittyCalls *int
}

func (r treeTestRunner) Run(ctx context.Context, command string, args []string, options run.Options) run.Result {
	if command == "zoxide" {
		return run.Result{ExitCode: 0, Stdout: "zoxide 0.0.0-test"}
	}
	if command == "kitty" {
		if len(args) > 0 && args[0] == "@" {
			if r.kittyCalls != nil {
				(*r.kittyCalls)++
			}
			for _, arg := range args[1:] {
				if arg == "ls" {
					return run.Result{ExitCode: 0, Stdout: "[]"}
				}
			}
			return run.Result{ExitCode: 0}
		}
		return run.Result{ExitCode: 0, Stdout: "kitty 0.0.0-test"}
	}
	return run.DefaultRunner{}.Run(ctx, command, args, options)
}
