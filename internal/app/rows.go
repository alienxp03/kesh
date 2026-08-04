package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"

	"github.com/alienxp03/kesh/internal/catalog"
	"github.com/alienxp03/kesh/internal/domain"
	gitx "github.com/alienxp03/kesh/internal/git"
	kittyx "github.com/alienxp03/kesh/internal/kitty"
)

func (m *model) expandOrDescend() {
	if len(m.rows) == 0 {
		return
	}
	r := m.rows[m.cursor]
	e := &m.entries[r.entryIndex]
	if r.windowIndex >= 0 {
		return
	}
	if r.tabIndex >= 0 {
		tab := &e.tabs[r.tabIndex]
		if len(tab.windows) == 0 {
			return
		}
		if !tab.expanded {
			tab.expanded = true
			m.rebuildRows()
			return
		}
		if m.cursor+1 < len(m.rows) && m.rows[m.cursor+1].tabIndex == r.tabIndex {
			m.cursor++
		}
		return
	}
	if len(e.tabs) == 0 {
		return
	}
	if !e.expanded {
		e.expanded = true
		m.rebuildRows()
		return
	}
	if m.cursor+1 < len(m.rows) && m.rows[m.cursor+1].entryIndex == r.entryIndex {
		m.cursor++
	}
}

func (m *model) ascendOrCollapse() {
	if len(m.rows) == 0 {
		return
	}
	r := m.rows[m.cursor]
	e := &m.entries[r.entryIndex]
	if r.windowIndex >= 0 {
		for m.cursor > 0 {
			m.cursor--
			if m.rows[m.cursor].tabIndex == r.tabIndex && m.rows[m.cursor].windowIndex < 0 {
				break
			}
		}
		return
	}
	if r.tabIndex >= 0 {
		if e.tabs[r.tabIndex].expanded {
			e.tabs[r.tabIndex].expanded = false
			m.rebuildRows()
			return
		}
		for m.cursor > 0 {
			m.cursor--
			if m.rows[m.cursor].tabIndex < 0 {
				break
			}
		}
		return
	}
	if e.expanded {
		e.expanded = false
		m.rebuildRows()
	}
}

// toggleExpandAtCursor expands or collapses the cursor's node in one keystroke,
// so the user does not have to walk the tree node by node:
//
//   - session row → toggle that session's whole subtree (the entry plus all its
//     tabs). Other sessions are not touched.
//   - tab or window row → toggle only that tab's windows.
//
// For the session scope, if the entry or any of its tabs is collapsed, expand
// the entry and all its tabs; otherwise collapse the entry. Entries without
// tabs (closed/saved projects, SSH hosts) are untouched.
func (m *model) toggleExpandAtCursor() {
	if len(m.rows) == 0 {
		return
	}
	r := m.rows[m.cursor]
	if r.entryIndex < 0 || r.entryIndex >= len(m.entries) {
		return
	}
	e := &m.entries[r.entryIndex]
	if r.tabIndex >= 0 {
		// On a tab/window row: toggle only that tab's windows. The session is
		// already expanded (otherwise the tab would not be visible).
		if r.tabIndex >= len(e.tabs) {
			return
		}
		e.tabs[r.tabIndex].expanded = !e.tabs[r.tabIndex].expanded
		m.rebuildRows()
		return
	}
	// Session row: toggle only this session's subtree.
	if len(e.tabs) == 0 {
		return
	}
	anyCollapsed := !e.expanded
	for t := range e.tabs {
		if !e.tabs[t].expanded {
			anyCollapsed = true
		}
	}
	if anyCollapsed {
		e.expanded = true
		for t := range e.tabs {
			e.tabs[t].expanded = true
		}
	} else {
		e.expanded = false
	}
	m.rebuildRows()
}

// preserveExpandedState keeps hierarchy navigation stable when an operation
// reloads Kitty's entries. IDs are supplied by Kitty and remain stable across a
// refresh, while the entry structs themselves are rebuilt from scratch.
func preserveExpandedState(previous, refreshed []entry) {
	oldEntries := make(map[string]entry, len(previous))
	for _, entry := range previous {
		oldEntries[entry.key] = entry
	}
	for i := range refreshed {
		old, ok := oldEntries[refreshed[i].key]
		if !ok {
			continue
		}
		refreshed[i].expanded = old.expanded
		oldTabs := make(map[int]bool, len(old.tabs))
		for _, tab := range old.tabs {
			oldTabs[tab.id] = tab.expanded
		}
		for tabIndex := range refreshed[i].tabs {
			if expanded, ok := oldTabs[refreshed[i].tabs[tabIndex].id]; ok {
				refreshed[i].tabs[tabIndex].expanded = expanded
			}
		}
	}
}

func (m *model) rebuildRows() {
	if m.filter == filterAgents {
		m.rebuildAgentRows()
		return
	}
	if m.filter == filterWorktrees {
		m.rebuildWorktreeRows()
		return
	}
	entryIndexes := make([]int, 0, len(m.entries))
	searchValues := make([]string, 0, len(m.entries))
	for i := range m.entries {
		e := m.entries[i]
		if !m.matchesFilter(e) {
			continue
		}
		entryIndexes = append(entryIndexes, i)
		searchValues = append(searchValues, e.name+" "+e.originalName+" "+e.detail)
	}
	if m.query != "" {
		matches := fuzzy.Find(m.query, searchValues)
		ranked := make([]int, 0, len(matches))
		for _, match := range matches {
			ranked = append(ranked, entryIndexes[match.Index])
		}
		// Fuzzy relevance determines the order within each group. Live
		// workspaces rank first, followed by restorable saved workspaces, then
		// source projects and SSH hosts.
		priority := func(e entry) int {
			if e.open {
				return 0
			}
			if e.saved {
				return 1
			}
			return 2
		}
		sort.SliceStable(ranked, func(i, j int) bool {
			return priority(m.entries[ranked[i]]) < priority(m.entries[ranked[j]])
		})
		entryIndexes = ranked
	}
	// Keep the catalog's activity order intact. Pins are direct Kitty
	// shortcuts and remain marked in the list, but should not move a recent
	// project below an older pinned entry.

	var rows []row
	for _, entryIndex := range entryIndexes {
		e := &m.entries[entryIndex]
		rows = append(rows, row{entryIndex: entryIndex, tabIndex: -1, windowIndex: -1})
		if e.expanded && m.query == "" {
			for tabIndex := range e.tabs {
				rows = append(rows, row{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: -1})
				if e.tabs[tabIndex].expanded {
					for windowIndex := range e.tabs[tabIndex].windows {
						rows = append(rows, row{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex})
					}
				}
			}
		}
	}
	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = max(0, len(rows)-1)
	}
}

func (m model) selectedDetailPath() string {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	selected := m.rows[m.cursor]
	entry := m.entries[selected.entryIndex]
	if selected.windowIndex >= 0 {
		return entry.tabs[selected.tabIndex].windows[selected.windowIndex].cwd
	}
	if selected.tabIndex >= 0 {
		for _, window := range entry.tabs[selected.tabIndex].windows {
			if window.cwd != "" {
				return window.cwd
			}
		}
	}
	return entry.path
}

func (m *model) queuePathPR() tea.Cmd {
	path := m.selectedDetailPath()
	if path == "" {
		return nil
	}
	if m.pathPRChecked == nil {
		m.pathPRChecked = map[string]bool{}
	}
	if m.pathPRChecked[path] {
		return nil
	}
	m.pathPRChecked[path] = true
	return func() tea.Msg {
		return pathPRMsg{path: path, info: cachedPathPR(path)}
	}
}

func (m model) hasSelectedAgentWindow() bool {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return false
	}
	r := m.rows[m.cursor]
	return r.windowIndex >= 0 && m.entries[r.entryIndex].tabs[r.tabIndex].windows[r.windowIndex].agent != ""
}

func (m model) shouldRefreshPreview(windowID int) bool {
	if !m.showPreview || !m.hasSelectedAgentWindow() || m.previewID != windowID {
		return false
	}
	r := m.rows[m.cursor]
	return m.entries[r.entryIndex].tabs[r.tabIndex].windows[r.windowIndex].id == windowID
}

func (m model) queuePreviewRefresh(windowID int, request uint64) tea.Cmd {
	if request != m.previewRequest || !m.shouldRefreshPreview(windowID) {
		return nil
	}
	return tea.Tick(previewRefreshInterval, func(time.Time) tea.Msg {
		return previewRefreshMsg{windowID: windowID, request: request}
	})
}

func (m *model) queuePreview() tea.Cmd {
	commands := []tea.Cmd{m.queuePathPR()}
	if !m.showPreview || !m.hasSelectedAgentWindow() {
		if len(m.rows) == 0 {
			m.previewID = 0
			m.preview = ""
			m.previewErr = nil
			m.previewBusy = false
		}
		return tea.Batch(commands...)
	}
	r := m.rows[m.cursor]
	windowID := m.entries[r.entryIndex].tabs[r.tabIndex].windows[r.windowIndex].id
	if windowID == m.previewID {
		return tea.Batch(commands...)
	}
	m.previewID = windowID
	m.preview = ""
	m.previewErr = nil
	m.previewBusy = true
	m.previewRequest++
	commands = append(commands, fetchPreviewRequest(m.kitty, windowID, m.previewRequest))
	return tea.Batch(commands...)
}

func fetchPreview(kitty string, windowID int) tea.Cmd {
	return fetchPreviewRequest(kitty, windowID, 0)
}

func fetchPreviewRequest(kitty string, windowID int, request uint64) tea.Cmd {
	return func() tea.Msg {
		if kitty == "" {
			return previewMsg{windowID: windowID, request: request, err: fmt.Errorf("kitty was not found")}
		}
		output, err := (kittyx.Client{Executable: kitty}).CaptureScreen(windowID)
		content := cleanPreview(string(output))
		if err != nil {
			message := strings.TrimSpace(ansiPattern.ReplaceAllString(content, ""))
			if message != "" {
				err = fmt.Errorf("%s: %s", err, message)
			}
		}
		return previewMsg{windowID: windowID, request: request, content: content, err: err}
	}
}

func cleanPreview(content string) string {
	content = backgroundSGR.ReplaceAllString(content, "")
	lines := strings.Split(content, "\n")
	for len(lines) > 0 && strings.TrimSpace(ansiPattern.ReplaceAllString(lines[len(lines)-1], "")) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func (m *model) rebuildAgentRows() {
	rows := make([]row, 0)
	searchValues := make([]string, 0)
	seen := map[int]bool{}
	for entryIndex := range m.entries {
		e := m.entries[entryIndex]
		for tabIndex := range e.tabs {
			tab := e.tabs[tabIndex]
			for windowIndex := range tab.windows {
				window := tab.windows[windowIndex]
				if window.agent == "" || seen[window.id] {
					continue
				}
				seen[window.id] = true
				rows = append(rows, row{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex})
				searchValues = append(searchValues, strings.Join([]string{
					window.agent, e.name, e.originalName, e.detail, tab.title, window.title, window.command, window.detail,
				}, " "))
			}
		}
	}
	if m.query != "" {
		matches := fuzzy.Find(m.query, searchValues)
		ranked := make([]row, 0, len(matches))
		for _, match := range matches {
			ranked = append(ranked, rows[match.Index])
		}
		rows = ranked
	} else {
		sort.SliceStable(rows, func(i, j int) bool {
			a := m.entries[rows[i].entryIndex].tabs[rows[i].tabIndex].windows[rows[i].windowIndex]
			b := m.entries[rows[j].entryIndex].tabs[rows[j].tabIndex].windows[rows[j].windowIndex]
			return a.lastFocused > b.lastFocused
		})
	}
	m.rows = rows
	if m.cursor >= len(rows) {
		m.cursor = max(0, len(rows)-1)
	}
}

func (m *model) rebuildWorktreeRows() {
	focused := m.focusedWorktreePath()
	entryIndex := m.worktreeFilterEntryIndex
	if entryIndex < 0 || entryIndex >= len(m.entries) {
		m.worktreeFilterRows = []worktreeFilterRow{}
		m.rows = []row{}
		m.cursor = 0
		return
	}

	e := m.entries[entryIndex]
	rows := []worktreeFilterRow{}
	searchValues := []string{}

	// Get worktrees from entry level if available
	if e.worktreesLoaded {
		for _, wt := range e.worktrees {
			rows = append(rows, worktreeFilterRow{
				worktree:  wt,
				entryName: e.name,
				entryPath: e.path,
			})
			searchValues = append(searchValues, wt.branch+" "+wt.path+" "+wt.head)
		}
	} else {
		// Need to fetch worktrees - return early and let the fetch happen elsewhere
		return
	}

	if m.query != "" {
		matches := fuzzy.Find(m.query, searchValues)
		filtered := make([]worktreeFilterRow, 0, len(matches))
		for _, match := range matches {
			filtered = append(filtered, rows[match.Index])
		}
		rows = filtered
	} else {
		// Sort: current first, then by branch name
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i].worktree, rows[j].worktree
			if a.current != b.current {
				return a.current // current worktree first
			}
			return worktreePriority(a) < worktreePriority(b)
		})
	}

	m.worktreeFilterRows = rows

	// Drop bulk selections for worktrees that no longer exist (removed or
	// pruned since selection) so the header count and bulk actions stay honest.
	if len(m.wtBulkSelected) > 0 {
		live := map[string]bool{}
		for _, wt := range m.entries[entryIndex].worktrees {
			live[wt.path] = true
		}
		for path := range m.wtBulkSelected {
			if !live[path] {
				delete(m.wtBulkSelected, path)
			}
		}
		if len(m.wtBulkSelected) == 0 {
			m.wtBulkSelected = nil
		}
	}

	// Build regular rows for cursor tracking. wt indexes worktreeFilterRows so
	// each rendered row maps to its own worktree, not the focused one.
	m.rows = make([]row, len(rows))
	for i := range rows {
		m.rows[i] = row{entryIndex: entryIndex, tabIndex: -1, windowIndex: -1, section: "wt-filter", wt: i}
	}
	if focused != "" {
		m.restoreFocusedWorktree(focused)
	} else if m.cursor >= len(m.rows) {
		m.cursor = max(0, len(m.rows)-1)
	}
}

// listPageSize mirrors the visible-row count computed in View() so jump/page
// keys move by roughly what is on screen.
func (m model) listPageSize() int {
	available := max(1, max(5, m.height-6)-3)
	return max(1, available/2)
}

func (m model) matchesFilter(e entry) bool {
	switch m.filter {
	case filterSSH:
		return e.kind == "ssh"
	case filterSaved:
		return e.saved
	default:
		return true
	}
}

// sortEntries orders entries: open sessions first (most recently focused), then
// by category (saved → source projects → SSH), stable within each group by
// discovery order. Used after the initial load and again when zoxide projects
// arrive asynchronously.
func sortEntries(entries []entry) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		return domain.EntryLess(
			domain.EntryOrder{
				Open: a.open, LastFocused: a.lastFocused, Saved: a.saved, Kind: a.kind, Order: a.order,
			},
			domain.EntryOrder{
				Open: b.open, LastFocused: b.lastFocused, Saved: b.saved, Kind: b.kind, Order: b.order,
			},
		)
	})
}

// buildZoxideEntries turns `zoxide query -l` output into source-project entries,
// skipping paths already represented (open/saved session) and including live
// window paths zoxide does not track. Order mirrors zoxide's frecency ranking.
func buildZoxideEntries(output []byte, ctx zoxideMergeContext) []entry {
	return fromDomainEntries(catalog.MergeZoxide(output, domain.CatalogContext{
		LivePaths: ctx.livePaths, MergedPaths: ctx.merged, SessionNames: ctx.sessionNames, Home: ctx.home,
		OpenTabs: ctx.openTabs,
	}))
}

// unmergeSessionSources keeps every single-project session distinct from the
// zoxide source it originated from. The source must remain discoverable so a
// second session can be created from the same repository.
func unmergeRenamedSessionSources(entries []entry, _ nameStore, ctx *zoxideMergeContext) {
	for index := range entries {
		entry := &entries[index]
		if entry.path == "" || entry.session == "" || entry.kind != "project" || !entry.open || entry.saved {
			continue
		}
		// The catalog normally merges a single-project session into its project
		// entry. Give the live session its own identity before restoring the
		// source path; aliases.json entries remain cosmetic only.
		entry.key = "workspace:" + entry.session
		entry.kind = "workspace"
		delete(ctx.merged, entry.path)
	}
}

// loadEntriesFast builds entries from live Kitty state, saved sessions, and SSH
// hosts only — everything obtainable without the (sometimes slow) zoxide query.
// Zoxide-sourced source projects are returned separately via the async
// fetchZoxideEntries command so the picker can paint before they arrive.
func loadEntriesFast(kitty string) ([]entry, zoxideMergeContext, error) {
	if kitty == "" {
		return nil, zoxideMergeContext{}, fmt.Errorf("kitty was not found")
	}
	savedStore, err := loadSavedSessions()
	if err != nil {
		return nil, zoxideMergeContext{}, err
	}
	kittyState, err := (kittyx.Client{Executable: kitty}).State()
	if err != nil {
		return nil, zoxideMergeContext{}, fmt.Errorf("kitty @ ls: %w", err)
	}
	home := os.Getenv("HOME")
	selfID, _ := strconv.Atoi(os.Getenv("KITTY_WINDOW_ID"))
	sshHosts := catalog.ReadSSHHosts(filepath.Join(home, ".ssh", "config"), os.Getenv("USER"))
	domainEntries, context := catalog.Assemble(kittyState, savedStore, sshHosts, selfID, home)
	return fromDomainEntries(domainEntries), zoxideMergeContext{
		livePaths: context.LivePaths, merged: context.MergedPaths,
		sessionNames: context.SessionNames, home: context.Home, openTabs: context.OpenTabs,
	}, nil
}

// loadEntries is the synchronous full load (live Kitty + saved + SSH + zoxide),
// used by one-shot CLI paths such as pin switching where blocking on zoxide is
// acceptable. The interactive picker uses loadEntriesFast + fetchZoxideEntries
// instead so first paint is not gated on zoxide.
func loadEntries(kitty, zoxide string) ([]entry, error) {
	entries, ctx, err := loadEntriesFast(kitty)
	if err != nil {
		return nil, err
	}
	names, err := loadNames()
	if err != nil {
		return nil, err
	}
	unmergeRenamedSessionSources(entries, names, &ctx)
	if zoxide != "" {
		output, zerr := (catalog.Zoxide{Executable: zoxide}).Query()
		if zerr != nil {
			return nil, zerr
		}
		entries = append(entries, buildZoxideEntries(output, ctx)...)
		sortEntries(entries)
	}
	return entries, nil
}

// fetchZoxideEntries runs `zoxide query -l` off the main startup path and
// returns the zoxide-sourced source-project entries to merge once they arrive.
type zoxideEntriesMsg struct {
	entries []entry
	err     error
}

func fetchZoxideEntries(zoxide string, ctx zoxideMergeContext) tea.Cmd {
	return func() tea.Msg {
		if zoxide == "" {
			return zoxideEntriesMsg{err: fmt.Errorf("zoxide was not found")}
		}
		output, err := (catalog.Zoxide{Executable: zoxide}).Query()
		if err != nil {
			return zoxideEntriesMsg{err: err}
		}
		return zoxideEntriesMsg{entries: buildZoxideEntries(output, ctx)}
	}
}

func isKeshTab(windows []kittyWindow) bool {
	return catalog.IsKeshTab(windows)
}

func cachedPathPR(path string) pathPRInfo {
	if path == "" {
		return pathPRInfo{}
	}
	head, branch, err := (gitx.Repository{Path: path}).HeadAndBranch()
	if err != nil {
		return pathPRInfo{}
	}
	if head == "" || branch == "" {
		return pathPRInfo{}
	}
	repoKey := repositoryCacheKey(path)
	pullRequests, _ := loadPRStatusCache(repoKey)
	pullRequest, exact := matchPullRequest(pullRequests, branch, head)
	return pathPRInfo{Branch: branch, Head: head, RepoKey: repoKey, PullRequest: pullRequest, Exact: exact}
}

func windowItemFromKitty(window kittyWindow) windowItem {
	source := catalog.WindowFromKitty(window, os.Getenv("HOME"))
	return windowItem{
		id: source.ID, title: source.Title, detail: source.Detail, command: source.Command,
		fullCommand: source.FullCommand, agent: source.Agent, lastFocused: source.LastFocused, cwd: source.CWD,
	}
}

func cleanAgentTitle(title, agent string) string {
	return catalog.CleanAgentTitle(title, agent)
}

func agentFromWindow(window kittyWindow) string {
	return catalog.AgentFromWindow(window)
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
