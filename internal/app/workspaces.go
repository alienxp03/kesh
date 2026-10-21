package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	gitx "github.com/alienxp03/kesh/internal/git"
)

// wsAgent is one coding-agent window attributed to a worktree.
type wsAgent struct {
	windowID int
	tool     string
	status   string // ""|"working"|"finished"|"errored"|"idle"
	since    time.Time
}

// wsWorktree is a worktree row in the main surface with its derived state.
type wsWorktree struct {
	item      worktreeItem
	state     string // blocked|working|open|idle
	agents    []wsAgent
	windowID  int // first live kitty window inside the worktree, 0 if none
	entryName string
}

// wsRepo is a repo group: the unit the main surface is organized by.
type wsRepo struct {
	dir       string // canonical key: repo dir under the worktree root, or main path
	mainPath  string
	name      string
	expanded  bool
	worktrees []wsWorktree
	blocked   int
	working   int
}

// wsRepoSource is the raw per-repo input to buildRepoGroups. Project entries
// and scanned worktree-root repos are normalized into this shape so the merge
// stays a pure function.
type wsRepoSource struct {
	dir       string
	mainPath  string
	name      string
	owner     string
	repo      string
	worktrees []worktreeItem
}

// wsWindow is the per-window input: cwd plus agent detection state.
type wsWindow struct {
	windowID    int
	cwd         string
	agent       string
	agentStatus string
	since       time.Time
}

// Worktree state priorities: lower sorts first inside a repo.
var wsStateRank = map[string]int{"blocked": 0, "working": 1, "open": 2, "idle": 3}

func wsAgentBlocked(a wsAgent) bool {
	return a.status == "finished" || a.status == "errored"
}

func wsAgentWorking(a wsAgent) bool {
	return a.status == "working"
}

// worktreeWindowState resolves the exclusive display state: blocked by any
// finished/errored agent, else working by any running agent, else open when a
// live kitty window sits inside the worktree, else idle.
func worktreeWindowState(agents []wsAgent, live bool) string {
	for _, a := range agents {
		if wsAgentBlocked(a) {
			return "blocked"
		}
	}
	for _, a := range agents {
		if wsAgentWorking(a) {
			return "working"
		}
	}
	if live {
		return "open"
	}
	return "idle"
}

// wsAttribution maps every window to the worktree it runs in. A window whose
// cwd equals a worktree path or sits underneath it belongs to that worktree;
// the deepest (longest) matching path wins so a worktree nested inside another
// checkout's subtree still resolves correctly.
func wsAttribution(worktrees []worktreeItem, windows []wsWindow) map[int]int {
	owner := make(map[int]int, len(windows))
	for _, w := range windows {
		best, bestLen := -1, 0
		clean := filepath.Clean(w.cwd)
		for i, wt := range worktrees {
			base := filepath.Clean(wt.path)
			if clean == base || strings.HasPrefix(clean, base+string(filepath.Separator)) {
				if len(base) > bestLen {
					best, bestLen = i, len(base)
				}
			}
		}
		if best >= 0 {
			owner[w.windowID] = best
		}
	}
	return owner
}

// buildRepoGroups merges repo sources with live windows into display groups.
// Worktrees sort default-branch first, then by state; repos sort by blocked
// count, then working count, then name.
func buildRepoGroups(sources []wsRepoSource, windows []wsWindow) []wsRepo {
	groups := make([]wsRepo, 0, len(sources))
	for _, src := range sources {
		owner := wsAttribution(src.worktrees, windows)
		byWorktree := make([][]wsAgent, len(src.worktrees))
		live := make([]int, len(src.worktrees))
		for _, w := range windows {
			index, ok := owner[w.windowID]
			if !ok {
				continue
			}
			agent := ""
			if w.agent != "" {
				agent = agentStatusTool(w.agent)
			}
			if agent != "" {
				byWorktree[index] = append(byWorktree[index], wsAgent{
					windowID: w.windowID, tool: agent, status: w.agentStatus, since: w.since,
				})
			}
			if live[index] == 0 {
				live[index] = w.windowID
			}
		}
		group := wsRepo{dir: src.dir, mainPath: src.mainPath, name: src.name}
		for i, item := range src.worktrees {
			agents := byWorktree[i]
			group.worktrees = append(group.worktrees, wsWorktree{
				item: item, state: worktreeWindowState(agents, live[i] > 0),
				agents: agents, windowID: live[i],
			})
		}
		sort.SliceStable(group.worktrees, func(i, j int) bool {
			a, b := group.worktrees[i], group.worktrees[j]
			if a.item.isDefault != b.item.isDefault {
				return a.item.isDefault
			}
			if wsStateRank[a.state] != wsStateRank[b.state] {
				return wsStateRank[a.state] < wsStateRank[b.state]
			}
			return strings.ToLower(a.item.branch) < strings.ToLower(b.item.branch)
		})
		for _, wt := range group.worktrees {
			for _, a := range wt.agents {
				if wsAgentBlocked(a) {
					group.blocked++
				}
				if wsAgentWorking(a) {
					group.working++
				}
			}
		}
		groups = append(groups, group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		a, b := groups[i], groups[j]
		if a.blocked != b.blocked {
			return a.blocked > b.blocked
		}
		if a.working != b.working {
			return a.working > b.working
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
	return groups
}

// wsRepoSourcesFromEntries normalizes project/workspace entries that carry a
// loaded worktree cache into repo sources. An entry whose path lives inside a
// linked worktree still maps to its repository via the worktree list's primary
// entry, which git always reports first.
func wsRepoSourcesFromEntries(entries []entry) []wsRepoSource {
	sources := make([]wsRepoSource, 0, len(entries))
	for _, e := range entries {
		if e.worktreesLoaded && len(e.worktrees) > 0 {
			sources = append(sources, wsRepoSourceFromWorktrees(repoKeyFromEntry(e), e.path, e.name, e.worktrees))
		}
	}
	return dedupeRepoSources(sources)
}

// wsRepoSourceFromWorktrees derives owner/repo naming and the repo dir from a
// worktree list: git reports the primary checkout first, and every worktree
// shares its metadata root.
func wsRepoSourceFromWorktrees(dir, mainPath, name string, worktrees []worktreeItem) wsRepoSource {
	if len(worktrees) > 0 && dir == "" {
		dir = mainPath
	}
	owner, repo := getRepoOwner(mainPath)
	if name == "" {
		name = repo
	}
	return wsRepoSource{dir: dir, mainPath: mainPath, name: name, owner: owner, repo: repo, worktrees: worktrees}
}

func repoKeyFromEntry(e entry) string {
	if len(e.worktrees) > 0 {
		return e.worktrees[0].path
	}
	return e.path
}

func dedupeRepoSources(sources []wsRepoSource) []wsRepoSource {
	seen := make(map[string]bool, len(sources))
	result := make([]wsRepoSource, 0, len(sources))
	for _, src := range sources {
		key := cleanWorktreePath(src.dir)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		src.dir = key
		result = append(result, src)
	}
	return result
}

func cleanWorktreePath(path string) string {
	if path == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Clean(path)); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// flattenAgents renders the agent pane's flat list: blocked agents first
// (oldest first), then working (newest first), then idle.
func flattenAgents(groups []wsRepo) []struct {
	agent    wsAgent
	worktree string
	repo     string
} {
	type row = struct {
		agent    wsAgent
		worktree string
		repo     string
	}
	var blocked, working, idle []row
	for _, group := range groups {
		for _, wt := range group.worktrees {
			for _, a := range wt.agents {
				entry := row{agent: a, worktree: filepath.Base(wt.item.path), repo: group.name}
				switch {
				case wsAgentBlocked(a):
					blocked = append(blocked, entry)
				case wsAgentWorking(a):
					working = append(working, entry)
				default:
					idle = append(idle, entry)
				}
			}
		}
	}
	sort.SliceStable(blocked, func(i, j int) bool {
		return blocked[i].agent.since.Before(blocked[j].agent.since)
	})
	sort.SliceStable(working, func(i, j int) bool {
		return working[i].agent.since.After(working[j].agent.since)
	})
	return append(append(blocked, working...), idle...)
}

// formatAge renders a compact human age for the agent pane.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// --- worktree root scan ---

type scanRepo struct {
	dir   string
	owner string
	repo  string
}

type scanReposMsg struct {
	repos []scanRepo
	err   error
}

type scanWorktreeMsg struct {
	dir       string
	worktrees []worktreeItem
	err       error
}

// scanWorktreeRoot discovers candidate repos under the worktree root. The
// root's layout is <root>/<owner>/<repo>/<branch-slug>, so two levels of
// directories name the repos; validity is confirmed later per repo by git.
func scanWorktreeRoot(root string) tea.Cmd {
	return func() tea.Msg {
		if root == "" {
			return scanReposMsg{err: fmt.Errorf("no worktree root configured")}
		}
		owners, err := os.ReadDir(root)
		if err != nil {
			return scanReposMsg{err: err}
		}
		var repos []scanRepo
		for _, owner := range owners {
			if !owner.IsDir() || strings.HasPrefix(owner.Name(), ".") {
				continue
			}
			reposDirs, err := os.ReadDir(filepath.Join(root, owner.Name()))
			if err != nil {
				continue
			}
			for _, repoDir := range reposDirs {
				if !repoDir.IsDir() || strings.HasPrefix(repoDir.Name(), ".") {
					continue
				}
				repos = append(repos, scanRepo{
					dir:   filepath.Join(root, owner.Name(), repoDir.Name()),
					owner: owner.Name(), repo: repoDir.Name(),
				})
			}
		}
		return scanReposMsg{repos: repos}
	}
}

// fetchScanWorktrees loads the worktree list for one scanned repo. It mirrors
// fetchWorktrees' enrichment (default branch, current marker, cached PR state)
// but keys the result by repo dir because scanned repos have no picker entry.
func fetchScanWorktrees(dir string) tea.Cmd {
	return func() tea.Msg {
		repository := gitx.Repository{Path: dir}
		output, err := repository.WorktreePorcelain()
		if err != nil {
			return scanWorktreeMsg{dir: dir, err: err}
		}
		worktrees := parseWorktreePorcelain(output)
		if len(worktrees) == 0 {
			return scanWorktreeMsg{dir: dir, err: fmt.Errorf("not a git repository: %s", dir)}
		}
		defaultBranch := repository.DefaultBranch()
		if defaultBranch == "" && len(worktrees) > 0 {
			defaultBranch = worktrees[0].branch
		}
		repoKey := repositoryCacheKey(dir)
		cached, _ := loadPRStatusCache(repoKey)
		for i := range worktrees {
			worktrees[i].isDefault = worktrees[i].branch == defaultBranch
			worktrees[i].prRepoKey = repoKey
			pullRequest, exact := matchPullRequest(cached, worktrees[i].branch, worktrees[i].head)
			worktrees[i].prStatus = pullRequest.Status
			worktrees[i].prURL = pullRequest.URL
			worktrees[i].prNumber = pullRequest.Number
			worktrees[i].prExact = exact
		}
		sortWorktreeItems(worktrees)
		return scanWorktreeMsg{dir: dir, worktrees: worktrees}
	}
}

// wsRepoCoveredByEntries reports whether a scanned repo is already represented
// by a project entry, so its worktrees are not fetched twice. Comparison runs
// on the primary checkout path: an entry opened inside any of the repo's
// worktrees shares the same worktree list.
func wsRepoCoveredByEntries(dir string, entries []entry) bool {
	for _, e := range entries {
		if e.kind != "project" && e.kind != "workspace" {
			continue
		}
		if e.worktreesLoaded && len(e.worktrees) > 0 {
			if cleanWorktreePath(e.worktrees[0].path) == cleanWorktreePath(dir) ||
				sameWorktreePath(e.worktrees[0].path, dir) {
				return true
			}
			continue
		}
		if e.path != "" && sameWorktreePath(e.path, dir) {
			return true
		}
	}
	return false
}

// prefetchEntryWorktrees eagerly loads worktree lists for open projects so the
// main surface groups them without waiting for a drill-in. Closed projects
// stay in the other group until opened.
func prefetchEntryWorktrees(entries []entry) tea.Cmd {
	var cmds []tea.Cmd
	for i, e := range entries {
		if e.open && (e.kind == "project" || e.kind == "workspace") && e.path != "" {
			cmds = append(cmds, fetchWorktrees(e.path, i, -1, -1))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}
