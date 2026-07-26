package app

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	kittyx "github.com/alienxp03/kesh/internal/kitty"
	"github.com/alienxp03/kesh/internal/system"
	"github.com/alienxp03/kesh/internal/workspace"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"gopkg.in/yaml.v3"
)

func run(name string, args ...string) error {
	if name == "" {
		return fmt.Errorf("required command was not found")
	}
	output, err := system.Command(name, args...).CombinedOutput()
	if err != nil && len(output) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return err
}

type countingRunner struct {
	calls int
}

func (r *countingRunner) Output(system.Spec) ([]byte, error) {
	r.calls++
	return nil, fmt.Errorf("unexpected process execution")
}

func (r *countingRunner) CombinedOutput(system.Spec) ([]byte, error) {
	r.calls++
	return nil, fmt.Errorf("unexpected process execution")
}

func TestUpdateSchedulesIOWithoutExecutingItSynchronously(t *testing.T) {
	runner := &countingRunner{}
	restore := system.SetRunner(runner)
	t.Cleanup(restore)

	worktreeModel := model{
		filter: filterWorktrees, worktreeFilterEntryIndex: 0,
		entries: []entry{{
			key: "/repo", path: "/repo", kind: "project",
			worktrees: []worktreeItem{{path: "/tree", branch: "feature"}}, worktreesLoaded: true,
		}},
		worktreeFilterRows: []worktreeFilterRow{{worktree: worktreeItem{path: "/tree", branch: "feature"}}},
		rows:               []row{{entryIndex: 0, section: "wt-filter"}},
	}
	if _, command := worktreeModel.Update(tea.KeyMsg{Type: tea.KeyEnter}); command == nil {
		t.Fatal("worktree open command was not scheduled")
	}
	if runner.calls != 0 {
		t.Fatalf("worktree enter executed %d process(es) during Update", runner.calls)
	}

	destroyModel := model{
		entries: []entry{{key: "/repo", path: "/repo", kind: "project"}},
		rows:    []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1}},
	}
	if _, command := destroyModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}}); command == nil {
		t.Fatal("destroy-plan command was not scheduled")
	}
	if runner.calls != 0 {
		t.Fatalf("destroy planning executed %d process(es) during Update", runner.calls)
	}

	pinModel := model{
		modeState: modeState{mode: modePin, pinMode: &pinMode{pinEntry: 0}},
		entries:   []entry{{key: "/repo", path: "/repo", name: "repo", kind: "project"}},
		pins:      pinStore{},
	}
	if _, command := pinModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}}); command == nil {
		t.Fatal("pin persistence command was not scheduled")
	}
	if runner.calls != 0 {
		t.Fatalf("pinning executed %d process(es) during Update", runner.calls)
	}

	formModel := model{
		modeState: modeState{
			mode:               modeWorktreeCreate,
			worktreeCreateForm: &worktreeCreateForm{worktreeRepositories: map[string]repoIdentity{}},
		},
		filter: filterWorktrees, worktreeFilterEntryIndex: 0, worktreeRoot: "/worktrees",
		entries: []entry{{key: "/repo", path: "/repo", kind: "project"}},
	}
	if _, command := formModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("feature")}); command != nil {
		t.Fatal("editing a worktree form unexpectedly scheduled a command")
	}
	if runner.calls != 0 {
		t.Fatalf("worktree editing executed %d process(es) during Update", runner.calls)
	}

	refreshModel := model{entries: []entry{{key: "/repo", path: "/repo", kind: "project"}}}
	if _, command := refreshModel.Update(worktreeListMsg{dir: "/repo", worktrees: []worktreeItem{{path: "/repo"}}}); command == nil {
		t.Fatal("PR cache lookup command was not scheduled")
	}
	if runner.calls != 0 {
		t.Fatalf("PR refresh executed %d process(es) during Update", runner.calls)
	}
}

func TestEveryModeCancelsToCleanNormalState(t *testing.T) {
	modes := []modeKind{
		modeSearch,
		modeRename,
		modeCreateSession,
		modeClone,
		modeCheckoutPR,
		modePin,
		modeSaveConfirm,
		modeCloseConfirm,
		modeWorktreeCreate,
	}
	for _, kind := range modes {
		t.Run(strconv.Itoa(int(kind)), func(t *testing.T) {
			m := model{}
			m.activateMode(kind)
			payloads := 0
			for _, present := range []bool{
				m.searchMode != nil,
				m.renameForm != nil,
				m.createSessionForm != nil,
				m.cloneForm != nil,
				m.checkoutForm != nil,
				m.pinMode != nil,
				m.saveConfirmation != nil,
				m.closeConfirmation != nil,
				m.worktreeCreateForm != nil,
			} {
				if present {
					payloads++
				}
			}
			if payloads != 1 {
				t.Fatalf("mode %d has %d active payloads", kind, payloads)
			}

			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
			got := updated.(model)
			if !reflect.DeepEqual(got.modeState, modeState{}) {
				t.Fatalf("mode %d retained payload after cancel: %#v", kind, got.modeState)
			}
		})
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args           []string
		wantFilter     int
		wantSlot       string
		wantPinCommand string
		wantError      bool
	}{
		{wantFilter: filterAll},
		{args: []string{"agents"}, wantFilter: filterAgents},
		{args: []string{"open"}, wantFilter: filterOpen},
		{args: []string{"projects"}, wantFilter: filterProjects},
		{args: []string{"ssh"}, wantFilter: filterSSH},
		{args: []string{"saved"}, wantFilter: filterSaved},
		{args: []string{"begin-run"}, wantFilter: filterAll, wantPinCommand: "begin-run"},
		{args: []string{"clear-pins"}, wantFilter: filterAll, wantPinCommand: "clear-pins"},
		{args: []string{"clear-pins", "--on-quit"}, wantFilter: filterAll, wantPinCommand: "end-run"},
		{args: []string{"switch", "4"}, wantFilter: filterAll, wantSlot: "4"},
		{args: []string{"switch", "10"}, wantError: true},
		{args: []string{"unknown"}, wantError: true},
	}
	for _, test := range tests {
		filter, slot, pinCommand, err := parseArgs(test.args)
		if (err != nil) != test.wantError {
			t.Fatalf("parseArgs(%q) error = %v, wantError %v", test.args, err, test.wantError)
		}
		if err == nil && (filter != test.wantFilter || slot != test.wantSlot || pinCommand != test.wantPinCommand) {
			t.Errorf("parseArgs(%q) = (%d, %q, %q), want (%d, %q, %q)", test.args, filter, slot, pinCommand, test.wantFilter, test.wantSlot, test.wantPinCommand)
		}
	}
}

func TestRelativeLastActive(t *testing.T) {
	const reference = 1_000_000.0
	for _, test := range []struct {
		name    string
		focused float64
		want    string
	}{
		{name: "unknown", want: "unknown"},
		{name: "just now", focused: reference - 30, want: "just now"},
		{name: "minutes", focused: reference - 60, want: "1m ago"},
		{name: "hours", focused: reference - 2*60*60, want: "2h ago"},
		{name: "days", focused: reference - 3*24*60*60, want: "3d ago"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := relativeLastActive(test.focused, reference); got != test.want {
				t.Fatalf("relativeLastActive() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRebuildRowsPrioritizesPinnedEntriesBySlot(t *testing.T) {
	m := model{entries: []entry{
		{name: "unpinned-first"},
		{name: "slot-three", pin: "3"},
		{name: "slot-zero", pin: "0"},
		{name: "unpinned-last"},
		{name: "slot-one", pin: "1"},
	}}
	m.rebuildRows()
	var got []string
	for _, row := range m.rows {
		if row.tabIndex < 0 && row.section == "" {
			got = append(got, m.entries[row.entryIndex].name)
		}
	}
	want := []string{"slot-zero", "slot-one", "slot-three", "unpinned-first", "unpinned-last"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("row order = %#v, want %#v", got, want)
	}
}

func TestMergedWorktreeItemsOnlyReturnsMergedNonCurrent(t *testing.T) {
	worktrees := []worktreeItem{
		{path: "/repos/main", branch: "main"},
		{path: "/worktrees/merged-one", branch: "merged-one"},
		{path: "/worktrees/merged-two", branch: "merged-two"},
		{path: "/worktrees/open", branch: "still-open"},
		{path: "/worktrees/detached", branch: "(detached)"},
		{path: "/worktrees/pr-merged", branch: "pr-merged", head: "merged-head"},
		{path: "/worktrees/pr-reused", branch: "pr-reused", head: "new-unmerged-head"},
	}
	pullRequestHeads := map[string]map[string]bool{
		"pr-merged": {"merged-head": true},
		"pr-reused": {"old-merged-head": true},
	}

	got := mergedWorktreeItems(worktrees, "main\nmerged-one\nmerged-two\n", "main", pullRequestHeads)
	want := []worktreeItem{worktrees[1], worktrees[2], worktrees[5]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged worktrees = %#v, want %#v", got, want)
	}

	// Running from a linked worktree must still protect both that current
	// worktree and the repository's primary working tree.
	got = mergedWorktreeItems(worktrees, "main\nmerged-one\nmerged-two\n", "merged-one", pullRequestHeads)
	want = []worktreeItem{worktrees[2], worktrees[5]}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged worktrees from linked worktree = %#v, want %#v", got, want)
	}
}

func TestOverlayPopupPreservesBackgroundOutsidePopup(t *testing.T) {
	lines := []string{
		"abcdefghijklmnopqrst",
		"abcdefghijklmnopqrst",
		"abcdefghijklmnopqrst",
		"abcdefghijklmnopqrst",
		"abcdefghijklmnopqrst",
	}
	got := overlayPopup(lines, "POP", 20)
	if got[3] != "abcdefghPOPlmnopqrst" {
		t.Fatalf("overlay line = %q", got[3])
	}
	if got[2] != "abcdefghijklmnopqrst" || got[4] != "abcdefghijklmnopqrst" {
		t.Fatalf("overlay changed lines outside popup: %#v", got)
	}
}

func TestFindMergedWorktreesShowsLoadingState(t *testing.T) {
	m := model{
		entries: []entry{{name: "repo", kind: "project", path: "/repos/repo"}},
		rows:    []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1}},
	}
	if cmd := m.findMergedWorktrees(); cmd == nil {
		t.Fatal("merged worktree query was not started")
	}
	if !m.mergedWorktreeBusy {
		t.Fatal("merged worktree loading state was not enabled")
	}
	if popup := ansi.Strip(m.popupView(100)); !strings.Contains(popup, "Checking merged worktrees") {
		t.Fatalf("loading popup = %q", popup)
	}

	updatedModel, _ := m.Update(mergedWorktreeListMsg{err: fmt.Errorf("query failed")})
	if updatedModel.(model).mergedWorktreeBusy {
		t.Fatal("merged worktree loading state was not cleared")
	}
}

func TestDestroyPromptListsApplicableLayers(t *testing.T) {
	got := destroyPrompt(destroyPlan{
		entryName: "repo", closeSession: true, tabCount: 3,
		worktreePath: "/home/wt/repo", branch: "feat/x", saved: true,
	})
	for _, want := range []string{`Destroy "repo"?`, "Close kitty session (3 tabs)", "Remove worktree", "Delete branch  feat/x", "Delete saved record"} {
		if !strings.Contains(got, want) {
			t.Fatalf("destroy prompt missing %q:\n%s", want, got)
		}
	}
	// A plan with only some layers omits the rest.
	minimal := destroyPrompt(destroyPlan{entryName: "drafts", saved: true})
	for _, absent := range []string{"worktree", "branch", "session"} {
		if strings.Contains(minimal, absent) {
			t.Fatalf("minimal destroy prompt should omit inapplicable layers:\n%s", minimal)
		}
	}
}

func TestDetectDestroyPlanSkipsNonWorktreeFolders(t *testing.T) {
	plain := t.TempDir() // no .git at all
	plan := detectDestroyPlan(entry{name: "plain", kind: "project", path: plain, saved: true, open: true, tabs: []tabItem{{}}})
	if plan.worktreePath != "" || plan.branch != "" {
		t.Fatalf("plain dir should not be destroyed as a worktree: %#v", plan)
	}
	if !plan.closeSession || !plan.saved {
		t.Fatalf("closeSession/saved should pass through: %#v", plan)
	}

	mainRepo := t.TempDir() // .git is a directory → main checkout, not a linked worktree
	if err := os.Mkdir(filepath.Join(mainRepo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan = detectDestroyPlan(entry{name: "main", kind: "project", path: mainRepo})
	if plan.worktreePath != "" || plan.branch != "" {
		t.Fatalf("main repo should not be destroyed as a worktree: %#v", plan)
	}

	// Composed workspaces never remove folders/branches, only close/release.
	plan = detectDestroyPlan(entry{name: "ws", kind: "workspace", path: plain, saved: true})
	if plan.worktreePath != "" || plan.branch != "" {
		t.Fatalf("composed workspace should not target a worktree: %#v", plan)
	}
}

func TestDetectDestroyPlanTargetsLinkedWorktree(t *testing.T) {
	bin := t.TempDir()
	shim := `#!/bin/sh
case "$*" in
  *"rev-parse --abbrev-ref HEAD"*) echo "feat/destroy" ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	wt := t.TempDir()
	// A linked worktree's .git is a file pointing at the main repo metadata.
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /fake/main/.git/worktrees/wt"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := detectDestroyPlan(entry{name: "wt", kind: "project", path: wt})
	if plan.worktreePath != wt || plan.branch != "feat/destroy" {
		t.Fatalf("linked worktree plan = %#v, want worktreePath=%s branch=feat/destroy", plan, wt)
	}
}

func TestHierarchyNamesIndentByDepth(t *testing.T) {
	m := model{entries: []entry{{
		name: ".dotfiles",
		tabs: []tabItem{{title: "kesh", windows: []windowItem{{title: "shell"}}}},
	}}}
	tests := []struct {
		row  row
		name string
	}{
		{row: row{entryIndex: 0, tabIndex: -1, windowIndex: -1}, name: ".dotfiles"},
		{row: row{entryIndex: 0, tabIndex: 0, windowIndex: -1}, name: "kesh"},
		{row: row{entryIndex: 0, tabIndex: 0, windowIndex: 0}, name: "shell"},
	}
	columns := make([]int, 0, len(tests))
	for _, test := range tests {
		rendered := ansi.Strip(m.renderRow(test.row, 100, false))
		before, _, found := strings.Cut(rendered, test.name)
		if !found {
			t.Fatalf("row is missing name %q: %q", test.name, rendered)
		}
		columns = append(columns, lipgloss.Width(before))
	}
	if columns[1] != columns[0]+2 || columns[2] != columns[1]+2 {
		t.Fatalf("hierarchy name columns = %v, want each child indented by 2", columns)
	}
}

func TestWindowIconsOnlyAppearOnWindowRows(t *testing.T) {
	m := model{entries: []entry{{
		name: "repo", agent: "pi",
		tabs: []tabItem{{title: "code", agent: "pi", windows: []windowItem{{title: "terminal", agent: "pi"}}}},
	}}}
	entryRow := row{entryIndex: 0, tabIndex: -1, windowIndex: -1}
	tabRow := row{entryIndex: 0, tabIndex: 0, windowIndex: -1}
	windowRow := row{entryIndex: 0, tabIndex: 0, windowIndex: 0}
	for _, parent := range []row{entryRow, tabRow} {
		if rendered := ansi.Strip(m.renderRow(parent, 80, false)); strings.Contains(rendered, shellIcon) {
			t.Fatalf("parent row contains a window icon: %q", rendered)
		}
	}
	if rendered := ansi.Strip(m.renderRow(windowRow, 80, false)); !strings.Contains(rendered, "π") {
		t.Fatalf("agent window row is missing pi icon: %q", rendered)
	}
}

func TestRowsShowSecondDetailColumnOnlyWhenSpaceAllows(t *testing.T) {
	m := model{entries: []entry{
		{
			key: "repo", name: "repo", detail: "/workspace/repo", path: "/workspace/repo",
			tabs: []tabItem{{title: "code", detail: "1 window", windows: []windowItem{{title: "editor", detail: "/workspace/repo", cwd: "/workspace/repo", command: "nvim"}}}},
		},
		// A closed project (no tabs) is not a live session, so its folder path
		// stays accurate and remains eligible for the detail column.
		{key: "/workspace/other", name: "other", detail: "/workspace/other", path: "/workspace/other"},
	}}
	tests := []row{
		{entryIndex: 0, tabIndex: -1, windowIndex: -1},
		{entryIndex: 0, tabIndex: 0, windowIndex: -1},
		{entryIndex: 0, tabIndex: 0, windowIndex: 0},
	}
	// A live session row hides the misleading single path; only its name shows.
	if rendered := ansi.Strip(m.renderRow(tests[0], 100, false)); strings.Contains(rendered, "/workspace/repo") {
		t.Fatalf("session row should hide folder path: %q", rendered)
	}
	// A closed project row still shows its path as the wide detail column.
	closedRow := row{entryIndex: 1, tabIndex: -1, windowIndex: -1}
	if rendered := ansi.Strip(m.renderRow(closedRow, 100, false)); !strings.Contains(rendered, "/workspace/other") {
		t.Fatalf("closed entry row is missing wide detail column: %q", rendered)
	}
	// Tab rows no longer carry a window count (the ▸/▾ arrow signals windows).
	if rendered := ansi.Strip(m.renderRow(tests[1], 100, false)); strings.Contains(rendered, "1 window") {
		t.Fatalf("tab row should hide window count: %q", rendered)
	}
	if rendered := ansi.Strip(m.renderRow(tests[2], 100, false)); !strings.Contains(rendered, "") || !strings.Contains(rendered, "/workspace/repo") {
		t.Fatalf("window row is missing process icon/path detail: %q", rendered)
	}
	for _, selected := range tests {
		if rendered := ansi.Strip(m.renderRow(selected, 40, false)); strings.Contains(rendered, "/workspace/repo") {
			t.Fatalf("narrow row retained detail column: %q", rendered)
		}
	}
	if rendered := ansi.Strip(m.renderRow(closedRow, 40, false)); strings.Contains(rendered, "/workspace/other") {
		t.Fatalf("narrow closed row retained detail column: %q", rendered)
	}
}

func TestCleanAgentTitleOmitsAgentPrefixes(t *testing.T) {
	for _, test := range []struct {
		title, agent, want string
	}{
		{"⠋ π - .dotfiles", "pi", ".dotfiles"},
		{"󰚩 - api", "codex", "api"},
	} {
		if got := cleanAgentTitle(test.title, test.agent); got != test.want {
			t.Errorf("cleanAgentTitle(%q, %q) = %q, want %q", test.title, test.agent, got, test.want)
		}
	}
}

func TestPiWindowTitleOmitsPiPrefixInKesh(t *testing.T) {
	window := kittyWindow{
		ID: 1, Title: "⠋ π - .dotfiles", CWD: "/Users/azuan/.dotfiles",
		ForegroundProcesses: []kittyx.ForegroundProcess{{
			Cmdline: []string{"pi"},
			CWD:     "/Users/azuan/.dotfiles",
		}},
	}
	if got := windowItemFromKitty(window).title; got != ".dotfiles" {
		t.Fatalf("Pi window title = %q, want %q", got, ".dotfiles")
	}
}

func TestProcessIcon(t *testing.T) {
	tests := map[string]string{
		"nvim": "",
		"-zsh": "",
		"pi":   "",
		"git":  "",
	}
	for command, want := range tests {
		if got := processIcon(command); got != want {
			t.Errorf("processIcon(%q) = %q, want %q", command, got, want)
		}
	}
}

func TestWorktreeRowUsesResponsivePathColumn(t *testing.T) {
	worktree := worktreeItem{path: "/workspace/worktrees/repo/feature", branch: "feat/feature", prStatus: "open", prNumber: 42}
	m := model{
		filter:             filterWorktrees,
		entries:            []entry{{worktrees: []worktreeItem{worktree}}},
		worktreeFilterRows: []worktreeFilterRow{{worktree: worktree}},
	}
	worktreeRow := row{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0}
	rendered := ansi.Strip(m.renderRow(worktreeRow, 80, false))
	if !strings.Contains(rendered, "#42") || !strings.Contains(rendered, "feat/feature") {
		t.Fatalf("worktree row = %q", rendered)
	}
	if !strings.Contains(rendered, "/workspace/worktrees") {
		t.Fatalf("wide worktree row is missing path column: %q", rendered)
	}
	narrow := ansi.Strip(m.renderRow(worktreeRow, 50, false))
	if lipgloss.Width(narrow) > 50 || !strings.Contains(narrow, "feat/feature") {
		t.Fatalf("narrow worktree row is not width-safe: %q", narrow)
	}
}

func TestEntryDetailPanelShowsUniqueSessionDirectories(t *testing.T) {
	m := model{
		entries: []entry{{name: "session", tabs: []tabItem{
			{windows: []windowItem{{cwd: "/workspace/api"}, {cwd: "/workspace/api"}}},
			{windows: []windowItem{{cwd: "/workspace/web"}}},
		}}},
		rows: []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1}},
	}
	panel := ansi.Strip(m.detailPanelView(44, 10, false))
	for _, expected := range []string{"Paths", "/workspace/api", "/workspace/web"} {
		if !strings.Contains(panel, expected) {
			t.Fatalf("session detail panel missing %q:\n%s", expected, panel)
		}
	}
	if strings.Count(panel, "/workspace/api") != 1 {
		t.Fatalf("session detail panel did not deduplicate paths:\n%s", panel)
	}

	compact := ansi.Strip(m.detailPanelView(40, 5, true))
	if !strings.Contains(compact, "(+1 more)") {
		t.Fatalf("compact session detail panel does not summarize extra paths:\n%s", compact)
	}
}

func TestDetailPanelWrapsLongValuesWithHangingIndent(t *testing.T) {
	panel := ansi.Strip(renderDetailPanel("Info", []detailField{{
		label: "Path", value: "/workspace/worktrees/aurora/configurable-chat-component",
	}}, "", nil, 34, 10, false))
	lines := strings.Split(panel, "\n")
	for index, line := range lines {
		if !strings.Contains(line, "Path    /workspace") {
			continue
		}
		if index+1 >= len(lines) || !strings.HasPrefix(lines[index+1], "│        ") {
			t.Fatalf("wrapped detail value lacks hanging indent:\n%s", panel)
		}
		return
	}
	t.Fatalf("detail value did not wrap as expected:\n%s", panel)
}

func TestWorktreeInfoPanelIsResponsiveAndOmitsFullPRURL(t *testing.T) {
	worktree := worktreeItem{
		path: "/workspace/worktrees/repo/feature", branch: "feat/feature", prStatus: "open", prNumber: 42,
		prURL: "https://github.com/example/repo/pull/42",
	}
	full := worktreeInfoView(worktree, 80, false)
	plain := ansi.Strip(full)
	for _, field := range []string{"Worktree", "Branch", "Path", "PR", "#42", "g Open PR"} {
		if !strings.Contains(plain, field) {
			t.Fatalf("full info panel missing %q:\n%s", field, plain)
		}
	}
	if strings.Contains(plain, worktree.prURL) {
		t.Fatalf("full info panel exposes PR URL:\n%s", plain)
	}
	if got := lipgloss.Width(full); got > 80 {
		t.Fatalf("full info panel width = %d, want <= 80", got)
	}

	compact := worktreeInfoView(worktree, 40, true)
	if got := lipgloss.Height(compact); got > 5 {
		t.Fatalf("compact info panel height = %d, want <= 5", got)
	}
	if strings.Contains(ansi.Strip(compact), "g Open PR") {
		t.Fatalf("compact info panel should omit action help:\n%s", ansi.Strip(compact))
	}
}

func TestWorktreeDetailPanelShowsChangedFilesWhenDirty(t *testing.T) {
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries:                  []entry{{name: "repo", kind: "project", path: "/projects/repo"}},
		worktreeFilterRows: []worktreeFilterRow{{worktree: worktreeItem{
			path:    "/wt/feat",
			branch:  "feat/x",
			dirty:   true,
			changes: []string{" M main.go", "?? new.txt"},
		}}},
	}
	m.rows = []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0}}
	panel := ansi.Strip(m.detailPanelView(80, 20, false))
	for _, want := range []string{"Changes", "M main.go", "new.txt"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("detail panel missing %q:\n%s", want, panel)
		}
	}
	// A clean worktree omits the Changes section entirely.
	m.worktreeFilterRows[0].worktree.dirty = false
	m.worktreeFilterRows[0].worktree.changes = nil
	if clean := ansi.Strip(m.detailPanelView(80, 20, false)); strings.Contains(clean, "Changes") {
		t.Fatalf("clean worktree should not show Changes:\n%s", clean)
	}
}

func TestDetailPanelSupportsEveryRowType(t *testing.T) {
	m := model{entries: []entry{{
		name: "repo", kind: "project", path: "/workspace/repo", open: false,
		worktrees: []worktreeItem{{path: "/workspace/tree", branch: "feat/tree", prNumber: 42, prStatus: "open"}},
		tabs: []tabItem{{title: "code", windows: []windowItem{{
			title: "editor", cwd: "/workspace/repo", command: "nvim",
			pathPR: pathPRInfo{
				Branch: "feat/tree", Head: "local-head", Exact: false,
				PullRequest: prInfo{Status: "open", Number: 42, URL: "https://github.com/example/repo/pull/42"},
			},
		}}}},
	}}}
	m.worktreeFilterRows = []worktreeFilterRow{{worktree: m.entries[0].worktrees[0]}}
	tests := []struct {
		name string
		row  row
		want string
	}{
		{name: "entry", row: row{entryIndex: 0, tabIndex: -1, windowIndex: -1}, want: "Project"},
		{name: "tab", row: row{entryIndex: 0, tabIndex: 0, windowIndex: -1}, want: "Tab"},
		{name: "window", row: row{entryIndex: 0, tabIndex: 0, windowIndex: 0}, want: "Window"},
		{name: "worktree", row: row{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0}, want: "Worktree"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m.filter = filterAll
			if test.name == "worktree" {
				m.filter = filterWorktrees
			}
			m.rows = []row{test.row}
			m.cursor = 0
			panel := ansi.Strip(m.detailPanelView(80, 8, false))
			if !strings.Contains(panel, test.want) {
				t.Fatalf("detail panel does not contain %q:\n%s", test.want, panel)
			}
			if test.name == "window" {
				for _, prDetail := range []string{"#42", "local HEAD differs"} {
					if !strings.Contains(panel, prDetail) {
						t.Fatalf("window detail panel missing PR detail %q:\n%s", prDetail, panel)
					}
				}
				for _, omitted := range []string{"Open", "Closed", "Merged"} {
					if strings.Contains(panel, omitted) {
						t.Fatalf("window detail panel includes redundant PR text %q:\n%s", omitted, panel)
					}
				}
			}
		})
	}
}

func TestWideLayoutRendersAdjacentListAndDetailPanels(t *testing.T) {
	m := model{
		width: 120, height: 24,
		entries: []entry{{name: "repo", kind: "project", path: "/workspace/repo", session: "kesh-repo", saved: true}},
	}
	m.rebuildRows()
	view := ansi.Strip(m.View())
	for _, expected := range []string{"Project", "Name", "repo", "Path", "/workspace/repo"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("fixed detail panel missing %q:\n%s", expected, view)
		}
	}
	for _, omitted := range []string{"Session", "kesh-repo", "State"} {
		if strings.Contains(view, omitted) {
			t.Fatalf("detail panel contains redundant field %q:\n%s", omitted, view)
		}
	}
	adjacentPanels := false
	for _, line := range strings.Split(view, "\n") {
		if strings.Count(line, "╭") == 2 {
			adjacentPanels = true
			break
		}
	}
	if !adjacentPanels {
		t.Fatalf("wide layout does not place list and details side by side:\n%s", view)
	}
}

func TestFixedDetailPanelFitsSmallSplitForEntry(t *testing.T) {
	m := model{
		width: 50, height: 12,
		entries: []entry{{name: "repo", kind: "project", path: "/workspace/repo"}},
	}
	m.rebuildRows()
	view := m.View()
	if got := lipgloss.Width(view); got > m.width {
		t.Fatalf("small entry view width = %d, want <= %d", got, m.width)
	}
	if got := lipgloss.Height(view); got > m.height {
		t.Fatalf("small entry view height = %d, want <= %d\n%s", got, m.height, ansi.Strip(view))
	}
}

func TestWorktreeInfoFitsSmallSplit(t *testing.T) {
	worktree := worktreeItem{path: "/workspace/worktrees/repo/feature", branch: "feat/feature", prStatus: "open", prNumber: 42}
	m := model{
		width: 50, height: 12, filter: filterWorktrees, worktreeFilterEntryIndex: 0,
		entries: []entry{{
			name: "repo", kind: "project", path: "/workspace/repo",
			worktrees: []worktreeItem{worktree}, worktreesLoaded: true,
		}},
		worktreeFilterRows: []worktreeFilterRow{{worktree: worktree}},
	}
	m.rebuildWorktreeRows()
	view := m.View()
	if got := lipgloss.Width(view); got > m.width {
		t.Fatalf("small split view width = %d, want <= %d", got, m.width)
	}
	if got := lipgloss.Height(view); got > m.height {
		t.Fatalf("small split view height = %d, want <= %d\n%s", got, m.height, ansi.Strip(view))
	}
}

func TestSortWorktreesPrioritizesDefaultAndPRStatus(t *testing.T) {
	worktrees := []worktreeItem{
		{branch: "z-no-pr"},
		{branch: "closed", prStatus: "closed"},
		{branch: "main", prStatus: "merged", isDefault: true},
		{branch: "open", prStatus: "open"},
		{branch: "merged", prStatus: "merged"},
		{branch: "a-no-pr"},
	}
	sortWorktreeItems(worktrees)
	got := make([]string, len(worktrees))
	for index, worktree := range worktrees {
		got[index] = worktree.branch
	}
	want := []string{"main", "open", "merged", "closed", "a-no-pr", "z-no-pr"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("worktree order = %#v, want %#v", got, want)
	}
}

func TestPRStatusReorderPreservesFocusedWorktree(t *testing.T) {
	const repoKey = "git@github.com:example/repo.git"
	m := model{filter: filterWorktrees, worktreeFilterEntryIndex: 0, entries: []entry{{
		worktrees:       []worktreeItem{{path: "/selected", branch: "z-selected", head: "aaa", prRepoKey: repoKey}, {path: "/open", branch: "a-open", head: "bbb", prRepoKey: repoKey}},
		worktreesLoaded: true,
	}}}
	m.rebuildWorktreeRows()
	m.cursor = 0
	focused := m.focusedWorktreePath()
	m.applyPRStatuses(repoKey, map[string]prInfo{prStatusKey("a-open", "bbb"): {Status: "open"}})
	m.rebuildRows()
	m.restoreFocusedWorktree(focused)
	if got := m.focusedWorktreePath(); got != "/selected" {
		t.Fatalf("focused worktree after reorder = %q", got)
	}
}

func TestPRStatusCacheRoundTrip(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	repoKey := "git@github.com:loveholidays/aurora.git"
	statuses := map[string]prInfo{
		prStatusKey("feat/open", "aaa"):    {Status: "open", URL: "https://github.com/loveholidays/aurora/pull/1", Number: 1},
		prStatusKey("fix/merged", "bbb"):   {Status: "merged", URL: "https://github.com/loveholidays/aurora/pull/2", Number: 2},
		prStatusKey("fix/rejected", "ccc"): {Status: "closed", URL: "https://github.com/loveholidays/aurora/pull/3", Number: 3},
	}
	if err := savePRStatusCache(repoKey, statuses); err != nil {
		t.Fatal(err)
	}
	got, fetchedAt := loadPRStatusCache(repoKey)
	if !reflect.DeepEqual(got, statuses) {
		t.Fatalf("cached statuses = %#v, want %#v", got, statuses)
	}
	if fetchedAt.IsZero() || time.Since(fetchedAt) > time.Minute {
		t.Fatalf("unexpected cache timestamp: %v", fetchedAt)
	}
}

func TestApplyPRStatusesPrefersExactHeadAndFallsBackToBranch(t *testing.T) {
	const repoKey = "git@github.com:loveholidays/aurora.git"
	m := model{entries: []entry{{
		worktrees: []worktreeItem{
			{branch: "feat/open", head: "aaa", prRepoKey: repoKey},
			{branch: "feat/open", head: "newer", prRepoKey: repoKey, prStatus: "closed"},
		},
	}}}
	m.applyPRStatuses(repoKey, map[string]prInfo{
		prStatusKey("feat/open", "aaa"):  {Status: "open", URL: "https://github.com/loveholidays/aurora/pull/1", Number: 1},
		prStatusKey("fix/merged", "bbb"): {Status: "merged", URL: "https://github.com/loveholidays/aurora/pull/2", Number: 2},
	})
	if got := m.entries[0].worktrees[0].prStatus; got != "open" {
		t.Fatalf("open status = %q", got)
	}
	if got := m.entries[0].worktrees[0].prURL; got != "https://github.com/loveholidays/aurora/pull/1" {
		t.Fatalf("open PR URL = %q", got)
	}
	if got := m.entries[0].worktrees[1].prStatus; got != "open" {
		t.Fatalf("branch fallback status = %q", got)
	}
	if m.entries[0].worktrees[1].prExact {
		t.Fatal("branch fallback was incorrectly marked as an exact HEAD match")
	}
	if !m.entries[0].worktrees[0].prExact {
		t.Fatal("exact PR HEAD was not marked exact")
	}
}

func TestOpenWorktreePROpensExactCachedURL(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPEN_CAPTURE", filepath.Join(directory, "opened"))
	if err := os.WriteFile(filepath.Join(directory, "open"), []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$OPEN_CAPTURE\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	const pullRequestURL = "https://github.com/loveholidays/aurora/pull/9801"
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries:                  []entry{{worktrees: []worktreeItem{{branch: "fix/vite-websocket-port", prURL: pullRequestURL}}}},
		worktreeFilterRows: []worktreeFilterRow{{
			worktree: worktreeItem{branch: "fix/vite-websocket-port", prURL: pullRequestURL},
		}},
		rows: []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0}},
	}
	command := m.openWorktreePR()
	if command == nil {
		t.Fatalf("open PR command was not created: %v", m.err)
	}
	message := command().(openPRMsg)
	if message.err != nil {
		t.Fatal(message.err)
	}
	updatedModel, nextCommand := m.Update(message)
	if nextCommand != nil {
		t.Fatal("opening a PR should keep Kesh open")
	}
	if updatedModel.(model).err != nil {
		t.Fatalf("opening a PR returned an error: %v", updatedModel.(model).err)
	}
	content, err := os.ReadFile(os.Getenv("OPEN_CAPTURE"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(content); got != pullRequestURL {
		t.Fatalf("opened URL = %q", got)
	}

	m = model{
		entries: []entry{{tabs: []tabItem{{windows: []windowItem{{pathPR: pathPRInfo{
			Branch: "feat/window", PullRequest: prInfo{URL: pullRequestURL, Number: 42, Status: "open"},
		}}}}}}},
		rows: []row{{entryIndex: 0, tabIndex: 0, windowIndex: 0}},
	}
	message = m.openWorktreePR()().(openPRMsg)
	if message.err != nil {
		t.Fatal(message.err)
	}
}

func TestFetchWorktreesRefreshesPRStatusesInBackground(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(directory, "cache"))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	git := `#!/bin/sh
case "$*" in
  *"worktree list --porcelain"*)
    printf 'worktree %s/repo\nHEAD aaa\nbranch refs/heads/main\n\nworktree %s/tree\nHEAD bbb\nbranch refs/heads/feat/open\n' "$TMPDIR" "$TMPDIR"
    ;;
  *"remote get-url origin"*) printf '%s\n' 'git@github.com:example/repo.git' ;;
  *) exit 1 ;;
esac
`
	gh := `#!/bin/sh
printf '%s\n' '[{"headRefName":"feat/open","headRefOid":"bbb","state":"OPEN","mergedAt":null}]'
`
	if err := os.WriteFile(filepath.Join(directory, "git"), []byte(git), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "gh"), []byte(gh), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", directory)
	if err := os.Mkdir(filepath.Join(directory, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}

	m := model{
		entries: []entry{{name: "repo", kind: "project", path: filepath.Join(directory, "repo")}},
		rows:    []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1}},
	}
	listCommand := fetchWorktrees(m.entries[0].path, 0, -1, -1)
	if listCommand == nil {
		t.Fatal("worktree query was not started")
	}
	listedModel, refreshCommand := m.Update(listCommand())
	listed := listedModel.(model)
	if refreshCommand == nil {
		t.Fatal("background PR refresh was not started")
	}
	cachedModel, queryCommand := listed.Update(refreshCommand())
	if queryCommand == nil {
		t.Fatal("PR query was not started after the cache lookup")
	}
	refreshedModel, _ := cachedModel.(model).Update(queryCommand())
	refreshed := refreshedModel.(model)
	if got := refreshed.entries[0].worktrees[1].prStatus; got != "open" {
		t.Fatalf("PR status = %q, want open", got)
	}
	if _, err := os.Stat(prStatusCachePath()); err != nil {
		t.Fatalf("PR status cache was not written: %v", err)
	}
}

func TestRefreshFetchesBeforeReloading(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(directory, "cache"))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	git := `#!/bin/sh
case "$*" in
  *"fetch --prune"*) touch "$TMPDIR/fetched"; exit 0 ;;
  *"worktree list --porcelain"*) printf 'worktree %s/repo\nHEAD aaa\nbranch refs/heads/main\n' "$TMPDIR" ;;
  *"remote get-url origin"*) printf '%s\n' 'git@github.com:example/repo.git' ;;
  *) exit 1 ;;
esac
`
	gh := `#!/bin/sh
printf '%s\n' '[]'
`
	if err := os.WriteFile(filepath.Join(directory, "git"), []byte(git), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "gh"), []byte(gh), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", directory)
	if err := os.Mkdir(filepath.Join(directory, "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries:                  []entry{{name: "repo", kind: "project", path: filepath.Join(directory, "repo")}},
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("refresh did not start a fetch")
	}
	// Running the command executes `git fetch --prune` in the goroutine.
	updated.Update(cmd())
	if _, err := os.Stat(filepath.Join(directory, "fetched")); err != nil {
		t.Fatalf("refresh did not run git fetch: %v", err)
	}
}

func TestPullWorktreeRunsGitPullAndReloads(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", filepath.Join(directory, "cache"))
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	repo := filepath.Join(directory, "repo")
	git := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *"pull --rebase"*) touch "$TMPDIR/pulled"; exit 0 ;;
  *"worktree list --porcelain"*) printf 'worktree %%s\nHEAD aaa\nbranch refs/heads/main\n' "$TMPDIR/repo" ;;
  *"remote get-url origin"*) printf '%%s\n' 'git@github.com:example/repo.git' ;;
  *) exit 1 ;;
esac
`)
	gh := `#!/bin/sh
printf '%s\n' '[]'
`
	if err := os.WriteFile(filepath.Join(directory, "git"), []byte(git), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "gh"), []byte(gh), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", directory)
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries:                  []entry{{name: "repo", kind: "project", path: repo}},
		worktreeFilterRows:       []worktreeFilterRow{{worktree: worktreeItem{path: repo, branch: "main"}}},
	}
	m.rows = []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0}}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if !updated.(model).worktreePullBusy {
		t.Fatal("pull did not mark the worktree busy")
	}
	if cmd == nil {
		t.Fatal("pull did not start")
	}
	// Running the command executes `git pull --rebase` in the worktree.
	msg := cmd()
	if _, err := os.Stat(filepath.Join(directory, "pulled")); err != nil {
		t.Fatalf("pull did not run git pull: %v", err)
	}
	reloadModel, reloadCmd := updated.Update(msg)
	if reloadModel.(model).worktreePullBusy {
		t.Fatal("pull busy flag was not cleared after pull completed")
	}
	if reloadCmd == nil {
		t.Fatal("pull did not trigger a worktree reload")
	}
}

func TestWorktreeResultStaysOutOfMainHierarchyAndWOpensPrimarySurface(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project")
	m := model{
		entries: []entry{{name: "project", kind: "project", path: path}},
		rows:    []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1}},
	}

	updatedModel, _ := m.Update(worktreeListMsg{
		entryIndex: 0, tabIndex: -1, windowIndex: -1, dir: path,
		worktrees: []worktreeItem{{path: path, branch: "main", current: true}},
	})
	updated := updatedModel.(model)
	if !updated.entries[0].worktreesLoaded {
		t.Fatalf("worktree cache was not populated: %#v", updated.entries[0])
	}
	if len(updated.rows) != 1 || updated.rows[0].section != "" {
		t.Fatalf("main hierarchy contains duplicate worktree rows: %#v", updated.rows)
	}

	opened, _ := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	worktrees := opened.(model)
	if worktrees.filter != filterWorktrees || len(worktrees.rows) != 1 || worktrees.rows[0].section != "wt-filter" {
		t.Fatalf("w did not route to the primary Worktrees surface: filter=%d rows=%#v", worktrees.filter, worktrees.rows)
	}
}

func TestParseAheadBehind(t *testing.T) {
	cases := []struct {
		segment       string
		ahead, behind int
	}{
		{"[ahead 2, behind 1]", 2, 1},
		{"[behind 3]", 0, 3},
		{"[ahead 5]", 5, 0},
		{"", 0, 0},
		{"[gone]", 0, 0},
	}
	for _, c := range cases {
		ahead, behind := parseAheadBehind(c.segment)
		if ahead != c.ahead || behind != c.behind {
			t.Fatalf("parseAheadBehind(%q) = ahead %d behind %d, want %d %d", c.segment, ahead, behind, c.ahead, c.behind)
		}
	}
}

func TestParseWorktreeStatus(t *testing.T) {
	dirty, ahead, behind, changes := parseWorktreeStatus("## main...origin/main [ahead 2, behind 1]\n M main.go\n?? new.txt\nA  staged.go\n")
	if !dirty || ahead != 2 || behind != 1 {
		t.Fatalf("dirty=%v ahead=%d behind=%d", dirty, ahead, behind)
	}
	want := []string{" M main.go", "?? new.txt", "A  staged.go"}
	if len(changes) != len(want) {
		t.Fatalf("changes=%#v want %#v", changes, want)
	}
	for i := range want {
		if changes[i] != want[i] {
			t.Fatalf("changes[%d]=%q want %q (full: %#v)", i, changes[i], want[i], changes)
		}
	}

	// A clean worktree with no upstream reports no changes and no divergence.
	dirty, ahead, behind, changes = parseWorktreeStatus("## main\n")
	if dirty || ahead != 0 || behind != 0 || len(changes) != 0 {
		t.Fatalf("clean parse: dirty=%v ahead=%d behind=%d changes=%#v", dirty, ahead, behind, changes)
	}
}

func TestWorktreeTabRendersEachRowWithSyncBadge(t *testing.T) {
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries: []entry{{
			name:            "repo",
			kind:            "project",
			path:            "/projects/repo",
			worktreesLoaded: true,
			worktrees: []worktreeItem{
				{path: "/wt/main", branch: "main", current: true},
				{path: "/wt/feat", branch: "feat/x", ahead: 2, behind: 1, dirty: true},
			},
		}},
	}
	// In the Worktree tab, worktreeEntries resolves the open project.
	if got := m.worktreeEntries(); len(got) != 1 || got[0].path != "/projects/repo" {
		t.Fatalf("worktreeEntries = %#v", got)
	}
	m.rebuildRows()
	if len(m.rows) != 2 || m.rows[0].section != "wt-filter" || m.rows[1].section != "wt-filter" {
		t.Fatalf("rows = %#v", m.rows)
	}
	if m.rows[0].wt != 0 || m.rows[1].wt != 1 {
		t.Fatalf("per-row wt index not stored: %#v", m.rows)
	}
	// Each row renders its own worktree (not the focused one) with its sync badge.
	first := ansi.Strip(m.renderRow(m.rows[0], 100, false))
	second := ansi.Strip(m.renderRow(m.rows[1], 100, false))
	if !strings.Contains(second, "feat/x") || !strings.Contains(second, "↑2") ||
		!strings.Contains(second, "↓1") || !strings.Contains(second, "✱") {
		t.Fatalf("dirty/ahead/behind badge missing from row:\n%s", second)
	}
	if strings.Contains(first, "↑2") {
		t.Fatalf("first row should not show the second worktree's badge:\n%s", first)
	}
}

func TestLoadEntriesDoesNotInspectGitRepositoriesDuringStartup(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	t.Setenv("XDG_STATE_HOME", directory)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_STARTUP_CAPTURE", filepath.Join(directory, "git-called"))
	kitty := filepath.Join(directory, "kitty")
	zoxide := filepath.Join(directory, "zoxide")
	git := filepath.Join(directory, "git")
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf '%s\\n' '[{\"tabs\":[]}]'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zoxide, []byte("#!/bin/sh\nprintf '%s\\n' '/projects/repo'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(git, []byte("#!/bin/sh\ntouch \"$GIT_STARTUP_CAPTURE\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEntries(kitty, zoxide); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(os.Getenv("GIT_STARTUP_CAPTURE")); !os.IsNotExist(err) {
		t.Fatal("Kesh inspected Git repositories during startup")
	}
}

func TestLoadEntriesIncludesUnscopedTabs(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	t.Setenv("XDG_STATE_HOME", directory)
	project := "/projects/homelab"
	kitty := filepath.Join(directory, "kitty")
	kittyState := `[{"tabs":[{"id":6,"title":"homelab-code","windows":[{"id":70,"cwd":"/projects/homelab","last_focused_at":12,"foreground_processes":[{"cmdline":["codex"],"cwd":"/projects/homelab"}]}]}]}]`
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf '%s\\n' '"+kittyState+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	zoxide := filepath.Join(directory, "zoxide")
	if err := os.WriteFile(zoxide, []byte("#!/bin/sh\nprintf '%s\\n' '"+project+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	entries, err := loadEntries(kitty, zoxide)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want one merged project", entries)
	}
	projectEntry := entries[0]
	if projectEntry.kind != "project" || !projectEntry.open || projectEntry.path != project || len(projectEntry.tabs) != 1 {
		t.Fatalf("unscoped project was not merged with its open state: %#v", projectEntry)
	}
	if got := projectEntry.tabs[0]; got.title != "homelab-code" || got.agent != "codex" {
		t.Errorf("tab = %#v, want homelab-code Codex tab", got)
	}
}

func TestLoadEntriesMergesNamedSingleProjectSession(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	t.Setenv("XDG_STATE_HOME", directory)
	project := "/Users/stan/workspace/aurora"
	kitty := filepath.Join(directory, "kitty")
	kittyState := `[{"tabs":[{"id":6,"title":"frontier","windows":[{"id":70,"cwd":"/Users/stan/workspace/aurora","session_name":"aurora","last_focused_at":12}]}]}]`
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf '%s\\n' '"+kittyState+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	zoxide := filepath.Join(directory, "zoxide")
	if err := os.WriteFile(zoxide, []byte("#!/bin/sh\nprintf '%s\\n' '"+project+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	entries, err := loadEntries(kitty, zoxide)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want one merged project", entries)
	}
	projectEntry := entries[0]
	if projectEntry.kind != "project" || projectEntry.key != project || projectEntry.session != "aurora" || !projectEntry.open {
		t.Fatalf("project = %#v, want merged live aurora project", projectEntry)
	}
}

func TestLoadEntriesKeepsComposedWorkspaceSeparateFromProjectSources(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	t.Setenv("XDG_STATE_HOME", directory)
	aurora := filepath.Join(directory, "aurora")
	frontier := filepath.Join(directory, "frontier")
	for _, project := range []string{aurora, frontier} {
		if err := os.MkdirAll(filepath.Join(project, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	kitty := filepath.Join(directory, "kitty")
	kittyState := fmt.Sprintf(`[{"tabs":[{"id":6,"title":"aurora","windows":[{"id":70,"cwd":%q,"session_name":"kesh-aurora-frontier","last_focused_at":12}]},{"id":7,"title":"frontier","windows":[{"id":71,"cwd":%q,"session_name":"kesh-aurora-frontier","last_focused_at":11}]}]}]`, aurora, frontier)
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf '%s\\n' '"+kittyState+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	zoxide := filepath.Join(directory, "zoxide")
	if err := os.WriteFile(zoxide, []byte("#!/bin/sh\nprintf '%s\\n' '"+aurora+"\n"+frontier+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	entries, err := loadEntries(kitty, zoxide)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[0].kind != "workspace" || entries[0].key != "workspace:kesh-aurora-frontier" {
		t.Fatalf("entries = %#v, want composed workspace followed by two projects", entries)
	}
	for _, project := range entries[1:] {
		if project.kind != "project" || project.open {
			t.Fatalf("project source = %#v", project)
		}
	}
}

func TestLoadEntriesMergesSavedAndOpenWorkspace(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	t.Setenv("XDG_STATE_HOME", directory)
	project := "/projects/ksm"
	sessionFile := filepath.Join(directory, "kesh", "sessions", "ksm.kitty-session")
	store := savedSessionStore{
		Version: currentSavedSessionVersion,
		Sessions: map[string]savedSessionRecord{
			sessionFile: {
				Name: "ksm", SessionName: "ksm", SessionFile: sessionFile,
				Projects: []string{project}, SavedAt: "2026-07-22T15:00:00Z",
			},
		},
	}
	if err := saveSavedSessions(store); err != nil {
		t.Fatal(err)
	}
	kitty := filepath.Join(directory, "kitty")
	kittyState := `[{"tabs":[{"id":6,"title":"ksm","windows":[{"id":70,"cwd":"/projects/ksm","session_name":"ksm","last_focused_at":12}]}]}]`
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf '%s\\n' '"+kittyState+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	zoxide := filepath.Join(directory, "zoxide")
	if err := os.WriteFile(zoxide, []byte("#!/bin/sh\nprintf '%s\\n' '"+project+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	entries, err := loadEntries(kitty, zoxide)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %#v, want one merged saved project", entries)
	}
	projectEntry := entries[0]
	if projectEntry.kind != "project" || !projectEntry.open || !projectEntry.saved || projectEntry.sessionFile != sessionFile || projectEntry.key != project {
		t.Fatalf("merged saved project = %#v", projectEntry)
	}
}

func TestLoadEntriesIncludesClosedSavedWorkspace(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	t.Setenv("XDG_STATE_HOME", directory)
	project := "/projects/ksm"
	sessionFile := filepath.Join(directory, "kesh", "sessions", "ksm.kitty-session")
	store := savedSessionStore{
		Version: currentSavedSessionVersion,
		Sessions: map[string]savedSessionRecord{
			sessionFile: {
				Name: "ksm", SessionName: "ksm", SessionFile: sessionFile,
				Projects: []string{project}, SavedAt: "2026-07-22T15:00:00Z",
			},
		},
	}
	if err := saveSavedSessions(store); err != nil {
		t.Fatal(err)
	}
	kitty := filepath.Join(directory, "kitty")
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf '%s\\n' '[{\"tabs\":[]}]'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	zoxide := filepath.Join(directory, "zoxide")
	if err := os.WriteFile(zoxide, []byte("#!/bin/sh\nprintf '%s\\n' '"+project+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	entries, err := loadEntries(kitty, zoxide)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].kind != "project" || !entries[0].saved || entries[0].open || entries[0].sessionFile != sessionFile {
		t.Fatalf("closed saved project entry = %#v", entries)
	}
}

func TestLoadEntriesFastDoesNotBlockOnZoxide(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	t.Setenv("XDG_STATE_HOME", directory)
	t.Setenv("KESH_CONCURRENCY_DIR", directory)
	kitty := filepath.Join(directory, "kitty")
	writeBin(t, directory, "kitty", `printf '%s\n' '[{"tabs":[]}]'`)
	// A zoxide stub that records if it ever runs. loadEntriesFast must NOT call
	// it — calling it would gate first paint on the (sometimes slow) query.
	writeBin(t, directory, "zoxide", `touch "$KESH_CONCURRENCY_DIR/zoxide.ran"
printf '%s\n' '/projects/parallel'`)

	entries, _, err := loadEntriesFast(kitty)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("loadEntriesFast should return no source projects without zoxide, got %#v", entries)
	}
	if _, err := os.Stat(filepath.Join(directory, "zoxide.ran")); err == nil {
		t.Fatal("loadEntriesFast must not invoke zoxide — it would block first paint")
	}
}

func TestZoxideEntriesMergeAsynchronously(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("HOME", directory)
	t.Setenv("XDG_STATE_HOME", directory)
	t.Setenv("KESH_CONCURRENCY_DIR", directory)
	kitty := filepath.Join(directory, "kitty")
	writeBin(t, directory, "kitty", `printf '%s\n' '[{"tabs":[]}]'`)
	zoxide := filepath.Join(directory, "zoxide")
	writeBin(t, directory, "zoxide", `printf '%s\n' '/projects/parallel'`)

	entries, ctx, err := loadEntriesFast(kitty)
	if err != nil {
		t.Fatal(err)
	}
	m := model{entries: entries, zoxide: zoxide, zoxideCtx: ctx, zoxidePending: true, selected: map[string]bool{}}
	m.rebuildRows()
	if len(m.entries) != 0 {
		t.Fatalf("expected no entries before zoxide arrives, got %d", len(m.entries))
	}

	// Fire the async fetch and feed the resulting message back through Update.
	msg := fetchZoxideEntries(m.zoxide, m.zoxideCtx)()
	updated, _ := m.Update(msg)
	m = updated.(model)
	if m.zoxidePending {
		t.Fatal("expected zoxidePending cleared after merge")
	}
	var found bool
	for _, e := range m.entries {
		if e.key == "/projects/parallel" {
			found = true
		}
	}
	if !found {
		t.Fatal("zoxide project was not merged into entries")
	}
}

func TestIsKeshTab(t *testing.T) {
	kesh := kittyWindow{Cmdline: []string{"/Users/stan/.local/bin/kesh"}}
	if !isKeshTab([]kittyWindow{kesh}) {
		t.Fatal("expected dedicated Kesh tab to be excluded")
	}
	if isKeshTab([]kittyWindow{kesh, {Cmdline: []string{"zsh"}}}) {
		t.Fatal("expected a mixed tab to remain visible")
	}
}

func TestAgentFromWindow(t *testing.T) {
	tests := map[string]struct {
		processes [][]string
		want      string
	}{
		"pi":          {processes: [][]string{{"/Users/stan/.local/bin/pi"}}, want: "pi"},
		"codex":       {processes: [][]string{{"node", "/opt/homebrew/bin/codex"}}, want: "codex"},
		"both agents": {processes: [][]string{{"pi"}, {"codex"}}, want: "pi,codex"},
		"other shell": {processes: [][]string{{"zsh"}}, want: ""},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			window := kittyWindow{}
			for _, cmdline := range test.processes {
				window.ForegroundProcesses = append(window.ForegroundProcesses, struct {
					Cmdline []string `json:"cmdline"`
					CWD     string   `json:"cwd"`
				}{Cmdline: cmdline})
			}
			if got := agentFromWindow(window); got != test.want {
				t.Errorf("agentFromWindow() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAgentRowsAreFlatSearchableAndMostRecentFirst(t *testing.T) {
	m := model{
		filter: filterAgents,
		entries: []entry{
			{
				name: "dotfiles",
				tabs: []tabItem{{
					title: "config",
					windows: []windowItem{
						{id: 10, title: "shell", lastFocused: 30},
						{id: 11, title: "kesh", agent: "codex", detail: "~/.dotfiles", lastFocused: 20},
					},
				}},
			},
			{
				name: "api",
				tabs: []tabItem{{
					title:   "review",
					windows: []windowItem{{id: 12, title: "pi review", agent: "pi", detail: "~/api", lastFocused: 40}},
				}},
			},
		},
	}
	m.rebuildRows()
	if len(m.rows) != 2 {
		t.Fatalf("agent rows = %d, want 2", len(m.rows))
	}
	first := m.entries[m.rows[0].entryIndex].tabs[m.rows[0].tabIndex].windows[m.rows[0].windowIndex]
	if first.id != 12 {
		t.Fatalf("first agent window = %d, want most recently focused window 12", first.id)
	}

	m.query = "dtfls"
	m.rebuildRows()
	if len(m.rows) != 1 {
		t.Fatalf("project search returned %d rows, want 1", len(m.rows))
	}
	got := m.entries[m.rows[0].entryIndex].tabs[m.rows[0].tabIndex].windows[m.rows[0].windowIndex]
	if got.id != 11 {
		t.Errorf("project search selected window %d, want 11", got.id)
	}
}

func TestPreviewIgnoresStaleResponse(t *testing.T) {
	m := model{previewID: 12, previewBusy: true}
	updated, _ := m.Update(previewMsg{windowID: 11, content: "old"})
	got := updated.(model)
	if got.preview != "" || !got.previewBusy {
		t.Fatalf("stale preview changed model: %#v", got)
	}
}

func TestPreviewRefreshFetchesCurrentAgentScreen(t *testing.T) {
	directory := t.TempDir()
	kitty := filepath.Join(directory, "kitty")
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf refreshed\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	m := model{
		kitty: kitty, filter: filterAgents, showPreview: true, previewID: 11,
		entries: []entry{{tabs: []tabItem{{windows: []windowItem{{id: 11, agent: "codex"}}}}}},
	}
	m.rebuildRows()
	_, command := m.Update(previewRefreshMsg{windowID: 11, request: m.previewRequest})
	if command == nil {
		t.Fatal("current agent preview did not schedule a refresh")
	}
	msg := command().(previewMsg)
	if msg.err != nil || msg.content != "refreshed" {
		t.Fatalf("refresh result = %#v, want refreshed screen", msg)
	}
}

func TestPreviewIgnoresOlderRequestForSameWindow(t *testing.T) {
	m := model{previewID: 11, previewRequest: 2, previewBusy: true}
	updated, _ := m.Update(previewMsg{windowID: 11, request: 1, content: "old"})
	got := updated.(model)
	if got.preview != "" || !got.previewBusy {
		t.Fatalf("older same-window preview changed model: %#v", got)
	}
}

func TestRenameResultResolvesStableWindowAfterEntryReorder(t *testing.T) {
	m := model{entries: []entry{
		{key: "other", tabs: []tabItem{{id: 2, windows: []windowItem{{id: 20, title: "other"}}}}},
		{key: "target", tabs: []tabItem{{id: 1, windows: []windowItem{{id: 10, title: "old"}}}}},
	}}
	updated, _ := m.Update(renameMsg{
		selected: row{entryIndex: 0, tabIndex: 0, windowIndex: 0},
		target:   renameTarget{entryKey: "target", tabID: 1, windowID: 10},
		title:    "renamed",
	})
	got := updated.(model)
	if got.entries[1].tabs[0].windows[0].title != "renamed" || got.entries[0].tabs[0].windows[0].title != "other" {
		t.Fatalf("rename targeted wrong entry after reorder: %#v", got.entries)
	}
}

func TestWorktreeResultResolvesStableDirectoryAfterEntryReorder(t *testing.T) {
	m := model{entries: []entry{
		{key: "other", path: "/other"},
		{key: "target", path: "/repo"},
	}}
	updated, _ := m.Update(worktreeListMsg{
		entryIndex: 0,
		dir:        "/repo",
		worktrees:  []worktreeItem{{path: "/repo", branch: "main"}},
	})
	got := updated.(model)
	if !got.entries[1].worktreesLoaded || got.entries[0].worktreesLoaded {
		t.Fatalf("worktree result targeted wrong entry after reorder: %#v", got.entries)
	}
}

func TestWindowIconPrioritizesAgentIcons(t *testing.T) {
	if got := windowIcon(windowItem{command: "zsh", agent: "pi"}); got != "π" {
		t.Errorf("pi window icon = %q, want π", got)
	}
	if got := windowIcon(windowItem{command: "zsh", agent: "codex"}); got != "󰚩" {
		t.Errorf("Codex window icon = %q, want Codex icon", got)
	}
	if got := windowIcon(windowItem{command: "nvim"}); got != "" {
		t.Errorf("Neovim window icon = %q, want editor icon", got)
	}
	if got := windowIcon(windowItem{command: "unknown"}); got != shellIcon {
		t.Errorf("unrecognized window icon = %q, want shell icon", got)
	}
}

func TestAgentRowPrioritizesProjectNameOverTruncatedTabTitle(t *testing.T) {
	m := model{showPreview: true}
	line := ansi.Strip(m.renderAgentRow(
		entry{name: "configurable-chat-component"},
		tabItem{title: "aurora-long-running-agent-task"},
		windowItem{agent: "pi"},
		44,
	))
	if !strings.Contains(line, "configurable-chat-component") {
		t.Errorf("agent row did not retain its project name: %q", line)
	}
	if strings.Contains(line, "…") || strings.Contains(line, "aurora") {
		t.Errorf("agent row retained a truncated tab title: %q", line)
	}
}

func TestAgentViewRendersFlatRowAndPreview(t *testing.T) {
	m := model{
		filter: filterAgents, showPreview: true, width: 120, height: 30,
		entries: []entry{{
			name: "dotfiles",
			tabs: []tabItem{{
				title:   "agents",
				windows: []windowItem{{id: 11, agent: "codex", detail: "~/.dotfiles", lastFocused: 20}},
			}},
		}},
	}
	m.rebuildRows()
	m.queuePreview()
	view := m.View()
	for _, expected := range []string{"Agents", "Codex", "dotfiles", "Agent screen", "Loading preview"} {
		if !strings.Contains(view, expected) {
			t.Errorf("agent view does not contain %q:\n%s", expected, view)
		}
	}
}

func TestFetchPreviewRemovesBackgroundAndTrailingBlankLines(t *testing.T) {
	directory := t.TempDir()
	kitty := filepath.Join(directory, "kitty")
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf '\\033[38;5;42m\\033[48;5;22mready\\033[0m\\n\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	msg := fetchPreview(kitty, 42)().(previewMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if strings.Contains(msg.content, "[48;") {
		t.Errorf("preview retained background colour: %q", msg.content)
	}
	if !strings.Contains(msg.content, "[38;5;42m") || !strings.HasSuffix(msg.content, "ready\x1b[0m") {
		t.Errorf("preview = %q, want foreground colour and no trailing blank lines", msg.content)
	}
}

func TestValidSlot(t *testing.T) {
	for _, slot := range []string{"0", "1", "9"} {
		if !validSlot(slot) {
			t.Fatalf("expected %q to be valid", slot)
		}
	}
	for _, slot := range []string{"", "10", "a", "-1"} {
		if validSlot(slot) {
			t.Fatalf("expected %q to be invalid", slot)
		}
	}
}

func TestLoadCloneRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	root, err := loadCloneRoot()
	if err != nil || root != filepath.Join(home, "workspace") {
		t.Fatalf("default clone root = (%q, %v)", root, err)
	}

	path := filepath.Join(home, ".config", "kesh", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("clone:\n  root: ~/code\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root, err = loadCloneRoot()
	if err != nil || root != filepath.Join(home, "code") {
		t.Fatalf("configured clone root = (%q, %v)", root, err)
	}
}

func TestRepositoryNameAndCloneDestination(t *testing.T) {
	for repository, want := range map[string]string{
		"https://github.com/example/project.git": "project",
		"git@github.com:example/project.git":     "project",
		"example/project":                        "project",
		"/tmp/local-project/":                    "local-project",
	} {
		got, err := repositoryName(repository)
		if err != nil || got != want {
			t.Errorf("repositoryName(%q) = (%q, %v), want (%q, nil)", repository, got, err, want)
		}
	}
	for _, repository := range []string{"", "https://github.com/", "-option"} {
		if _, err := repositoryName(repository); err == nil {
			t.Errorf("repositoryName(%q) did not fail", repository)
		}
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "workspace")
	got, err := resolveCloneDestination("custom/project", root)
	if err != nil || got != filepath.Join(root, "custom", "project") {
		t.Fatalf("relative clone destination = (%q, %v)", got, err)
	}
	got, err = resolveCloneDestination("~/code/project", root)
	if err != nil || got != filepath.Join(home, "code", "project") {
		t.Fatalf("home clone destination = (%q, %v)", got, err)
	}
}

func TestParsePullRequestInput(t *testing.T) {
	tests := []struct {
		value       string
		owner       string
		repo        string
		number      int
		useSelected bool
		wantErr     bool
	}{
		{"https://github.com/owner/repo/pull/123", "owner", "repo", 123, false, false},
		{"https://github.com/owner/repo/pull/123/files", "owner", "repo", 123, false, false},
		{"https://github.com/owner/repo/pulls/456", "owner", "repo", 456, false, false},
		{"https://git.example.com/acme/widget/pull/9", "acme", "widget", 9, false, false},
		{"owner/repo#123", "owner", "repo", 123, false, false},
		{"  owner/repo#42  ", "owner", "repo", 42, false, false},
		{"123", "", "", 123, true, false},
		{"  7 ", "", "", 7, true, false},
		// Errors
		{"", "", "", 0, false, true},
		{"git@github.com:owner/repo.git", "", "", 0, false, true},
		{"https://github.com/owner/repo", "", "", 0, false, true},
		{"owner/repo", "", "", 0, false, true},
		{"https://github.com/owner/repo/pull/abc", "", "", 0, false, true},
		{"owner#0", "", "", 0, false, true},
		{"owner/repo#-3", "", "", 0, false, true},
	}
	for _, tt := range tests {
		owner, repo, number, useSelected, err := parsePullRequestInput(tt.value)
		if tt.wantErr {
			if err == nil {
				t.Errorf("parsePullRequestInput(%q) expected error, got (%q, %q, %d, %v)", tt.value, owner, repo, number, useSelected)
			}
			continue
		}
		if err != nil || owner != tt.owner || repo != tt.repo || number != tt.number || useSelected != tt.useSelected {
			t.Errorf("parsePullRequestInput(%q) = (%q, %q, %d, %v, %v), want (%q, %q, %d, %v, nil)",
				tt.value, owner, repo, number, useSelected, err, tt.owner, tt.repo, tt.number, tt.useSelected)
		}
	}
}

func TestViewHeightStaysStableForWorktreeRows(t *testing.T) {
	worktrees := []worktreeItem{{branch: "master", path: "/workspace/repo"}, {branch: "fix/a-long-branch-name", path: "/workspace/worktree/repo/fix-a-long-branch-name"}}
	m := model{
		width: 100, height: 24, filter: filterWorktrees, worktreeFilterEntryIndex: 0,
		entries: []entry{{
			key: "repo", name: "repo", path: "/workspace/repo", open: true,
			worktreesLoaded: true, worktrees: worktrees,
		}},
	}
	m.rebuildWorktreeRows()
	for cursor := range m.rows {
		m.cursor = cursor
		if got := lipgloss.Height(m.View()); got != m.height {
			t.Fatalf("cursor %d view height = %d, want %d", cursor, got, m.height)
		}
	}
}

func TestLoadRecipe(t *testing.T) {
	repo := t.TempDir()
	if err := run("git", "-C", repo, "init"); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(repo, ".kesh.yaml")
	if err := os.WriteFile(configPath, []byte("workspace_mode: all\nworkspaces:\n  - name: api\n  - name: web\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recipe, gotPath, err := loadRecipe(repo)
	if err != nil || recipe == nil || recipe.WorkspaceMode != "all" || len(recipe.Workspaces) != 2 || gotPath != configPath {
		t.Fatalf("loadRecipe() = (%#v, %q, %v)", recipe, gotPath, err)
	}
}

func TestWorktreeDirectoryName(t *testing.T) {
	if got := worktreeDirectoryName("fix/verify-asset-sync-on-build"); got != "fix-verify-asset-sync-on-build" {
		t.Fatalf("worktreeDirectoryName() = %q", got)
	}
}

func TestCloneFormShowsAndUpdatesBothFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "workspace")
	m := model{
		modeState: modeState{
			mode:      modeClone,
			cloneForm: &cloneForm{cloneRoot: root, cloneDestination: "~/workspace"},
		},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("git@github.com:example/project.git"), Paste: true})
	m = updated.(model)
	if cmd != nil || m.cloneDestination != "~/workspace/project" {
		t.Fatalf("clone form destination = %q, cmd = %v", m.cloneDestination, cmd)
	}
	popup := m.popupView(100)
	plainPopup := ansi.Strip(popup)
	if !strings.Contains(plainPopup, "Repository: git@github.com:example/project.git") || !strings.Contains(plainPopup, "Clone into: ~/workspace/project") {
		t.Fatalf("clone popup does not show both fields:\n%s", popup)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if !m.cloneDestinationFocus {
		t.Fatal("tab did not focus the clone destination")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("~/code/custom")})
	m = updated.(model)
	if m.cloneDestination != "~/code/custom" || !m.cloneDestinationEdited {
		t.Fatalf("edited clone destination = %q, edited = %t", m.cloneDestination, m.cloneDestinationEdited)
	}
}

func TestPRCheckoutPopupShowsResolvedTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	checkoutRoot := filepath.Join(home, "workspace")
	cloneRoot := filepath.Join(home, "workspace")
	worktreeRoot := filepath.Join(home, "worktree")
	// Existing clone under the checkout root → preview shows the full worktree path.
	if err := os.MkdirAll(filepath.Join(checkoutRoot, "owner", "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := model{
		modeState: modeState{
			mode:         modeCheckoutPR,
			checkoutForm: &checkoutForm{checkoutRoot: checkoutRoot, checkoutCloneRoot: cloneRoot},
		},
		worktreeRoot: worktreeRoot,
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("https://github.com/owner/repo/pull/123"), Paste: true})
	m = updated.(model)
	if cmd == nil || m.prCheckoutValue == "" {
		t.Fatalf("PR input not captured or preview lookup not started: %q (cmd=%v)", m.prCheckoutValue, cmd)
	}
	updated, _ = m.Update(prPreviewMsg{
		value: m.prCheckoutValue, branch: "feature/checkout",
		repoPath: filepath.Join(checkoutRoot, "owner", "repo"),
	})
	m = updated.(model)
	popup := ansi.Strip(m.popupView(100))
	if !strings.Contains(popup, "Checkout pull request") || !strings.Contains(popup, "Root repo path: ~/workspace/owner/repo") {
		t.Fatalf("PR popup missing title/summary:\n%s", popup)
	}
	if !strings.Contains(popup, "Worktree path: ~/worktree/owner/repo/feature-checkout") {
		t.Fatalf("PR popup does not show full worktree path:\n%s", popup)
	}

	// A repo not present locally → preview marks a fresh clone destination.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("https://github.com/other/widget/pull/7")})
	m = updated.(model)
	updated, _ = m.Update(prPreviewMsg{
		value:    m.prCheckoutValue,
		branch:   "fix/widget",
		repoPath: filepath.Join(checkoutRoot, "other", "widget"),
		newClone: true,
	})
	m = updated.(model)
	popup = ansi.Strip(m.popupView(100))
	if !strings.Contains(popup, "Root repo path: ~/workspace/other/widget (new clone)") || !strings.Contains(popup, "Worktree path: ~/worktree/other/widget/fix-widget (new clone)") {
		t.Fatalf("PR popup does not show clone target:\n%s", popup)
	}
}

func TestSaveOpenProjectRequiresConfirmation(t *testing.T) {
	e := entry{key: "/projects/ksm", name: "ksm", kind: "project", session: "ksm", open: true}
	m := model{
		entries: []entry{e},
		rows:    []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1}},
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(model)
	if m.mode != modeSaveConfirm || m.saving || cmd != nil {
		t.Fatalf("save confirmation state = confirming:%t saving:%t cmd:%v", m.mode == modeSaveConfirm, m.saving, cmd)
	}
	popup := ansi.Strip(m.popupView(80))
	if !strings.Contains(popup, `Save "ksm" for later restoration?`) || !strings.Contains(popup, "Press y to confirm") {
		t.Fatalf("save confirmation popup:\n%s", popup)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.mode == modeSaveConfirm || m.saving || cmd != nil {
		t.Fatalf("escape did not cancel save confirmation: %#v, cmd:%v", m, cmd)
	}
}

func TestSaveWithForegroundCommandsShowsStrongConfirmation(t *testing.T) {
	e := entry{
		key: "workspace:ksm", name: "ksm", kind: "workspace", session: "ksm", open: true,
		tabs: []tabItem{{windows: []windowItem{
			{command: "zsh", fullCommand: "-zsh"},
			{command: "pnpm", fullCommand: "pnpm run dev"},
		}}},
	}
	m := model{
		entries: []entry{e},
		rows:    []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1}},
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = updated.(model)
	if m.mode != modeSaveConfirm || !m.saveForeground || cmd != nil {
		t.Fatalf("foreground save confirmation = confirming:%t foreground:%t cmd:%v", m.mode == modeSaveConfirm, m.saveForeground, cmd)
	}
	popup := ansi.Strip(m.popupView(100))
	if !strings.Contains(popup, "Save with running commands") || !strings.Contains(popup, "pnpm run dev") || strings.Contains(popup, "-zsh") {
		t.Fatalf("foreground save confirmation popup:\n%s", popup)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if m.mode == modeSaveConfirm || !m.saving || cmd == nil {
		t.Fatalf("foreground save was not confirmed: confirming:%t saving:%t cmd:%v", m.mode == modeSaveConfirm, m.saving, cmd)
	}
}

func TestRunSaveSessionPersistsWorkspaceSnapshot(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("XDG_STATE_HOME", directory)
	logPath := filepath.Join(directory, "kitty.log")
	kitty := filepath.Join(directory, "kitty")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
for last; do :; done
printf 'layout splits\ncd /projects/ksm\nlaunch\n' > "$last"
`, logPath)
	if err := os.WriteFile(kitty, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	e := entry{
		key: "workspace:ksm", name: "ksm", kind: "workspace", path: "/projects/ksm",
		session: "ksm", open: true,
	}
	msg := runSaveSession(kitty, e, 3, false)().(saveSessionMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if msg.entryIndex != 3 || msg.record.SessionName != "ksm" || msg.record.SessionFile != savedSessionFilePath("ksm") {
		t.Fatalf("save message = %#v", msg)
	}
	info, err := os.Stat(msg.record.SessionFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("saved Kitty session permissions are %o, want 600", info.Mode().Perm())
	}
	store, err := loadSavedSessions()
	if err != nil || !reflect.DeepEqual(store.Sessions[msg.record.SessionFile], msg.record) {
		t.Fatalf("stored session = %#v, err = %v", store, err)
	}
	command, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(command), "@ action save_as_session --save-only --match=session:^ksm$ "+msg.record.SessionFile) {
		t.Fatalf("Kitty save command = %q", command)
	}

	msg = runSaveSession(kitty, e, 3, true)().(saveSessionMsg)
	if msg.err != nil || !msg.record.ForegroundCommands {
		t.Fatalf("foreground save message = %#v", msg)
	}
	command, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(command), "@ action save_as_session --save-only --use-foreground-process --match=session:^ksm$ "+msg.record.SessionFile) {
		t.Fatalf("foreground Kitty save command = %q", command)
	}
}

func TestRunActionUsesSavedSessionFileForOpenAndClosedWorkspace(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "kitty.log")
	kitty := filepath.Join(directory, "kitty")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\n", logPath)
	if err := os.WriteFile(kitty, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "ksm.kitty-session")
	e := entry{key: "workspace:ksm", session: "ksm", sessionFile: file, saved: true}
	selected := row{entryIndex: 0, tabIndex: -1, windowIndex: -1}
	for _, open := range []bool{false, true} {
		e.open = open
		msg := runAction(kitty, "", e, selected)().(actionMsg)
		if msg.err != nil {
			t.Fatal(msg.err)
		}
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "@ action goto_session " + file
	if strings.Count(string(commands), want) != 2 {
		t.Fatalf("Kitty commands = %q, want two %q actions", commands, want)
	}
}

func TestRunCloneOpensProjectAndAddsItToZoxide(t *testing.T) {
	directory := t.TempDir()
	logPath := filepath.Join(directory, "commands.log")
	writeCommand := func(name, body string) string {
		path := filepath.Join(directory, name)
		script := fmt.Sprintf("#!/bin/sh\nprintf '%s:%%s\\n' \"$*\" >> %q\n%s\n", name, logPath, body)
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	writeCommand("git", `mkdir -p "$4"`)
	kitty := writeCommand("kitty", "")
	zoxide := writeCommand("zoxide", "")
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", directory)

	destination := filepath.Join(directory, "clones", "project")
	msg := runClone(kitty, zoxide, "git@github.com:example/project.git", destination)().(cloneMsg)
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if info, err := os.Stat(destination); err != nil || !info.IsDir() {
		t.Fatalf("clone destination was not created: %v", err)
	}
	sessionPath := filepath.Join(os.TempDir(), "kitty-zoxide-sessions", "project.kitty-session")
	session, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(session), "cd "+destination+"\n") || strings.Contains(string(session), "cd \"") {
		t.Fatalf("generated session has an invalid working directory:\n%s", session)
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(commands)
	for _, expected := range []string{
		"git:clone -- git@github.com:example/project.git " + destination,
		"kitty:@ action goto_session ",
		"zoxide:add -- " + destination,
	} {
		if !strings.Contains(log, expected) {
			t.Errorf("command log does not contain %q:\n%s", expected, log)
		}
	}
}

func TestPinsRoundTrip(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	want := pinStore{
		"0": {Key: "/projects/zero", Name: "zero"},
		"9": {Key: "ssh://production", Name: "production"},
	}
	if err := savePins(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadPins()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || got["0"] != want["0"] || got["9"] != want["9"] {
		t.Fatalf("unexpected pins: %#v", got)
	}
	info, err := os.Stat(filepath.Join(stateHome, "kesh", "pins.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("pin state permissions are %o, want 600", info.Mode().Perm())
	}
}

func TestClearAllPinsResetsStateAndKittyMappings(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	kittyLog := filepath.Join(stateHome, "kitty.log")
	kitty := filepath.Join(stateHome, "kitty")
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf '%s' \"$*\" > "+kittyLog+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := savePins(pinStore{"2": {Key: "/projects/old", Name: "old"}}); err != nil {
		t.Fatal(err)
	}
	if err := clearAllPins(kitty, true); err != nil {
		t.Fatal(err)
	}
	pins, err := loadPins()
	if err != nil || len(pins) != 0 {
		t.Fatalf("pins after clear = %#v, err = %v", pins, err)
	}
	shortcuts, err := os.ReadFile(pinShortcutsPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(shortcuts), "goto_session") || !strings.Contains(string(shortcuts), "map cmd+2\n") {
		t.Fatalf("shortcuts after clear:\n%s", shortcuts)
	}
	command, err := os.ReadFile(kittyLog)
	if err != nil || string(command) != "@ load-config" {
		t.Fatalf("Kitty reload = %q, err = %v", command, err)
	}
}

func TestKittyRunLifecycleClearsPinsAfterUncleanExit(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	kittyLog := filepath.Join(stateHome, "kitty.log")
	kitty := filepath.Join(stateHome, "kitty")
	if err := os.WriteFile(kitty, []byte("#!/bin/sh\nprintf '%s' \"$*\" >> "+kittyLog+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := savePins(pinStore{"2": {Key: "/projects/stale", Name: "stale"}}); err != nil {
		t.Fatal(err)
	}
	marker := kittyRunPath()
	if err := os.WriteFile(marker, []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := beginKittyRun(kitty, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	pins, err := loadPins()
	if err != nil || len(pins) != 0 {
		t.Fatalf("pins after unclean exit recovery = %#v, err = %v", pins, err)
	}
	markerContent, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(markerContent)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("run marker = %q, err = %v", markerContent, err)
	}
	if err := savePins(pinStore{"3": {Key: "/projects/current", Name: "current"}}); err != nil {
		t.Fatal(err)
	}
	if err := beginKittyRun(kitty, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	pins, err = loadPins()
	if err != nil || len(pins) != 1 || pins["3"].Key != "/projects/current" {
		t.Fatalf("pins from active run = %#v, err = %v", pins, err)
	}
	if err := endKittyRun(kitty); err != nil {
		t.Fatal(err)
	}
	pins, err = loadPins()
	if err != nil || len(pins) != 0 {
		t.Fatalf("pins after normal exit = %#v, err = %v", pins, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("run marker still exists: %v", err)
	}
	command, err := os.ReadFile(kittyLog)
	if err != nil || string(command) != "@ load-config" {
		t.Fatalf("Kitty recovery reload = %q, err = %v", command, err)
	}
}

func TestClearStalePinsIfNeededClearsAfterDeadKitty(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", stateHome)
	t.Setenv("KESH_KITTY_PID", "") // picker path resolves Kitty via parent PID

	if err := savePins(pinStore{"2": {Key: "/projects/stale", Name: "stale"}}); err != nil {
		t.Fatal(err)
	}
	marker := kittyRunPath()
	if err := os.WriteFile(marker, []byte("999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearStalePinsIfNeeded(); err != nil {
		t.Fatal(err)
	}
	pins, err := loadPins()
	if err != nil || len(pins) != 0 {
		t.Fatalf("stale pins were not cleared: %#v, err = %v", pins, err)
	}
	markerContent, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(markerContent)) != strconv.Itoa(currentKittyPID()) {
		t.Fatalf("run marker = %q, err = %v", markerContent, err)
	}
}

func TestClearStalePinsIfNeededKeepsPinsWhileKittyAlive(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", stateHome)

	if err := savePins(pinStore{"3": {Key: "/projects/current", Name: "current"}}); err != nil {
		t.Fatal(err)
	}
	marker := kittyRunPath()
	// Own PID is, by definition, still running — pins must survive.
	if err := os.WriteFile(marker, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := clearStalePinsIfNeeded(); err != nil {
		t.Fatal(err)
	}
	pins, err := loadPins()
	if err != nil || len(pins) != 1 || pins["3"].Key != "/projects/current" {
		t.Fatalf("pins from active run were cleared: %#v, err = %v", pins, err)
	}
}

func TestSavedSessionFilePreservesKittySessionName(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	got := savedSessionFilePath("project workspace")
	if filepath.Base(got) != "project workspace.kitty-session" {
		t.Fatalf("saved session filename = %q", got)
	}
}

func TestSavedSessionsRoundTrip(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	file := filepath.Join(stateHome, "kesh", "sessions", "ksm.kitty-session")
	want := savedSessionStore{
		Version: currentSavedSessionVersion,
		Sessions: map[string]savedSessionRecord{
			file: {
				Name: "ksm", SessionName: "ksm", SessionFile: file,
				Projects: []string{"/projects/ksm"}, SavedAt: "2026-07-22T15:00:00Z",
			},
		},
	}
	if err := saveSavedSessions(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadSavedSessions()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("saved sessions = %#v, want %#v", got, want)
	}
	info, err := os.Stat(filepath.Join(stateHome, "kesh", "saved-sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("saved session state permissions are %o, want 600", info.Mode().Perm())
	}
}

func TestDeleteSavedSessionRemovesCatalogAndSnapshot(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	file := savedSessionFilePath("ksm")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("launch\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := savedSessionStore{
		Version: currentSavedSessionVersion,
		Sessions: map[string]savedSessionRecord{
			file: {Name: "ksm", SessionName: "ksm", SessionFile: file},
		},
	}
	if err := saveSavedSessions(store); err != nil {
		t.Fatal(err)
	}
	if err := deleteSavedSession(entry{saved: true, sessionFile: file}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("saved session file still exists: %v", err)
	}
	got, err := loadSavedSessions()
	if err != nil || len(got.Sessions) != 0 {
		t.Fatalf("saved session catalog = %#v, err = %v", got, err)
	}
}

func TestPinShortcutsGenerateNativeKittyMappings(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	pins := pinStore{
		"1": {SessionFile: filepath.Join(stateHome, "kesh", "sessions", "project one.kitty-session")},
		"9": {SessionFile: filepath.Join(stateHome, "kesh", "sessions", "production.kitty-session")},
	}

	content := string(pinShortcutsContent(pins))
	for _, expected := range []string{
		"map cmd+0\n",
		"map cmd+1 goto_session \"" + pins["1"].SessionFile + "\"\n",
		"map cmd+9 goto_session \"" + pins["9"].SessionFile + "\"\n",
	} {
		if !strings.Contains(content, expected) {
			t.Errorf("shortcut config does not contain %q:\n%s", expected, content)
		}
	}

	changed, err := savePinShortcuts(pins)
	if err != nil || !changed {
		t.Fatalf("first shortcut save = (%t, %v), want (true, nil)", changed, err)
	}
	changed, err = savePinShortcuts(pins)
	if err != nil || changed {
		t.Fatalf("unchanged shortcut save = (%t, %v), want (false, nil)", changed, err)
	}
	info, err := os.Stat(filepath.Join(stateHome, "kesh", "kitty-pins.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("shortcut config permissions are %o, want 600", info.Mode().Perm())
	}
}

func TestLoadPinsRejectsInvalidState(t *testing.T) {
	tests := map[string]string{
		"malformed JSON":      `{`,
		"invalid slot":        `{"10":{"key":"/projects/ten","name":"ten"}}`,
		"empty key":           `{"1":{"key":"","name":"empty"}}`,
		"duplicate pin":       `{"1":{"key":"/same","name":"same"},"2":{"key":"/same","name":"same"}}`,
		"invalid kind":        `{"1":{"key":"/project","name":"project","kind":"other"}}`,
		"invalid version":     `{"1":{"key":"/project","name":"project","kind":"project","version":99}}`,
		"unsafe session file": `{"1":{"key":"/project","name":"project","session_file":"/tmp/outside.kitty-session"}}`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			stateHome := t.TempDir()
			t.Setenv("XDG_STATE_HOME", stateHome)
			path := filepath.Join(stateHome, "kesh", "pins.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadPins(); err == nil {
				t.Fatal("expected invalid pin state to fail")
			}
		})
	}
}

func TestLegacyProjectPinMigratesToMatchingWorkspace(t *testing.T) {
	path := "/projects/aurora"
	pins := pinStore{
		"1": {Key: path, Name: "aurora | frontier", Kind: "project"},
		"2": {Key: "/projects/closed", Name: "closed", Kind: "project"},
	}
	workspaces := []entry{
		{key: "workspace:kesh-aurora", name: "aurora | frontier", kind: "workspace", path: path},
		{key: path, name: "aurora", kind: "project", path: path},
	}
	got, changed := migrateLegacyPins(workspaces, pins)
	if !changed {
		t.Fatal("legacy pins were not migrated")
	}
	if target := got["1"]; target.Key != "workspace:kesh-aurora" || target.Kind != "workspace" || target.Version != currentPinVersion {
		t.Fatalf("workspace pin = %#v", target)
	}
	if target := got["2"]; target.Key != "/projects/closed" || target.Kind != "project" || target.Version != currentPinVersion {
		t.Fatalf("closed project pin = %#v", target)
	}
}

func TestWorkspacePinMigratesToMergedProject(t *testing.T) {
	project := "/projects/dotfiles"
	pins := pinStore{
		"0": {Key: "workspace:.dotfiles", Name: ".dotfiles", Kind: "workspace", Version: currentPinVersion},
	}
	entries := []entry{{key: project, name: ".dotfiles", kind: "project", path: project, session: ".dotfiles"}}
	got, changed := migrateLegacyPins(entries, pins)
	if !changed {
		t.Fatal("workspace pin was not migrated to the merged project")
	}
	if target := got["0"]; target.Key != project || target.Kind != "project" || target.Version != currentPinVersion {
		t.Fatalf("merged project pin = %#v", target)
	}
}

func TestWorkspaceNamesRoundTripInConfigHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	want := nameStore{
		"/projects/payments": "Payments",
		"ssh://production":   "Production",
	}
	if err := saveNames(want); err != nil {
		t.Fatal(err)
	}
	got, err := loadNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || got["/projects/payments"] != "Payments" || got["ssh://production"] != "Production" {
		t.Fatalf("workspace names = %#v, want %#v", got, want)
	}
	info, err := os.Stat(filepath.Join(home, ".config", "kesh", "names.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("workspace name permissions are %o, want 600", info.Mode().Perm())
	}
}

func TestSessionRenamePersistsAliasAndEmptyNameResetsIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	e := entry{key: "workspace:payments", name: "payments", originalName: "payments", kind: "workspace", path: "/projects/payments"}
	selected := row{entryIndex: 0, tabIndex: -1, windowIndex: -1}
	m := model{entries: []entry{e}, rows: []row{selected}, names: nameStore{}}

	msg := runRename("", e, selected, "  Payments  ", m.names)().(renameMsg)
	updated, _ := m.Update(msg)
	m = updated.(model)
	if m.entries[0].name != "Payments" || m.names[e.key] != "Payments" {
		t.Fatalf("renamed model = %#v, names = %#v", m.entries[0], m.names)
	}
	stored, err := loadNames()
	if err != nil || stored[e.key] != "Payments" {
		t.Fatalf("stored names = %#v, err = %v", stored, err)
	}

	msg = runRename("", m.entries[0], selected, "", m.names)().(renameMsg)
	updated, _ = m.Update(msg)
	m = updated.(model)
	if m.entries[0].name != "payments" {
		t.Fatalf("reset name = %q, want payments", m.entries[0].name)
	}
	stored, err = loadNames()
	if err != nil || len(stored) != 0 {
		t.Fatalf("stored names after reset = %#v, err = %v", stored, err)
	}
}

func TestSessionRowRenameUsesSessionAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	e := entry{key: "/projects/payments", name: "payments", originalName: "payments", kind: "project", path: "/projects/payments", session: "kesh-payments", open: true}
	selected := row{entryIndex: 0, tabIndex: -1, windowIndex: -1}
	m := model{entries: []entry{e}, rows: []row{selected}, names: nameStore{}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = updated.(model)
	if m.mode != modeRename || m.renameValue != "payments" {
		t.Fatalf("r on session row = mode %v, value %q; want rename mode with session name", m.mode, m.renameValue)
	}

	msg := runRename("", e, selected, "Billing", m.names)().(renameMsg)
	updated, _ = m.Update(msg)
	m = updated.(model)
	if m.entries[0].name != "Billing" || m.names["workspace:kesh-payments"] != "Billing" {
		t.Fatalf("renamed session = %#v, names = %#v", m.entries[0], m.names)
	}
	stored, err := loadNames()
	if err != nil || stored["workspace:kesh-payments"] != "Billing" {
		t.Fatalf("stored session alias = %#v, err = %v", stored, err)
	}
}

func TestWorkspaceSearchMatchesOriginalNameAfterRename(t *testing.T) {
	m := model{
		query: "pymnts",
		entries: []entry{{
			key: "workspace:payments", name: "Billing", originalName: "payments", detail: "/projects/payments", kind: "workspace",
		}},
	}
	m.rebuildRows()
	if len(m.rows) != 1 {
		t.Fatalf("original workspace name search returned %d rows, want 1", len(m.rows))
	}
}

func TestSearchRanksOpenWorkspacesAboveUnopenedProjects(t *testing.T) {
	m := model{
		query: "flux",
		entries: []entry{
			{key: "/projects/flux", name: "flux", kind: "project"},
			{key: "workspace:flux", name: "flux service", kind: "workspace", open: true},
		},
	}
	m.rebuildRows()
	if len(m.rows) != 2 {
		t.Fatalf("search returned %d rows, want 2", len(m.rows))
	}
	first := m.entries[m.rows[0].entryIndex]
	if !first.open || first.kind != "workspace" {
		t.Fatalf("first search result = %#v, want open workspace", first)
	}
}

func TestSearchRanksSavedWorkspacesAboveSourceProjects(t *testing.T) {
	m := model{
		query: "ksm",
		entries: []entry{
			{key: "/projects/ksm", name: "ksm", kind: "project"},
			{key: "workspace:ksm", name: "ksm workspace", kind: "workspace", saved: true},
		},
	}
	m.rebuildRows()
	if len(m.rows) != 2 {
		t.Fatalf("search returned %d rows, want 2", len(m.rows))
	}
	first := m.entries[m.rows[0].entryIndex]
	if !first.saved || first.kind != "workspace" {
		t.Fatalf("first search result = %#v, want saved workspace", first)
	}
}

func TestCloseArgsTargetsSelectedHierarchyLevel(t *testing.T) {
	e := entry{
		name: "Payments",
		tabs: []tabItem{
			{id: 12, windows: []windowItem{{id: 120}, {id: 121}}},
			{id: 14, windows: []windowItem{{id: 140}}},
		},
	}
	tests := []struct {
		name     string
		selected row
		want     []string
	}{
		{
			name:     "workspace",
			selected: row{entryIndex: 0, tabIndex: -1, windowIndex: -1},
			want:     []string{"@", "close-tab", "--match", "id:12 or id:14"},
		},
		{
			name:     "tab",
			selected: row{entryIndex: 0, tabIndex: 1, windowIndex: -1},
			want:     []string{"@", "close-tab", "--match", "id:14"},
		},
		{
			name:     "window",
			selected: row{entryIndex: 0, tabIndex: 0, windowIndex: 1},
			want:     []string{"@", "close-window", "--match", "id:121"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := closeArgs(e, test.selected)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("closeArgs() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestComposedSessionContentCreatesOneTabPerEntry(t *testing.T) {
	t.Setenv("HOME", "/Users/stan")
	content := composedSessionContent("release", []entry{
		{key: "/projects/api", name: "API", kind: "project"},
		{key: "ssh://production", name: "production", kind: "ssh"},
	})
	want := "os_window_title release\nlayout splits\n" +
		"new_tab API\ncd /projects/api\nlaunch --title \"API\"\n" +
		"new_tab production\ncd /Users/stan\nlaunch --title \"ssh: production\" ssh \"production\"\n" +
		"focus\nfocus_os_window\n"
	if content != want {
		t.Fatalf("composedSessionContent() = %q, want %q", content, want)
	}
}

func TestComposedSessionName(t *testing.T) {
	if name, ok := composedSessionName("kesh-release"); !ok || name != "release" {
		t.Fatalf("composedSessionName() = (%q, %v), want (release, true)", name, ok)
	}
	if _, ok := composedSessionName("dotfiles"); ok {
		t.Fatal("ordinary session was identified as composed")
	}
}

func TestWorkspaceAndProjectFiltersUseSeparateEntries(t *testing.T) {
	entries := []entry{
		{key: "workspace:aurora", name: "aurora | frontier", kind: "workspace", open: true},
		{key: "/projects/aurora", name: "aurora", kind: "project"},
	}
	m := model{entries: entries, filter: filterOpen}
	m.rebuildRows()
	if len(m.rows) != 1 || m.entries[m.rows[0].entryIndex].kind != "workspace" {
		t.Fatalf("open rows = %#v, want workspace only", m.rows)
	}
	m.filter = filterProjects
	m.rebuildRows()
	if len(m.rows) != 1 || m.entries[m.rows[0].entryIndex].kind != "project" {
		t.Fatalf("project rows = %#v, want source project only", m.rows)
	}
}

func TestSavedFilterShowsOnlySavedEntries(t *testing.T) {
	entries := []entry{
		{key: "workspace:aurora", name: "aurora", kind: "workspace", saved: true},
		{key: "workspace:frontier", name: "frontier", kind: "workspace", saved: true, open: true},
		{key: "/projects/drafts", name: "drafts", kind: "project"},
	}
	m := model{entries: entries, filter: filterSaved}
	m.rebuildRows()
	if len(m.rows) != 2 {
		t.Fatalf("saved rows = %#v, want the 2 saved entries", m.rows)
	}
	for _, r := range m.rows {
		if !m.entries[r.entryIndex].saved {
			t.Fatalf("non-saved entry leaked into saved filter: %#v", m.entries[r.entryIndex])
		}
	}
	// Saved includes open saved sessions, mirroring how SSH ignores open state.
	sawOpen := false
	for _, r := range m.rows {
		if m.entries[r.entryIndex].open {
			sawOpen = true
		}
	}
	if !sawOpen {
		t.Fatalf("expected the open saved session to remain visible under Saved")
	}
}

func TestRowIconsDescribeEntryType(t *testing.T) {
	tests := []struct {
		name  string
		entry entry
		want  []string
	}{
		{name: "local folder", entry: entry{name: "dotfiles", kind: "project", open: true}, want: []string{""}},
		{name: "open composed session", entry: entry{name: "sideview-mmbot", kind: "workspace", open: true}, want: []string{""}},
		{name: "closed composed session", entry: entry{name: "sideview-mmbot", kind: "workspace", saved: true}, want: []string{""}},
		{name: "SSH", entry: entry{name: "hermes", kind: "ssh"}, want: []string{""}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := model{entries: []entry{test.entry}}
			rendered := ansi.Strip(m.renderRow(row{entryIndex: 0, tabIndex: -1, windowIndex: -1}, 100, false))
			for _, icon := range test.want {
				if !strings.Contains(rendered, icon) {
					t.Fatalf("rendered row %q does not contain %q", rendered, icon)
				}
			}
		})
	}
}

func TestSearchRanksExactProjectNameFirst(t *testing.T) {
	m := model{
		query: "crm",
		entries: []entry{
			{key: "/workspace/customer-relations-manager", name: "customer-relations-manager", detail: "/workspace/customer-relations-manager"},
			{key: "/workspace/crm", name: "crm", detail: "/workspace/crm"},
		},
	}
	m.rebuildRows()
	if len(m.rows) != 2 || m.entries[m.rows[0].entryIndex].name != "crm" {
		t.Fatalf("ranked rows = %#v, want crm first", m.rows)
	}
}

func TestSlashSearchReturnsToCommandsForSelectionAndCreation(t *testing.T) {
	m := model{entries: []entry{
		{key: "/projects/java", name: "java", kind: "project"},
		{key: "/projects/javascript", name: "javascript", kind: "project"},
	}}
	m.rebuildRows()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(model)
	if m.mode == modeSearch || m.cursor != 1 {
		t.Fatalf("j should navigate in command mode: searching=%v cursor=%d", m.mode == modeSearch, m.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(model)
	if m.mode != modeSearch || m.query != "j" || len(m.rows) != 2 {
		t.Fatalf("slash search state: searching=%v query=%q rows=%d", m.mode == modeSearch, m.query, len(m.rows))
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = updated.(model)
	if m.cursor != 0 {
		t.Fatalf("ctrl+k moved cursor to %d, want 0", m.cursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if m.mode == modeSearch || m.query != "j" {
		t.Fatalf("esc did not retain the filter in command mode: searching=%v query=%q", m.mode == modeSearch, m.query)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(model)
	if cmd != nil {
		t.Fatal("esc in command mode should not close Kesh")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if len(m.selected) != 1 || m.mode != modeCreateSession {
		t.Fatalf("command mode actions failed after search: selected=%#v creating=%v", m.selected, m.mode == modeCreateSession)
	}
}

func TestWorkspaceCannotBeSelectedAsProjectSource(t *testing.T) {
	m := model{
		entries: []entry{{key: "workspace:aurora", name: "aurora | frontier", kind: "workspace", open: true}},
		rows:    []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1}},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	if len(m.selected) != 0 || m.err == nil || !strings.Contains(m.err.Error(), "source project") {
		t.Fatalf("workspace selection state: selected=%#v err=%v", m.selected, m.err)
	}
}

func TestSelectedHeaderIncludesCountAndProjectName(t *testing.T) {
	m := model{
		width: 140, height: 30,
		entries:  []entry{{key: "/projects/api", name: "API"}},
		selected: map[string]bool{"/projects/api": true},
	}
	m.rebuildRows()
	view := m.View()
	if !strings.Contains(view, "Selected (1): API") || strings.Contains(view, "Selected: 1") {
		t.Fatalf("selected header does not include count and name:\n%s", view)
	}
}

func TestSpaceTogglesTopLevelSelection(t *testing.T) {
	m := model{entries: []entry{{key: "/projects/api", name: "API", kind: "project"}}, rows: []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1}}}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	if !m.selected["/projects/api"] {
		t.Fatalf("space did not select the project: %#v", m.selected)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(model)
	if m.mode != modeCreateSession {
		t.Fatal("n did not open the create-session prompt")
	}
	m.cancelMode()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = updated.(model)
	if len(m.selected) != 0 {
		t.Fatalf("space did not clear the project selection: %#v", m.selected)
	}
}

func TestCloseRequiresConfirmationAndRejectsInactiveWorkspace(t *testing.T) {
	selected := row{entryIndex: 0, tabIndex: -1, windowIndex: -1}
	m := model{
		entries: []entry{{name: "Payments", tabs: []tabItem{{id: 12}}}},
		rows:    []row{selected},
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(model)
	if m.mode != modeCloseConfirm || m.closeBusy || cmd != nil {
		t.Fatalf("first x should open confirmation: closing=%v busy=%v cmd=%v", m.mode == modeCloseConfirm, m.closeBusy, cmd)
	}
	if popup := m.popupView(80); !strings.Contains(popup, `Close workspace "Payments"`) || !strings.Contains(popup, "Press y to confirm") {
		t.Fatalf("close popup is missing confirmation details:\n%s", popup)
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(model)
	if m.mode != modeCloseConfirm || !m.closeBusy || cmd == nil {
		t.Fatalf("y should start closing: closing=%v busy=%v cmd=%v", m.mode == modeCloseConfirm, m.closeBusy, cmd)
	}

	inactive := model{entries: []entry{{name: "Payments"}}, rows: []row{selected}}
	updated, _ = inactive.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	inactive = updated.(model)
	if inactive.mode == modeCloseConfirm || inactive.err == nil || !strings.Contains(inactive.err.Error(), "not open") {
		t.Fatalf("inactive close state: mode=%v err=%v", inactive.mode, inactive.err)
	}
}

func worktreeSelectedTestRecipe(t *testing.T) *workspace.Config {
	t.Helper()
	var recipe workspace.Config
	if err := yaml.Unmarshal([]byte(strings.Join([]string{
		"workspace_mode: selected",
		"default_workspaces: [backend, worker]",
		"workspaces:",
		"  - name: backend",
		"  - name: frontend",
		"  - name: worker",
		"",
	}, "\n")), &recipe); err != nil {
		t.Fatalf("unmarshal recipe: %v", err)
	}
	return &recipe
}

func TestWorktreeTabCyclesPlainAndWorkspaces(t *testing.T) {
	recipe := worktreeSelectedTestRecipe(t)
	m := model{modeState: modeState{
		mode:               modeWorktreeCreate,
		worktreeCreateForm: &worktreeCreateForm{worktreeRecipe: recipe, worktreeRecipeMode: "none"},
	}}
	m.ensureWorktreeSelection()

	// Plain -> Workspaces. With >1 workspace the checklist is editable and
	// defaults honor workspace_mode "selected" + default_workspaces.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = updated.(model)
	if m.worktreeRecipeMode != "selected" || !m.worktreeCustomWorkspaces {
		t.Fatalf("expected Workspaces mode, got mode=%q custom=%v", m.worktreeRecipeMode, m.worktreeCustomWorkspaces)
	}
	if names := m.selectedWorkspaceNames(); !reflect.DeepEqual(names, []string{"backend", "worker"}) {
		t.Fatalf("Workspaces defaults = %v, want [backend worker]", names)
	}

	// Workspaces -> Plain.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = updated.(model)
	if m.worktreeRecipeMode != "none" {
		t.Fatalf("expected Plain after Shift+Tab, got %q", m.worktreeRecipeMode)
	}
}

func TestWorktreeSelectedSpaceAndEnterGuard(t *testing.T) {
	recipe := worktreeSelectedTestRecipe(t)
	m := model{modeState: modeState{
		mode: modeWorktreeCreate,
		worktreeCreateForm: &worktreeCreateForm{
			worktreeRecipe: recipe, worktreeRecipeMode: "selected",
			worktreeCustomWorkspaces: true, worktreeBranch: "feat/x",
		},
	}}
	m.ensureWorktreeSelection()
	// Defaults: [backend on, frontend off, worker on]. Cursor at backend; toggle it off.
	if !reflect.DeepEqual(m.selectedWorkspaceNames(), []string{"backend", "worker"}) {
		t.Fatalf("defaults = %v", m.selectedWorkspaceNames())
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	m = updated.(model)
	if m.worktreeSelected[0] {
		t.Fatalf("expected backend toggled off")
	}
	// Ctrl+J/K move the editable workspace cursor without consuming branch text.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = updated.(model)
	if m.worktreeWorkspaceCursor != 1 {
		t.Fatalf("cursor after Ctrl+J = %d, want 1", m.worktreeWorkspaceCursor)
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	m = updated.(model)
	if m.worktreeWorkspaceCursor != 0 {
		t.Fatalf("cursor after Ctrl+K = %d, want 0", m.worktreeWorkspaceCursor)
	}
	// Deselect worker too so nothing remains, then Enter must guard without exec.
	m.worktreeSelected[2] = false
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.err == nil || !strings.Contains(m.err.Error(), "select at least one workspace") {
		t.Fatalf("expected guard error, got err=%v busy=%v", m.err, m.worktreeBusy)
	}
	if m.worktreeBusy {
		t.Fatalf("Enter should not start creation with no selection")
	}
}

func TestLaunchLayoutGuardsEmptyWorkspaceSelection(t *testing.T) {
	recipe := worktreeSelectedTestRecipe(t)
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries:                  []entry{{key: repo, name: "repo", path: repo, kind: "project"}},
		modeState: modeState{
			mode: modeWorktreeCreate,
			worktreeCreateForm: &worktreeCreateForm{
				launchOnFolder:           true,
				worktreeRecipe:           recipe,
				worktreeRecipeMode:       "selected",
				worktreeCustomWorkspaces: true,
			},
		},
	}
	m.ensureWorktreeSelection()
	// Toggle every workspace off; Enter must guard without launching.
	for i := range m.worktreeSelected {
		m.worktreeSelected[i] = false
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(model)
	if m.err == nil || !strings.Contains(m.err.Error(), "select at least one workspace") {
		t.Fatalf("expected guard error, got err=%v", m.err)
	}
	if m.worktreeBusy {
		t.Fatalf("Enter should not launch with no selection")
	}
	if cmd != nil {
		t.Fatalf("no command should dispatch on empty selection, got %v", cmd)
	}
}

func TestWktreeLayoutPreviewModes(t *testing.T) {
	recipe := worktreeSelectedTestRecipe(t)
	selected := []bool{true, false, true}
	sel := strings.Join(layoutPreview(recipe, "/repo/.kesh.yaml", "selected", 60, selected), "\n")
	if !strings.Contains(sel, "backend") || !strings.Contains(sel, "worker") || strings.Contains(sel, "frontend") {
		t.Fatalf("selected preview = %q", sel)
	}
	single := strings.Join(layoutPreview(recipe, "/repo/.kesh.yaml", "single", 60, nil), "\n")
	if !strings.Contains(single, "backend") || strings.Contains(single, "worker") || strings.Contains(single, "frontend") {
		t.Fatalf("single preview = %q", single)
	}
	all := strings.Join(layoutPreview(recipe, "/repo/.kesh.yaml", "all", 60, nil), "\n")
	for _, name := range []string{"backend", "frontend", "worker"} {
		if !strings.Contains(all, name) {
			t.Fatalf("all preview missing %s: %q", name, all)
		}
	}
}

// writeBin writes an executable shell script into dir. The body is appended to
// "#!/bin/sh\n" so callers can focus on the script logic.
func writeBin(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
}

// TestWorktreeTabXConfirmsAndTargetsSelectedWorktree locks in the fix for the
// critical bug where `x` in the Worktree tab force-removed entries[0]'s first
// worktree with no confirmation. It must now open the confirm popup and remove
// the worktree under the cursor — even when the tab list is a reordered subset
// that does not start at the entry's first worktree.
func TestWorktreeTabXConfirmsAndTargetsSelectedWorktree(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", directory)
	removeCapture := filepath.Join(directory, "removed")
	t.Setenv("REMOVE_CAPTURE", removeCapture)
	writeBin(t, directory, "git", `case "$*" in
  *"worktree remove"*) printf '%s\n' "$*" >> "$REMOVE_CAPTURE"; exit 0 ;;
  *"worktree list --porcelain"*) printf 'worktree %s/repo\nHEAD aaa\nbranch refs/heads/main\n' "$TMPDIR" ;;
  *) exit 0 ;;
esac
`)
	writeBin(t, directory, "kitty", `case "$*" in
  *"@ ls"*) printf '[]\n' ;;
  *) exit 0 ;;
esac
`)

	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		kitty:                    filepath.Join(directory, "kitty"),
		entries: []entry{{
			name:            "repo",
			kind:            "project",
			path:            "/projects/repo",
			worktreesLoaded: true,
			worktrees: []worktreeItem{
				{path: "/wt/main", branch: "main"},
				{path: "/wt/feat", branch: "feat/x"},
			},
		}},
		// Reordered subset: the cursor lands on feat/x, which is NOT the
		// entry's first worktree (main). Pre-fix, removal targeted main.
		worktreeFilterRows: []worktreeFilterRow{
			{worktree: worktreeItem{path: "/wt/feat", branch: "feat/x"}},
			{worktree: worktreeItem{path: "/wt/main", branch: "main"}},
		},
		cursor: 0,
	}
	m.rows = []row{
		{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0},
		{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 1},
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	tab := updated.(model)
	if cmd != nil {
		t.Fatalf("x should open the confirm popup, not remove immediately")
	}
	if tab.mode != modeCloseConfirm {
		t.Fatal("x in the Worktree tab should open the confirm popup")
	}
	if tab.closeRow.section != "wt-filter" || tab.closeRow.worktreePath != "/wt/feat" {
		t.Fatalf("closeRow should target the selected worktree by path, got %#v", tab.closeRow)
	}
	// The popup must describe the selected worktree, not the entry's first one.
	if prompt := tab.closePrompt(); !strings.Contains(prompt, "feat/x") || strings.Contains(prompt, "main") {
		t.Fatalf("close prompt should name feat/x only:\n%s", prompt)
	}

	// Confirming runs the removal; the recorded git invocation must target feat/x.
	_, confirmCmd := tab.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if confirmCmd == nil {
		t.Fatal("confirming should start the removal")
	}
	if msg := confirmCmd().(worktreeRemoveMsg); msg.err != nil {
		t.Fatalf("removal failed: %v", msg.err)
	}
	content, err := os.ReadFile(removeCapture)
	if err != nil {
		t.Fatalf("worktree remove was not invoked: %v", err)
	}
	if !strings.Contains(string(content), "/wt/feat") {
		t.Fatalf("removed the wrong worktree:\n%s", content)
	}
	if strings.Contains(string(content), "/wt/main") {
		t.Fatalf("removed the entry's first worktree instead of the selected one:\n%s", content)
	}
}

// TestWorktreeTabEnterOpensWorktreeNotProject locks in the fix for the critical
// bug where `enter` in the Worktree tab opened the project's main checkout
// because runAction ignored the row's worktreePath.
func TestWorktreeTabEnterOpensWorktreeNotProject(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", directory)
	writeBin(t, directory, "kitty", `case "$*" in
  *"@ ls"*) printf '[]\n' ;;
  *) exit 0 ;;
esac
`)
	writeBin(t, directory, "zoxide", "exit 0\n")

	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		kitty:                    filepath.Join(directory, "kitty"),
		zoxide:                   filepath.Join(directory, "zoxide"),
		entries: []entry{{
			name: "repo", kind: "project", path: "/projects/repo", key: "/projects/repo",
			worktreesLoaded: true,
			worktrees:       []worktreeItem{{path: "/wt/feat", branch: "feat/x"}},
		}},
		worktreeFilterRows: []worktreeFilterRow{{worktree: worktreeItem{path: "/wt/feat", branch: "feat/x"}}},
		cursor:             0,
	}
	m.rows = []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0}}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should open the worktree")
	}
	lookedUp, actionCmd := m.Update(cmd())
	if actionCmd == nil {
		t.Fatal("worktree lookup did not schedule an open action")
	}
	if msg := actionCmd().(actionMsg); msg.err != nil {
		t.Fatalf("enter failed: %v", msg.err)
	}
	_ = lookedUp

	sessionFile := filepath.Join(directory, "kitty-zoxide-sessions", "feat.kitty-session")
	content, err := os.ReadFile(sessionFile)
	if err != nil {
		t.Fatalf("worktree session was not written: %v", err)
	}
	if !strings.Contains(string(content), "cd /wt/feat") {
		t.Fatalf("enter should cd into the worktree, got:\n%s", content)
	}
	if strings.Contains(string(content), "/projects/repo") {
		t.Fatalf("enter opened the project checkout instead of the worktree:\n%s", content)
	}
}

// TestWorktreeTabDTargetsSelectedWorktreeNotProject locks in the fix for the
// critical bug where `D` in the Worktree tab planned full entry destruction
// because the wt-filter section was not handled.
func TestWorktreeTabDTargetsSelectedWorktreeNotProject(t *testing.T) {
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries: []entry{{
			name:            "repo",
			kind:            "project",
			path:            "/projects/repo",
			open:            true,
			tabs:            []tabItem{{}}, // an open session: full-entry destroy would close it
			worktreesLoaded: true,
			worktrees: []worktreeItem{
				{path: "/wt/main", branch: "main"},
				{path: "/wt/feat", branch: "feat/x"},
			},
		}},
		worktreeFilterRows: []worktreeFilterRow{
			{worktree: worktreeItem{path: "/wt/feat", branch: "feat/x"}},
		},
		cursor: 0,
	}
	m.rows = []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	tab := updated.(model)
	if tab.destroyPlan == nil {
		t.Fatal("D should build a destroy plan")
	}
	if tab.destroyPlan.closeSession {
		t.Fatalf("D in the Worktree tab must not destroy the whole project session: %#v", tab.destroyPlan)
	}
	if tab.destroyPlan.worktreePath != "/wt/feat" {
		t.Fatalf("D should target the selected worktree, got %q", tab.destroyPlan.worktreePath)
	}
	if tab.destroyPlan.branch != "feat/x" {
		t.Fatalf("D should target the feat/x branch, got %q", tab.destroyPlan.branch)
	}
	if tab.closeRow.worktreePath != "/wt/feat" || tab.closeRow.section != "wt-filter" {
		t.Fatalf("closeRow should address the worktree: %#v", tab.closeRow)
	}
}

// TestWorktreeTabFindMergedForOpenProject locks in the fix for the critical bug
// where `X` (remove merged) errored for open projects in the Worktree tab
// because worktreeDirectory could not resolve an open entry.
func TestWorktreeTabFindMergedForOpenProject(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", directory)
	repo := filepath.Join(directory, "repo")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	removeCapture := filepath.Join(directory, "removed")
	t.Setenv("REMOVE_CAPTURE", removeCapture)
	writeBin(t, directory, "git", `case "$*" in
  *"worktree remove"*) printf '%s\n' "$*" >> "$REMOVE_CAPTURE"; exit 0 ;;
  *"worktree list --porcelain"*) printf 'worktree /repo\nHEAD aaa\nbranch refs/heads/main\n\nworktree /feat\nHEAD bbb\nbranch refs/heads/feat/x\n' ;;
  *"branch --merged"*) printf 'main\nfeat/x\n' ;;
  *"branch --show-current"*) printf 'main\n' ;;
  *) exit 0 ;;
esac
`)
	writeBin(t, directory, "gh", "printf '[]\\n'\n")

	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries: []entry{{
			name: "repo", kind: "project", path: repo, open: true,
		}},
		worktreeFilterRows: []worktreeFilterRow{{worktree: worktreeItem{path: filepath.Join(directory, "feat"), branch: "feat/x"}}},
		cursor:             0,
	}
	m.rows = []row{{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0}}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	tab := updated.(model)
	if tab.err != nil {
		t.Fatalf("X should not error for an open project in the tab: %v", tab.err)
	}
	if !tab.mergedWorktreeBusy {
		t.Fatal("X should start the merged-worktree scan")
	}
	if cmd == nil {
		t.Fatal("X should return a scan command")
	}
	msg := cmd().(mergedWorktreeListMsg)
	if msg.err != nil {
		t.Fatalf("merged scan failed: %v", msg.err)
	}
	if len(msg.worktrees) != 1 || msg.worktrees[0].branch != "feat/x" {
		t.Fatalf("merged worktrees = %#v, want [feat/x]", msg.worktrees)
	}

	// The scan opens the confirm popup; confirming must remove the merged
	// worktree from the tab's project (worktreeDirectory resolves the open
	// entry's repo), not fail with an empty -C dir.
	popupModel, _ := tab.Update(msg)
	_, removeCmd := popupModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if removeCmd == nil {
		t.Fatal("confirming merged removal should start the removal")
	}
	if removeMsg := removeCmd().(mergedWorktreeRemoveMsg); removeMsg.err != nil {
		t.Fatalf("merged removal failed: %v", removeMsg.err)
	}
	content, err := os.ReadFile(removeCapture)
	if err != nil {
		t.Fatalf("merged worktree remove was not invoked: %v", err)
	}
	if !strings.Contains(string(content), "/feat") {
		t.Fatalf("merged removal did not target the feat/x worktree:\n%s", content)
	}
}

func TestWorktreeTabNotableBugFixes(t *testing.T) {
	t.Run("shift-tab moves back one filter", func(t *testing.T) {
		m := model{filter: filterAll}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
		// Worktrees is no longer a cycle filter, so wrapping backwards from All
		// lands on the last flat filter (Saved), not on Worktrees.
		if got := updated.(model).filter; got != filterSaved {
			t.Fatalf("shift-tab filter = %d, want %d", got, filterSaved)
		}
	})

	t.Run("rebuild preserves the focused worktree", func(t *testing.T) {
		m := model{
			filter: filterWorktrees, worktreeFilterEntryIndex: 0, cursor: 1,
			entries:            []entry{{worktreesLoaded: true, worktrees: []worktreeItem{{path: "/wt/main", branch: "main"}, {path: "/wt/feature", branch: "feature"}}}},
			worktreeFilterRows: []worktreeFilterRow{{worktree: worktreeItem{path: "/wt/main"}}, {worktree: worktreeItem{path: "/wt/feature"}}},
			rows:               []row{{section: "wt-filter", wt: 0}, {section: "wt-filter", wt: 1}},
		}
		m.rebuildWorktreeRows()
		if got := m.focusedWorktreePath(); got != "/wt/feature" {
			t.Fatalf("focused worktree = %q, want /wt/feature", got)
		}
	})

	t.Run("escape clears selections before leaving the tab", func(t *testing.T) {
		m := model{filter: filterWorktrees, previousFilter: filterAll, selected: map[string]bool{"repo": true}}
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
		result := updated.(model)
		if result.filter != filterAll || len(result.selected) != 0 {
			t.Fatalf("escape result = filter %d, selected %#v", result.filter, result.selected)
		}
	})

	t.Run("fetch failures are returned while reload continues", func(t *testing.T) {
		directory := t.TempDir()
		writeBin(t, directory, "git", "echo offline >&2\nexit 1\n")
		t.Setenv("PATH", directory)
		msg := fetchOriginThenReload("/repo", 3)().(worktreeFetchedMsg)
		if msg.err == nil || msg.dir != "/repo" || msg.entryIndex != 3 {
			t.Fatalf("fetch result = %#v", msg)
		}
	})
}

// TestOpenURLPlatformAware locks in the fix for the critical bug where the PR
// opener was platform-wrong: the Worktree tab hardcoded xdg-open (broken on
// macOS) and main mode hardcoded open (missing on Linux).
func TestOpenURLPlatformAware(t *testing.T) {
	t.Run("errors when no opener is installed", func(t *testing.T) {
		directory := t.TempDir()
		t.Setenv("PATH", directory) // no openers present
		if err := openURL("https://example.com"); err == nil {
			t.Fatal("openURL should error when no opener is on PATH")
		}
	})

	t.Run("opens via the available opener", func(t *testing.T) {
		directory := t.TempDir()
		t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
		capture := filepath.Join(directory, "opened")
		for _, name := range []string{"open", "xdg-open"} {
			writeBin(t, directory, name, fmt.Sprintf("printf '%%s' \"$1\" >> %q\n", capture))
		}
		if err := openURL("https://example.com/pr/1"); err != nil {
			t.Fatal(err)
		}
		content, err := os.ReadFile(capture)
		if err != nil {
			t.Fatalf("no opener was invoked: %v", err)
		}
		if got := strings.TrimSpace(string(content)); got != "https://example.com/pr/1" {
			t.Fatalf("opened URL = %q", got)
		}
	})

	t.Run("prefers the platform default", func(t *testing.T) {
		directory := t.TempDir()
		t.Setenv("PATH", directory)
		for _, name := range []string{"open", "xdg-open"} {
			writeBin(t, directory, name, "exit 0\n")
		}
		preferred := "xdg-open"
		if runtime.GOOS == "darwin" {
			preferred = "open"
		}
		if got := filepath.Base(openerCommand()); got != preferred {
			t.Fatalf("openerCommand = %q, want %s", got, preferred)
		}
	})
}

func TestWorktreeTabSpaceTogglesBulkSelection(t *testing.T) {
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries:                  []entry{{name: "repo", kind: "project", path: "/p/repo", worktrees: []worktreeItem{{path: "/wt/a", branch: "a"}, {path: "/wt/b", branch: "b"}}}},
		worktreeFilterRows: []worktreeFilterRow{
			{worktree: worktreeItem{path: "/wt/a", branch: "a"}},
			{worktree: worktreeItem{path: "/wt/b", branch: "b"}},
		},
	}
	m.rows = []row{
		{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 0},
		{entryIndex: 0, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: 1},
	}

	// space on the first row selects it.
	m.toggleSelected()
	if !m.wtBulkSelected["/wt/a"] || len(m.wtBulkSelected) != 1 {
		t.Fatalf("expected /wt/a selected, got %v", m.wtBulkSelected)
	}
	// space again deselects.
	m.toggleSelected()
	if len(m.wtBulkSelected) != 0 {
		t.Fatalf("expected selection cleared, got %v", m.wtBulkSelected)
	}
}

func TestSelectedWorktreeItemsResolvesFromFullWorktreeList(t *testing.T) {
	m := model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries: []entry{{name: "repo", kind: "project", path: "/p/repo", worktrees: []worktreeItem{
			{path: "/wt/a", branch: "a"},
			{path: "/wt/b", branch: "b"},
			{path: "/wt/c", branch: "c"},
		}}},
		// The visible/filtered rows are a subset; selection should still resolve
		// every selected worktree from the project's full list.
		worktreeFilterRows: []worktreeFilterRow{{worktree: worktreeItem{path: "/wt/a", branch: "a"}}},
		wtBulkSelected:     map[string]bool{"/wt/a": true, "/wt/c": true, "/wt/gone": true},
	}
	items := m.selectedWorktreeItems()
	if len(items) != 2 {
		t.Fatalf("expected 2 live items, got %d: %+v", len(items), items)
	}
	got := map[string]bool{}
	for _, item := range items {
		got[item.path] = true
	}
	if !got["/wt/a"] || !got["/wt/c"] {
		t.Fatalf("expected /wt/a and /wt/c, got %v", got)
	}
}

func TestRebuildWorktreeRowsPrunesStaleBulkSelections(t *testing.T) {
	m := &model{
		filter:                   filterWorktrees,
		worktreeFilterEntryIndex: 0,
		entries: []entry{{name: "repo", kind: "project", path: "/p/repo", worktreesLoaded: true, worktrees: []worktreeItem{
			{path: "/wt/a", branch: "a"},
			{path: "/wt/b", branch: "b"},
		}}},
		wtBulkSelected: map[string]bool{"/wt/a": true, "/wt/removed": true},
	}
	m.rebuildWorktreeRows()
	if m.wtBulkSelected["/wt/removed"] {
		t.Fatalf("stale selection should have been pruned, got %v", m.wtBulkSelected)
	}
	if !m.wtBulkSelected["/wt/a"] {
		t.Fatalf("live selection should have been kept, got %v", m.wtBulkSelected)
	}
}

func TestListPageSizeIsPositiveAndBoundedByHeight(t *testing.T) {
	m := model{height: 24}
	if size := m.listPageSize(); size < 1 {
		t.Fatalf("listPageSize = %d, want >= 1", size)
	}
	// Tiny terminal must still return at least 1.
	m.height = 4
	if size := m.listPageSize(); size < 1 {
		t.Fatalf("listPageSize at height 4 = %d, want >= 1", size)
	}
}

func TestToggleExpandFromSessionRowTogglesOnlyThatSession(t *testing.T) {
	m := &model{
		filter: filterAll,
		entries: []entry{
			{name: "a", tabs: []tabItem{{title: "t1", windows: []windowItem{{title: "w"}}}}},
			{name: "b", tabs: []tabItem{{title: "t2", windows: []windowItem{{title: "w"}}}}},
		},
	}
	m.rebuildRows()
	// Cursor on session "a" (the first row). It starts collapsed → expand its subtree.
	m.cursor = 0
	m.toggleExpandAtCursor()
	if !m.entries[0].expanded || !m.entries[0].tabs[0].expanded {
		t.Fatalf("expected session a subtree expanded, got entry=%v tab=%v", m.entries[0].expanded, m.entries[0].tabs[0].expanded)
	}
	// Session b is untouched.
	if m.entries[1].expanded {
		t.Fatal("session b should not have been affected by a toggle on session a")
	}
	// Press again on session a: now fully expanded → collapse it.
	m.toggleExpandAtCursor()
	if m.entries[0].expanded {
		t.Fatal("expected session a collapsed after second toggle")
	}
	// Session b still untouched.
	if m.entries[1].expanded {
		t.Fatal("session b should still be untouched")
	}
}

func TestToggleExpandAllFromTabRowTogglesOnlyThatTab(t *testing.T) {
	m := &model{
		filter: filterAll,
		entries: []entry{
			{name: "a", expanded: true, tabs: []tabItem{
				{title: "t1", windows: []windowItem{{title: "w"}}},
				{title: "t2", windows: []windowItem{{title: "w"}}},
			}},
			{name: "b", expanded: false, tabs: []tabItem{{title: "t3", windows: []windowItem{{title: "w"}}}}},
		},
	}
	m.rebuildRows()
	// Put the cursor on session "a"'s first tab row.
	cursorOnTab := -1
	for i, r := range m.rows {
		if r.entryIndex == 0 && r.tabIndex == 0 && r.windowIndex < 0 {
			cursorOnTab = i
			break
		}
	}
	if cursorOnTab < 0 {
		t.Fatal("could not find tab row for entry 0")
	}
	m.cursor = cursorOnTab
	// From a tab row: toggle only that tab's windows. t1 collapsed → expand.
	m.toggleExpandAtCursor()
	if !m.entries[0].tabs[0].expanded {
		t.Fatal("expected tab t1 expanded")
	}
	// The sibling tab and the other session are untouched.
	if m.entries[0].tabs[1].expanded {
		t.Fatal("sibling tab t2 should not have been toggled")
	}
	if m.entries[1].expanded {
		t.Fatal("session b should not have been affected by a tab-row toggle in session a")
	}
	// Press again from the same tab row: t1 now expanded → collapse.
	m.toggleExpandAtCursor()
	if m.entries[0].tabs[0].expanded {
		t.Fatal("expected tab t1 collapsed after second toggle")
	}
	// The session itself stays expanded so the tabs remain visible.
	if !m.entries[0].expanded {
		t.Fatal("session a should remain expanded after a tab-row toggle")
	}
}

// TestTabNeverCyclesIntoWorktrees locks in the fix for the empty-by-default
// Worktrees tab: Worktrees is a project-scoped drill-in surface opened with w,
// not a flat filter, so cycling Tab/Shift-tab must never land on it. Reaching it
// by cycling used to render an empty list because no project was selected.
func TestTabNeverCyclesIntoWorktrees(t *testing.T) {
	m := model{
		filter: filterAll,
		entries: []entry{
			{name: "first", kind: "project", path: "/p/first"},
			{name: "second", kind: "project", path: "/p/second"},
		},
		selected:                 map[string]bool{},
		worktreeFilterEntryIndex: -1,
	}
	m.rebuildRows()
	// A full cycle of the flat filters never visits Worktrees.
	for i := 0; i < len(cycleFilters); i++ {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = updated.(model)
		if m.filter == filterWorktrees {
			t.Fatalf("Tab cycled into Worktrees after %d presses", i+1)
		}
	}
	if m.filter != filterAll {
		t.Fatalf("after a full cycle, filter = %d, want %d", m.filter, filterAll)
	}

	// Tab while already in Worktrees is a no-op (esc is the exit), so a session
	// scoped with w is not silently dropped by an stray Tab.
	scoped := model{
		filter:                   filterWorktrees,
		previousFilter:           filterAll,
		worktreeFilterEntryIndex: 0,
		entries:                  []entry{{name: "first", kind: "project", path: "/p/first"}},
	}
	updated, _ := scoped.Update(tea.KeyMsg{Type: tea.KeyTab})
	result := updated.(model)
	if result.filter != filterWorktrees || result.worktreeFilterEntryIndex != 0 {
		t.Fatalf("Tab inside Worktrees should be a no-op: filter=%d entry=%d", result.filter, result.worktreeFilterEntryIndex)
	}
}

func TestWOpensWorktreesWithSelectedProject(t *testing.T) {
	m := model{
		filter:   filterAll,
		entries:  []entry{{name: "repo", kind: "project", path: "/p/repo", worktreesLoaded: true}},
		selected: map[string]bool{},
	}
	m.rebuildRows()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	m = updated.(model)
	if m.filter != filterWorktrees {
		t.Fatalf("expected Worktrees filter, got %d", m.filter)
	}
	if m.worktreeFilterEntryIndex != 0 {
		t.Fatalf("expected worktreeFilterEntryIndex=0, got %d", m.worktreeFilterEntryIndex)
	}
}
