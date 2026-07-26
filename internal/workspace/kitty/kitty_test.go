package kitty

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alienxp03/kesh/internal/workspace/layout"
	"github.com/alienxp03/kesh/internal/workspace/run"
)

func TestRenderSessionEscapesCommandsAndMapsSplits(t *testing.T) {
	session, err := RenderSession("repo/feature's", []layout.Window{
		{
			Name:         "back end's",
			WorktreePath: "/tmp/work tree's",
			Commands: []layout.PaneCommand{
				{Commands: []string{"nvim"}},
				{Commands: []string{"pnpm install", "pnpm run dev"}, Split: "horizontal", Percentage: 35, Focus: true},
				{Commands: []string{"echo 'ok'"}, Split: "vertical"},
			},
		},
		{Name: "frontend", WorktreePath: "/tmp/frontend"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"launch '--title=back end'\\''s'", "layout splits", "cd '/tmp/work tree'\\''s'",
		"new_tab frontend", "--location=vsplit", "--bias=35", "--location=hsplit", "pnpm install && pnpm run dev",
		"KESH_KITTY_SESSION=repo/feature", "--var", "kesh_focus=yes",
		"focus_matching_window var:kesh_focus=yes", "focus_tab 0", "focus_os_window",
		".kesh.env", "exec \"${SHELL:-/bin/sh}\" -i",
	} {
		if !strings.Contains(session, want) {
			t.Fatalf("session missing %q:\n%s", want, session)
		}
	}
	firstTab := session[:strings.Index(session, "new_tab frontend")]
	if strings.Count(session, "'kesh_focus=yes'") != 2 || !strings.Contains(firstTab, "'kesh_focus=yes'") {
		t.Fatalf("each tab must mark exactly one focus target:\n%s", session)
	}
	focusLaunch := firstTab[strings.LastIndex(firstTab[:strings.Index(firstTab, "kesh_focus=yes")], "launch "):]
	if !strings.Contains(focusLaunch, "pnpm install && pnpm run dev") {
		t.Fatalf("explicitly focused pane was not marked:\n%s", firstTab)
	}
	lastLaunch := strings.LastIndex(firstTab, "launch ")
	if focus := strings.Index(firstTab, "focus_matching_window"); focus < lastLaunch {
		t.Fatalf("pane focus must be deferred until after every launch:\n%s", session)
	}
	if strings.HasPrefix(session, "new_tab") {
		t.Fatalf("first workspace must use Kitty's implicit first tab:\n%s", session)
	}
	if strings.Contains(session, "new_tab 'frontend'") {
		t.Fatalf("Kitty would display shell quotes in the tab title:\n%s", session)
	}
}

func TestRenderSessionAllowsShellOnlySplit(t *testing.T) {
	session, err := RenderSession("repo/topic", []layout.Window{{
		Name: "editor", WorktreePath: "/tmp/repo",
		Commands: []layout.PaneCommand{
			{Commands: []string{"nvim"}},
			{Split: "horizontal"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(session, "--location=vsplit") {
		t.Fatalf("session did not create the split:\n%s", session)
	}
	if strings.Count(session, "exec \"${SHELL:-/bin/sh}\" -fc") != 1 {
		t.Fatalf("shell-only pane should not receive a command wrapper:\n%s", session)
	}
}

func TestRenderSessionUsesTallForMainPaneAndRightStack(t *testing.T) {
	session, err := RenderSession("repo/topic", []layout.Window{{
		Name: "editor", WorktreePath: "/tmp/repo",
		Commands: []layout.PaneCommand{
			{Commands: []string{"nvim"}},
			{Commands: []string{"echo build"}, Split: "horizontal"},
			{Commands: []string{"agent"}, Split: "vertical"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(session, "layout tall") || strings.Contains(session, "layout splits") {
		t.Fatalf("session should use stable tall layout:\n%s", session)
	}
	if strings.Contains(session, "--location=") {
		t.Fatalf("tall layout must let Kitty arrange panes without split locations:\n%s", session)
	}
}

func TestRenderSessionRejectsLineBreaks(t *testing.T) {
	for _, window := range []layout.Window{
		{Name: "bad\nname", WorktreePath: "/tmp/app"},
		{Name: "app", WorktreePath: "/tmp/bad\npath"},
		{Name: "app", WorktreePath: "/tmp/app", Commands: []layout.PaneCommand{{Commands: []string{"echo first\necho second"}}}},
	} {
		if _, err := RenderSession("repo/feature", []layout.Window{window}); err == nil || !strings.Contains(err.Error(), "line break") {
			t.Fatalf("window=%#v error=%v", window, err)
		}
	}
}

func TestWriteSessionFileIsStableAndPrivate(t *testing.T) {
	cache := t.TempDir()
	first, err := WriteSessionFile(cache, "repo/feature", "first\n")
	if err != nil {
		t.Fatal(err)
	}
	second, err := WriteSessionFile(cache, "repo/feature", "second\n")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || filepath.Ext(first) != ".kitty-session" || !strings.HasPrefix(filepath.Base(first), "repo-feature-") {
		t.Fatalf("paths = %q, %q", first, second)
	}
	content, err := os.ReadFile(second)
	if err != nil || string(content) != "second\n" {
		t.Fatalf("content = %q err=%v", content, err)
	}
	info, err := os.Stat(second)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v err=%v", info.Mode(), err)
	}
	if err := RemoveSessionFile(cache, "repo/feature"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatalf("session file still exists: %v", err)
	}
}

func TestOpenLayoutUsesGotoSessionAndListenSocket(t *testing.T) {
	runner := &kittyRunner{lsOutput: "[]"}
	status, err := OpenLayout(context.Background(), layout.OpenOptions{
		SessionName: "repo/feature",
		Windows:     []layout.Window{{Name: "app", WorktreePath: "/tmp/app"}},
		Env:         map[string]string{"KITTY_LISTEN_ON": "unix:/tmp/kitty.sock"},
		CacheDir:    t.TempDir(),
		Runner:      run.RunnerFunc(runner.run),
	})
	if err != nil || status != 0 {
		t.Fatalf("status=%d err=%v", status, err)
	}
	assertKittyCommands(t, runner.calls, []string{"--version", "@ --to unix:/tmp/kitty.sock ls", "@ --to unix:/tmp/kitty.sock action goto_session"})
	last := runner.calls[len(runner.calls)-1]
	if filepath.Ext(last.args[len(last.args)-1]) != ".kitty-session" {
		t.Fatalf("goto args = %#v", last.args)
	}
}

func TestOpenLayoutFocusesExistingGeneratedSession(t *testing.T) {
	runner := &kittyRunner{lsOutput: `[{"tabs":[{"id":7,"windows":[{"id":42,"env":{"KESH_KITTY_SESSION":"repo/feature"}}]}]}]`}
	status, err := OpenLayout(context.Background(), layout.OpenOptions{
		SessionName: "repo/feature", Env: map[string]string{"KITTY_WINDOW_ID": "1"}, Runner: run.RunnerFunc(runner.run),
	})
	if err != nil || status != 0 {
		t.Fatalf("status=%d err=%v", status, err)
	}
	assertKittyCommands(t, runner.calls, []string{"--version", "@ ls", "@ focus-window --match id:42"})
}

func TestKillLayoutClosesEveryMatchingTabAndToleratesNoMatch(t *testing.T) {
	cache := t.TempDir()
	sessionPath, err := WriteSessionFile(cache, "repo/feature", "layout splits\n")
	if err != nil {
		t.Fatal(err)
	}
	runner := &kittyRunner{lsOutput: `[{"tabs":[{"id":8,"windows":[{"id":1,"env":{"KESH_KITTY_SESSION":"repo/feature"}},{"id":2,"env":{"KESH_KITTY_SESSION":"repo/feature"}}]},{"id":9,"windows":[{"id":3,"env":{"KESH_KITTY_SESSION":"other"}}]},{"id":10,"windows":[{"id":4,"env":{"KESH_KITTY_SESSION":"repo/feature"}}]}]}]`}
	err = KillLayout(context.Background(), layout.KillOptions{SessionName: "repo/feature", CacheDir: cache, Runner: run.RunnerFunc(runner.run)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("session file still exists after close: %v", err)
	}
	assertKittyCommands(t, runner.calls, []string{"--version", "@ ls", "@ close-tab --match id:8", "@ close-tab --match id:10"})

	noServer := &kittyRunner{lsExit: 1, lsStderr: "Failed to connect to Kitty"}
	if err := KillLayout(context.Background(), layout.KillOptions{SessionName: "repo/feature", Runner: run.RunnerFunc(noServer.run)}); err != nil {
		t.Fatalf("no server: %v", err)
	}

	unauthorized := &kittyRunner{lsExit: 1, lsStderr: "remote control permission denied"}
	if err := KillLayout(context.Background(), layout.KillOptions{SessionName: "repo/feature", Runner: run.RunnerFunc(unauthorized.run)}); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("authorization error = %v", err)
	}

	unavailableCache := t.TempDir()
	unavailablePath, err := WriteSessionFile(unavailableCache, "repo/unavailable", "layout splits\n")
	if err != nil {
		t.Fatal(err)
	}
	unavailable := &kittyRunner{versionExit: 1}
	if err := KillLayout(context.Background(), layout.KillOptions{SessionName: "repo/unavailable", CacheDir: unavailableCache, Runner: run.RunnerFunc(unavailable.run)}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unavailablePath); !os.IsNotExist(err) {
		t.Fatalf("session file remains when Kitty is unavailable: %v", err)
	}
}

func TestAvailableUsesKittyVersion(t *testing.T) {
	runner := &kittyRunner{}
	if err := Available(context.Background(), run.RunnerFunc(runner.run)); err != nil {
		t.Fatal(err)
	}
	assertKittyCommands(t, runner.calls, []string{"--version"})

	unavailable := &kittyRunner{versionExit: 1}
	if err := Available(context.Background(), run.RunnerFunc(unavailable.run)); err == nil || !strings.Contains(err.Error(), "kitty --version failed") {
		t.Fatalf("availability error = %v", err)
	}
}

func TestCheckRequiresWorkingRemoteControl(t *testing.T) {
	working := &kittyRunner{lsOutput: "[]"}
	if err := Check(context.Background(), map[string]string{"KITTY_WINDOW_ID": "1"}, run.RunnerFunc(working.run)); err != nil {
		t.Fatal(err)
	}

	unreachable := &kittyRunner{lsExit: 1, lsStderr: "Failed to connect to Kitty"}
	if err := Check(context.Background(), nil, run.RunnerFunc(unreachable.run)); err == nil || !strings.Contains(err.Error(), "remote control is unavailable") {
		t.Fatalf("check error = %v", err)
	}
}

func TestOpenLayoutRequiresKittyContextOrListenSocket(t *testing.T) {
	runner := &kittyRunner{lsExit: 1, lsStderr: "Failed to connect to Kitty"}
	_, err := OpenLayout(context.Background(), layout.OpenOptions{
		SessionName: "repo/feature",
		Windows:     []layout.Window{{Name: "app", WorktreePath: "/tmp/app"}},
		Runner:      run.RunnerFunc(runner.run),
	})
	if err == nil || !strings.Contains(err.Error(), "running inside Kitty or KITTY_LISTEN_ON") {
		t.Fatalf("error = %v", err)
	}
}

type kittyCall struct {
	args    []string
	options run.Options
}

type kittyRunner struct {
	calls       []kittyCall
	lsOutput    string
	lsStderr    string
	lsExit      int
	versionExit int
}

func (runner *kittyRunner) run(_ context.Context, command string, args []string, options run.Options) run.Result {
	if command != "kitty" {
		return run.Result{ExitCode: 1, Stderr: "unexpected command " + command}
	}
	runner.calls = append(runner.calls, kittyCall{args: append([]string(nil), args...), options: options})
	if args[0] == "--version" {
		return run.Result{ExitCode: runner.versionExit, Stderr: "not found"}
	}
	if args[len(args)-1] == "ls" {
		return run.Result{ExitCode: runner.lsExit, Stdout: runner.lsOutput, Stderr: runner.lsStderr}
	}
	return run.Result{}
}

func assertKittyCommands(t *testing.T, calls []kittyCall, prefixes []string) {
	t.Helper()
	if len(calls) != len(prefixes) {
		t.Fatalf("calls = %#v, want %d", calls, len(prefixes))
	}
	for index, prefix := range prefixes {
		got := strings.Join(calls[index].args, " ")
		if !strings.HasPrefix(got, prefix) {
			t.Fatalf("call[%d] = %q, want prefix %q", index, got, prefix)
		}
	}
}
