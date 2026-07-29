package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alienxp03/kesh/internal/catalog"
	"github.com/alienxp03/kesh/internal/config"
	"github.com/alienxp03/kesh/internal/domain"
	gitx "github.com/alienxp03/kesh/internal/git"
	githubx "github.com/alienxp03/kesh/internal/github"
	kittyx "github.com/alienxp03/kesh/internal/kitty"
	"github.com/alienxp03/kesh/internal/state"
	"github.com/alienxp03/kesh/internal/system"
	"github.com/alienxp03/kesh/internal/workspace"
)

func worktreeDirectoryName(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// sameWorktreePath handles Kitty's occasional case-preserving path variants
// on case-insensitive filesystems (for example /Workspace vs /workspace).
func sameWorktreePath(left, right string) bool {
	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}
	leftInfo, leftErr := os.Stat(left)
	rightInfo, rightErr := os.Stat(right)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

// worktreeCreate is the workspace entry point used by runWktreeNew. It is a
// package-level variable so tests can substitute a fake without exercising git
// or Kitty.
var worktreeCreate = workspace.Create

// workspaceOpen is the no-worktree launch entry point used by runLaunchLayout.
// Like worktreeCreate, it is a package-level variable for test substitution.
var workspaceOpen = workspace.Open

// runWktreeNew drives recipe-driven worktree creation and Kitty layout setup
// through the in-process workspace package. Kesh owns the interactive branch
// prompt and refresh afterward.
func runWktreeNew(recipePath, mode, branch string, selected []string) tea.Cmd {
	return func() tea.Msg {
		worktreeHome, err := loadWorktreeRoot()
		if err != nil {
			return worktreeMsg{err: err}
		}
		err = worktreeCreate(context.Background(), workspace.CreateOptions{
			Cwd:      filepath.Dir(recipePath),
			Branch:   branch,
			Home:     worktreeHome,
			Mode:     mode,
			Selected: selected,
		})
		return worktreeMsg{err: err}
	}
}

// runLaunchLayout drives the no-worktree launch: it opens the .kesh.yaml
// layout against each selected workspace's existing folder through the
// in-process workspace package. Kesh owns the workspace picker; refresh
// afterward reuses the worktreeMsg completion path. sessionName overrides the
// Kitty session name (empty defers to the auto-derived name).
func runLaunchLayout(cwd, mode, sessionName string, selected []string) tea.Cmd {
	return func() tea.Msg {
		err := workspaceOpen(context.Background(), workspace.OpenOptions{
			Cwd:         cwd,
			Mode:        mode,
			Selected:    selected,
			SessionName: sessionName,
		})
		return worktreeMsg{err: err}
	}
}

func uniqueSessionSuffix() (string, error) {
	id := make([]byte, 6)
	if _, err := rand.Read(id); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(id), nil
}

func runCreateSession(kitty string, entries []entry, name string) tea.Cmd {
	return func() tea.Msg {
		if len(entries) == 0 {
			return createMsg{err: fmt.Errorf("select at least one project or SSH host")}
		}
		suffix, err := uniqueSessionSuffix()
		if err != nil {
			return createMsg{err: err}
		}
		internalName := "kesh-" + name + "--" + suffix
		path := composedSessionPath(internalName)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return createMsg{err: fmt.Errorf("create session directory: %w", err)}
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if os.IsExist(err) {
				return createMsg{err: fmt.Errorf("a session named %q already exists", name)}
			}
			return createMsg{err: fmt.Errorf("create session file: %w", err)}
		}
		defer os.Remove(path)
		if _, err := file.WriteString(composedSessionContent(internalName, entries)); err != nil {
			file.Close()
			return createMsg{err: fmt.Errorf("write session file: %w", err)}
		}
		if err := file.Close(); err != nil {
			return createMsg{err: fmt.Errorf("close session file: %w", err)}
		}
		if err := (kittyx.Client{Executable: kitty}).GotoSession(path); err != nil {
			return createMsg{err: err}
		}
		return createMsg{}
	}
}

func runAction(kitty, zoxide string, e entry, selected row) tea.Cmd {
	return func() tea.Msg {
		// The Worktree tab addresses a worktree by path (the tab list is a
		// filtered, re-sorted subset of the entry's worktrees, so a wt index
		// would target the wrong tree). Open it directly instead of falling
		// through to the project's main checkout.
		if selected.worktreePath != "" {
			return actionMsg{err: openProjectSession(kitty, zoxide, selected.worktreePath, false)}
		}
		if selected.windowIndex >= 0 {
			window := e.tabs[selected.tabIndex].windows[selected.windowIndex]
			return actionMsg{err: (kittyx.Client{Executable: kitty}).FocusWindow(window.id)}
		}
		if selected.tabIndex >= 0 {
			return actionMsg{err: (kittyx.Client{Executable: kitty}).FocusTab(e.tabs[selected.tabIndex].id)}
		}
		if e.sessionFile != "" {
			return actionMsg{err: (kittyx.Client{Executable: kitty}).GotoSession(e.sessionFile)}
		}
		if e.session != "" {
			return actionMsg{err: (kittyx.Client{Executable: kitty}).GotoSession(e.session)}
		}
		if len(e.tabs) > 0 {
			return actionMsg{err: (kittyx.Client{Executable: kitty}).FocusTab(e.tabs[0].id)}
		}
		if e.kind == "ssh" {
			sessionDir := filepath.Join(os.TempDir(), "kitty-zoxide-sessions")
			if err := os.MkdirAll(sessionDir, 0o755); err != nil {
				return actionMsg{err: err}
			}
			host := strings.TrimPrefix(e.key, "ssh://")
			file := filepath.Join(sessionDir, "ssh-"+safeName(host)+".kitty-session")
			content := fmt.Sprintf("layout splits\ncd %s\nlaunch --title \"ssh: %s\" ssh \"%s\"\nfocus\nfocus_os_window\n", os.Getenv("HOME"), host, host)
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				return actionMsg{err: err}
			}
			return actionMsg{err: (kittyx.Client{Executable: kitty}).GotoSession(file)}
		}
		return actionMsg{err: openProjectSession(kitty, zoxide, e.key, e.nameTaken)}
	}
}

func openProjectSession(kitty, zoxide, project string, nameTaken bool) error {
	sessionDir := filepath.Join(os.TempDir(), "kitty-zoxide-sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}
	name := safeName(filepath.Base(project))
	if nameTaken {
		suffix, err := uniqueSessionSuffix()
		if err != nil {
			return err
		}
		name += "-" + suffix
	}
	file := filepath.Join(sessionDir, name+".kitty-session")
	content := fmt.Sprintf("layout splits\ncd %s\nlaunch --title %s\nfocus\nfocus_os_window\n", project, strconv.Quote(filepath.Base(project)))
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		return err
	}
	_ = (catalog.Zoxide{Executable: zoxide}).Add(project)
	if err := (kittyx.Client{Executable: kitty}).GotoSession(file); err != nil {
		return err
	}
	return nil
}

// fetchOriginThenReload updates remote-tracking refs before reloading a
// project's worktrees. Local state still reloads if fetch fails, while the
// returned error makes a stale remote status visible to the user.
func fetchOriginThenReload(dir string, entryIndex int) tea.Cmd {
	return func() tea.Msg {
		err := (gitx.Repository{Path: dir}).FetchPrune()
		return worktreeFetchedMsg{dir: dir, entryIndex: entryIndex, err: err}
	}
}

// pullWorktree fast-forwards or rebases a single worktree's branch onto its
// upstream. The reload afterwards recomputes the worktree's ahead/behind.
func pullWorktree(path, dir string, entryIndex int) tea.Cmd {
	return func() tea.Msg {
		if err := (gitx.Repository{Path: path}).PullRebase(); err != nil {
			return worktreePullMsg{dir: dir, entryIndex: entryIndex, err: err}
		}
		return worktreePullMsg{dir: dir, entryIndex: entryIndex}
	}
}

// pullWorktrees pulls every target worktree in one command, collecting per-
// target failures so one bad branch does not abort the rest. Returns a single
// summary that the worktreePullMsg handler surfaces and then reloads.
func pullWorktrees(targets []worktreeItem, dir string, entryIndex int) tea.Cmd {
	return func() tea.Msg {
		var failures []string
		for _, target := range targets {
			if target.path == "" {
				continue
			}
			if err := (gitx.Repository{Path: target.path}).PullRebase(); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", target.branch, err).Error())
			}
		}
		if len(failures) > 0 {
			return worktreePullMsg{dir: dir, entryIndex: entryIndex, err: fmt.Errorf("some worktrees did not pull: %s", strings.Join(failures, "; "))}
		}
		return worktreePullMsg{dir: dir, entryIndex: entryIndex}
	}
}

// openerCommand resolves the platform's default URL opener so PR links open in
// the browser on every OS: macOS uses "open", Linux/BSDs use "xdg-open". It
// scans PATH so a missing opener is reported cleanly instead of shelling out to
// a binary that does not exist. Returns "" when no opener is installed.
func openerCommand() string {
	return system.DefaultOpener().Command()
}

// openURL opens target in the user's default browser via the platform opener.
// Both the Worktree tab and the main-mode PR opener route through here so the
// platform choice lives in one place.
func openURL(target string) error {
	return system.DefaultOpener().Open(target)
}

func safeName(value string) string {
	return regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(value, "_")
}

func shortHash(value string) string {
	// FNV-1a is sufficient here; the hash only disambiguates equal basenames.
	var hash uint32 = 2166136261
	for i := 0; i < len(value); i++ {
		hash ^= uint32(value[i])
		hash *= 16777619
	}
	return fmt.Sprintf("%06x", hash)[:6]
}

func displayPath(path, home string) string {
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func (m *model) calculateWorktreePaths() []string {
	entries := m.worktreeEntries()
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.kind != "project" {
			continue
		}
		identity := m.worktreeRepositories[entry.path]
		if identity.owner == "" {
			identity = repoIdentity{owner: "user", repo: filepath.Base(entry.path)}
		}
		// filepath.Join drops a trailing empty segment, so with no branch typed
		// yet the preview resolves to the repo directory and fills in as the
		// branch is entered — keeping the popup layout stable from the start.
		worktreePath := filepath.Join(m.worktreeRoot, identity.owner, identity.repo, m.worktreeBranch)
		paths = append(paths, displayPath(worktreePath, os.Getenv("HOME")))
	}
	return paths
}

func (m *model) validateWorktreeBranch() tea.Cmd {
	if m.worktreeBranch == "" {
		return nil
	}
	entries := m.worktreeEntries()
	if len(entries) == 0 {
		return nil
	}
	branch := m.worktreeBranch
	worktreeRoot := m.worktreeRoot
	home := os.Getenv("HOME")

	return func() tea.Msg {
		for _, entry := range entries {
			if entry.kind != "project" {
				continue
			}
			// A fresh branch name is valid: it is created off each project's
			// HEAD at worktree-add time. Only a path collision is rejected, so
			// validation stays local and network-free.
			owner, repo := getRepoOwner(entry.path)
			worktreePath := filepath.Join(worktreeRoot, owner, repo, branch)
			if _, err := os.Stat(worktreePath); err == nil {
				return worktreeMsg{err: fmt.Errorf("worktree already exists at %s", displayPath(worktreePath, home)), validation: true}
			}
		}
		// Validation successful - return nil error to indicate valid.
		return worktreeMsg{validation: true}
	}

}
func (m *model) createWorktree() tea.Cmd {
	entries := m.worktreeEntries()
	branch := m.worktreeBranch
	worktreeRoot := m.worktreeRoot
	kitty := m.kitty
	zoxide := m.zoxide
	return func() tea.Msg {
		created := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.kind != "project" {
				continue
			}
			owner, repo := getRepoOwner(entry.path)
			worktreePath := filepath.Join(worktreeRoot, owner, repo, branch)

			repository := gitx.Repository{Path: entry.path}
			if err := addWorktreeForBranch(repository, worktreePath, branch); err != nil {
				return worktreeMsg{err: fmt.Errorf("failed to create worktree for %s: %w", entry.name, err)}
			}

			created = append(created, worktreePath)
		}
		if len(created) == 0 {
			return worktreeMsg{err: fmt.Errorf("no project selected for worktree creation")}
		}
		// Native creation has no recipe engine to perform the Kitty handoff.
		// Open the new folder explicitly so Enter behaves like recipe-backed
		// creation instead of silently returning to the worktree list.
		if err := openProjectSession(kitty, zoxide, created[0], false); err != nil {
			return worktreeMsg{err: fmt.Errorf("open created worktree: %w", err)}
		}
		return worktreeMsg{}
	}
}

// addWorktreeForBranch checks out an existing remote branch when origin has it;
// otherwise it creates a new branch off the repository's current HEAD. This lets
// the worktree popup accept brand-new branch names instead of only those already
// pushed to origin.
func addWorktreeForBranch(repository gitx.Repository, worktreePath, branch string) error {
	exists, err := repository.BranchExistsOnOrigin(branch)
	if err != nil {
		return fmt.Errorf("check origin for %s: %w", branch, err)
	}
	if exists {
		return repository.AddWorktree(worktreePath, "origin/"+branch)
	}
	return repository.CreateBranchWorktree(worktreePath, branch)
}
func getRepoOwner(repoPath string) (owner, repo string) {
	remoteURL, err := (gitx.Repository{Path: repoPath}).RemoteURL()
	if err != nil {
		return "user", filepath.Base(repoPath)
	}
	url := strings.TrimSpace(remoteURL)

	// Parse GitHub URL: git@github.com:owner/repo.git or https://github.com/owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		parts := strings.TrimPrefix(url, "git@github.com:")
		parts = strings.TrimSuffix(parts, ".git")
		slashes := strings.Split(parts, "/")
		if len(slashes) >= 2 {
			return slashes[0], slashes[1]
		}
	}
	if strings.HasPrefix(url, "https://github.com/") {
		parts := strings.TrimPrefix(url, "https://github.com/")
		parts = strings.TrimSuffix(parts, ".git")
		slashes := strings.Split(parts, "/")
		if len(slashes) >= 2 {
			return slashes[0], slashes[1]
		}
	}
	// Fallback
	return "user", filepath.Base(repoPath)
}

func (m model) selectedPullRequest() (string, string) {
	if len(m.rows) == 0 || m.cursor < 0 || m.cursor >= len(m.rows) {
		return "", ""
	}
	selected := m.rows[m.cursor]
	entry := m.entries[selected.entryIndex]
	pullRequestURL := ""
	branch := entry.pathPR.Branch
	switch {
	case selected.section == "wt-filter" && selected.wt >= 0 && selected.wt < len(m.worktreeFilterRows):
		worktree := m.worktreeFilterRows[selected.wt].worktree
		pullRequestURL, branch = worktree.prURL, worktree.branch
	case selected.windowIndex >= 0:
		info := entry.tabs[selected.tabIndex].windows[selected.windowIndex].pathPR
		pullRequestURL, branch = info.PullRequest.URL, info.Branch
	case selected.tabIndex >= 0:
		for _, window := range entry.tabs[selected.tabIndex].windows {
			if window.pathPR.PullRequest.URL != "" {
				pullRequestURL, branch = window.pathPR.PullRequest.URL, window.pathPR.Branch
				break
			}
		}
	default:
		pullRequestURL = entry.pathPR.PullRequest.URL
	}
	return pullRequestURL, branch
}

func (m *model) openWorktreePR() tea.Cmd {
	pullRequestURL, branch := m.selectedPullRequest()
	parsed, err := url.Parse(pullRequestURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		if branch == "" {
			branch = "selected row"
		}
		m.err = fmt.Errorf("no matching pull request for %s", branch)
		return nil
	}
	m.err = nil
	return func() tea.Msg {
		return openPRMsg{err: openURL(pullRequestURL)}
	}
}

// beginWorktreeCreate opens the create-worktree form for the project whose
// Worktree tab is open. While the tab is active, worktreeEntries() resolves
// that project, so the paths, recipe, and validation pipeline target it
// unchanged. The form overlays the tab; cancelling returns there because the
// filter is left on filterWorktrees.
func (m *model) beginWorktreeCreate() tea.Cmd {
	if len(m.worktreeEntries()) == 0 {
		m.err = fmt.Errorf("no project in this worktree tab")
		return nil
	}
	m.activateMode(modeWorktreeCreate)
	m.worktreeBranch = ""
	m.worktreePaths = m.calculateWorktreePaths()
	m.worktreeRecipe = nil
	m.worktreeRecipePath = ""
	m.worktreeRecipeMode = ""
	m.worktreeCustomWorkspaces = false
	entries := m.worktreeEntries()
	if len(entries) == 1 && entries[0].path != "" {
		projectPath := entries[0].path
		entries = append([]entry(nil), entries...)
		m.err = nil
		return func() tea.Msg {
			recipe, recipePath, err := loadRecipe(projectPath)
			repositories := make(map[string]repoIdentity, len(entries))
			for _, entry := range entries {
				owner, repo := getRepoOwner(entry.path)
				repositories[entry.path] = repoIdentity{owner: owner, repo: repo}
			}
			return worktreeRecipeMsg{
				projectPath: projectPath, recipe: recipe, recipePath: recipePath,
				repositories: repositories, err: err,
			}
		}
	}
	m.err = nil
	return nil
}

// beginLaunchLayoutForEntry opens the launch-layout form for the project under
// the cursor. It reuses the worktree-create form's recipe loading and workspace
// picker, but sets launchOnFolder so Enter dispatches to runLaunchLayout.
func (m *model) beginLaunchLayoutForEntry(entry entry) tea.Cmd {
	if entry.kind != "project" || entry.path == "" {
		m.err = fmt.Errorf("select a project entry")
		return nil
	}
	m.activateMode(modeWorktreeCreate)
	m.launchOnFolder = true
	m.launchProjectPath = entry.path
	folderName := safeName(filepath.Base(filepath.Clean(entry.path)))
	if folderName != "" && folderName != "." {
		m.worktreeSessionName = folderName
	}
	m.worktreeBranch = ""
	m.worktreePaths = nil
	m.worktreeRecipe = nil
	m.worktreeRecipePath = ""
	m.worktreeRecipeMode = ""
	m.worktreeCustomWorkspaces = false
	projectPath := entry.path
	m.err = nil
	return func() tea.Msg {
		recipe, recipePath, err := loadRecipe(projectPath)
		return worktreeRecipeMsg{
			projectPath: projectPath, recipe: recipe, recipePath: recipePath,
			repositories: map[string]repoIdentity{}, err: err,
		}
	}
}

func (m *model) launchEntries() []entry {
	if m.launchProjectPath == "" {
		return m.worktreeEntries()
	}
	for _, candidate := range m.entries {
		if candidate.path == m.launchProjectPath && candidate.kind == "project" {
			return []entry{candidate}
		}
	}
	return nil
}

// confirmLaunchLayout validates the picker and dispatches the no-worktree
// launch. With no recipe it degrades to a plain folder open; otherwise it
// launches the recipe layout on the selected workspaces' existing folders.
func (m model) confirmLaunchLayout() (tea.Model, tea.Cmd) {
	entries := m.launchEntries()
	if len(entries) != 1 || entries[0].path == "" {
		m.err = fmt.Errorf("select a single project")
		return m, nil
	}
	m.err = nil
	if m.worktreeRecipe == nil || m.worktreeRecipeMode == "none" {
		// No .kesh.yaml, or Plain mode selected: open the folder as a single
		// window without launching the recipe layout.
		entry := entries[0]
		m.cancelMode()
		return m, func() tea.Msg {
			return actionMsg{err: openProjectSession(m.kitty, m.zoxide, entry.path, entry.nameTaken)}
		}
	}
	var selected []string
	mode := ""
	if m.worktreeCustomWorkspaces {
		mode = workspace.ModeSelected
		selected = m.selectedWorkspaceNames()
		if len(selected) == 0 {
			m.err = fmt.Errorf("select at least one workspace")
			return m, nil
		}
	}
	m.worktreeBusy = true
	return m, runLaunchLayout(entries[0].path, mode, m.worktreeSessionName, selected)
}

func prStatusCachePath() string {
	return config.FromEnvironment().PRCache()
}

func prStatusKey(branch, head string) string {
	return domain.PullRequestKey(branch, head)
}

func matchPullRequest(pullRequests map[string]prInfo, branch, head string) (prInfo, bool) {
	return domain.MatchPullRequest(pullRequests, branch, head)
}

func repositoryCacheKey(dir string) string {
	repository := gitx.Repository{Path: dir}
	remote, err := repository.RemoteURL()
	if err == nil && strings.TrimSpace(remote) != "" {
		remote = strings.TrimSpace(remote)
		if parsed, parseErr := url.Parse(remote); parseErr == nil && parsed.Scheme != "" {
			parsed.User = nil
			remote = parsed.String()
		}
		return remote
	}
	root, err := repository.Root()
	if err != nil {
		return ""
	}
	return root
}

func loadPRStatusCache(repoKey string) (map[string]prInfo, time.Time) {
	return state.LoadPRCache(prStatusCachePath(), repoKey)
}

func savePRStatusCache(repoKey string, pullRequests map[string]prInfo) error {
	return state.SavePRCache(prStatusCachePath(), repoKey, pullRequests, time.Now())
}

func queryPRStatuses(dir string) (string, map[string]prInfo, error) {
	repoKey := repositoryCacheKey(dir)
	if repoKey == "" {
		return "", nil, fmt.Errorf("repository has no cache key")
	}
	gh := findCommand("gh",
		filepath.Join(os.Getenv("HOME"), ".local", "share", "mise", "shims", "gh"),
		"/opt/homebrew/bin/gh",
		"/usr/local/bin/gh",
	)
	if gh == "" {
		return repoKey, nil, fmt.Errorf("gh was not found")
	}
	statuses, err := (githubx.Client{Executable: gh}).PullRequests(dir)
	if err != nil {
		return repoKey, nil, err
	}
	_ = savePRStatusCache(repoKey, statuses)
	return repoKey, statuses, nil
}

func (m *model) refreshPRStatuses(dir string, force bool) tea.Cmd {
	if dir == "" {
		return nil
	}
	if m.prStatusDirPending == nil {
		m.prStatusDirPending = map[string]bool{}
	}
	if m.prStatusDirPending[dir] {
		return nil
	}
	m.prStatusDirPending[dir] = true
	return func() tea.Msg {
		repoKey := repositoryCacheKey(dir)
		if repoKey == "" {
			return prStatusCacheMsg{dir: dir, force: force, err: fmt.Errorf("repository has no cache key")}
		}
		var cached map[string]prInfo
		var cachedAt time.Time
		if !force {
			cached, cachedAt = loadPRStatusCache(repoKey)
		}
		return prStatusCacheMsg{
			dir: dir, repoKey: repoKey, pullRequests: cached, cachedAt: cachedAt,
			now: time.Now(), force: force,
		}
	}
}

func queryPRStatusesCommand(dir, repoKey string) tea.Cmd {
	return func() tea.Msg {
		key, pullRequests, err := queryPRStatuses(dir)
		if key == "" {
			key = repoKey
		}
		return prStatusMsg{repoKey: key, pullRequests: pullRequests, fetchedAt: time.Now(), err: err}
	}
}

func worktreePriority(worktree worktreeItem) int {
	return domain.WorktreePriority(toDomainWorktree(worktree))
}

func sortWorktreeItems(worktrees []worktreeItem) {
	domainWorktrees := toDomainWorktrees(worktrees)
	domain.SortWorktrees(domainWorktrees)
	copy(worktrees, fromDomainWorktrees(domainWorktrees))
}

func (m *model) applyPRStatuses(repoKey string, pullRequests map[string]prInfo) {
	apply := func(worktrees []worktreeItem) {
		for index := range worktrees {
			if worktrees[index].prRepoKey == repoKey {
				pullRequest, exact := matchPullRequest(pullRequests, worktrees[index].branch, worktrees[index].head)
				worktrees[index].prStatus = pullRequest.Status
				worktrees[index].prURL = pullRequest.URL
				worktrees[index].prNumber = pullRequest.Number
				worktrees[index].prExact = exact
			}
		}
		sortWorktreeItems(worktrees)
	}
	applyPath := func(info *pathPRInfo) {
		if info.RepoKey == repoKey && info.Branch != "" {
			info.PullRequest, info.Exact = matchPullRequest(pullRequests, info.Branch, info.Head)
		}
	}
	for index := range m.entries {
		apply(m.entries[index].worktrees)
		applyPath(&m.entries[index].pathPR)
		for tabIndex := range m.entries[index].tabs {
			for windowIndex := range m.entries[index].tabs[tabIndex].windows {
				applyPath(&m.entries[index].tabs[tabIndex].windows[windowIndex].pathPR)
			}
		}
	}
}

// findMergedWorktrees finds non-current worktrees whose branches are either
// ancestors of the repository's current HEAD or are the exact head of a merged
// GitHub pull request. Capital X confirms once; dirty worktrees are never forced.
func (m *model) findMergedWorktrees() tea.Cmd {
	if len(m.rows) == 0 {
		return nil
	}
	selected := m.rows[m.cursor]
	dir := m.worktreeDirectory(selected)
	if dir == "" {
		m.err = fmt.Errorf("place the cursor on a window or closed project")
		return nil
	}
	m.mergedWorktreeBusy = true
	m.err = nil
	return func() tea.Msg {
		repository := gitx.Repository{Path: dir}
		worktreeOutput, err := repository.WorktreePorcelain()
		if err != nil {
			return mergedWorktreeListMsg{selected: selected, dir: dir, err: err}
		}
		mergedOutput, err := repository.MergedBranches()
		if err != nil {
			return mergedWorktreeListMsg{selected: selected, dir: dir, err: err}
		}
		currentBranch, err := repository.CurrentBranch()
		if err != nil {
			return mergedWorktreeListMsg{selected: selected, dir: dir, err: err}
		}
		return mergedWorktreeListMsg{
			selected: selected,
			dir:      dir,
			worktrees: mergedWorktreeItems(
				parseWorktreePorcelain(worktreeOutput),
				mergedOutput,
				currentBranch,
				mergedPullRequestHeads(dir),
			),
		}
	}
}

func mergedWorktreeItems(worktrees []worktreeItem, mergedOutput, currentBranch string, pullRequestHeads map[string]map[string]bool) []worktreeItem {
	return fromDomainWorktrees(domain.MergedWorktrees(
		toDomainWorktrees(worktrees),
		mergedOutput,
		currentBranch,
		pullRequestHeads,
	))
}

// mergedPullRequestHeads supplements Git's ancestry check for squash- and
// rebase-merged pull requests. A branch is accepted only when its current
// worktree HEAD exactly matches the head recorded by GitHub, preventing a reused
// branch with newer unmerged commits from being removed. Git remains the
// fallback when gh is unavailable, unauthenticated, or used outside GitHub.
func mergedPullRequestHeads(dir string) map[string]map[string]bool {
	_, statuses, err := queryPRStatuses(dir)
	if err != nil {
		return nil
	}
	heads := map[string]map[string]bool{}
	for key, pullRequest := range statuses {
		if pullRequest.Status != "merged" {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		if heads[parts[0]] == nil {
			heads[parts[0]] = map[string]bool{}
		}
		heads[parts[0]][parts[1]] = true
	}
	return heads
}

func (m *model) runRemoveMergedWorktrees(force bool) tea.Cmd {
	selected := m.closeRow
	dir := m.worktreeDirectory(selected)
	targets := append([]worktreeItem(nil), m.mergedWorktrees...)
	return func() tea.Msg {
		var failures []string
		var remaining []worktreeItem
		for _, target := range targets {
			if err := (gitx.Repository{Path: dir}).RemoveWorktree(target.path, force); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", target.branch, err).Error())
				remaining = append(remaining, target)
			}
		}
		if len(failures) > 0 {
			return mergedWorktreeRemoveMsg{
				selected: selected, dir: dir, remaining: remaining, forceTried: force,
				err: fmt.Errorf("some merged worktrees were not removed: %s", strings.Join(failures, "; ")),
			}
		}
		return mergedWorktreeRemoveMsg{selected: selected, dir: dir, forceTried: force}
	}
}

// runRemoveWorktrees removes every bulk-selected worktree in the Worktree tab,
// mirroring runRemoveMergedWorktrees. Non-force: a target with open Kitty
// windows or uncommitted changes fails for that one and is named in the
// summary; the rest are still removed.
func (m *model) runRemoveWorktrees() tea.Cmd {
	selected := m.closeRow
	dir := m.worktreeDirectory(selected)
	targets := append([]worktreeItem(nil), m.bulkWorktrees...)
	entryIndex := selected.entryIndex
	return func() tea.Msg {
		var failures []string
		for _, target := range targets {
			if err := (gitx.Repository{Path: dir}).RemoveWorktree(target.path, false); err != nil {
				failures = append(failures, fmt.Errorf("%s: %w", target.branch, err).Error())
			}
		}
		if len(failures) > 0 {
			return bulkWorktreeRemoveMsg{entryIndex: entryIndex, dir: dir, err: fmt.Errorf("some worktrees were not removed: %s", strings.Join(failures, "; "))}
		}
		return bulkWorktreeRemoveMsg{entryIndex: entryIndex, dir: dir}
	}
}

func (m *model) worktreeDirectory(r row) string {
	if m.filter == filterWorktrees && r.entryIndex >= 0 && r.entryIndex < len(m.entries) {
		// The tab lists one project's worktrees regardless of whether it is
		// open; closedEntryAt skips open entries, so resolve the repo from the
		// tab's project directly.
		return m.entries[r.entryIndex].path
	}
	if w := m.windowAt(r.entryIndex, r.tabIndex, r.windowIndex); w != nil {
		return w.cwd
	}
	if e := m.closedEntryAt(r.entryIndex, r.tabIndex, r.windowIndex); e != nil {
		return e.path
	}
	return ""
}

func (m *model) invalidateWorktrees(r row) {
	if r.entryIndex >= 0 && r.entryIndex < len(m.entries) {
		m.entries[r.entryIndex].worktreesLoaded = false
	}
}

// worktreeWindowIDs returns live Kitty windows rooted in target (including a
// child directory). Querying Kitty here, rather than relying on the picker
// snapshot, prevents deleting a worktree opened after Kesh started.
func worktreeWindowIDs(kitty, target string) ([]int, error) {
	state, err := (kittyx.Client{Executable: kitty}).State()
	if err != nil {
		return nil, fmt.Errorf("kitty @ ls: %w", err)
	}
	target = filepath.Clean(target)
	prefix := target + string(filepath.Separator)
	var ids []int
	for _, osWindow := range state {
		for _, tab := range osWindow.Tabs {
			for _, window := range tab.Windows {
				cwd := filepath.Clean(window.CWD)
				if cwd == target || strings.HasPrefix(cwd, prefix) {
					ids = append(ids, window.ID)
				}
			}
		}
	}
	return ids, nil
}

// destroyPlan describes the layers a unified Destroy (D) removes for one
// focused entry. Only non-empty/true layers are touched.
type destroyPlan struct {
	entryName    string
	closeSession bool // close the kitty session tabs
	tabCount     int
	worktreePath string // first linked worktree, kept for prompt/test compatibility
	branch       string // first linked branch, kept for prompt/test compatibility
	worktrees    []domain.LinkedWorktree
	saved        bool // delete the saved-session record + snapshot
}

type destroyMsg struct {
	entries []entry
	err     error
}

type destroyPlanMsg struct {
	entryKey string
	plan     destroyPlan
}

// linkedWorktreeBranch reports whether dir is a git linked worktree (its .git
// is a file, not a directory) and returns the checked-out branch. The main
// checkout and plain directories return ok=false so Destroy never deletes them.
func linkedWorktreeBranch(dir string) (branch string, ok bool) {
	candidate := filepath.Clean(dir)
	for {
		info, err := os.Stat(filepath.Join(candidate, ".git"))
		if err == nil {
			if info.IsDir() {
				return "", false
			}
			branch, err = (gitx.Repository{Path: candidate}).CheckedOutBranch()
			if err != nil {
				return "", true // linked worktree whose branch can't be resolved: drop dir, skip branch
			}
			if branch == "" || branch == "HEAD" {
				return "", true // detached HEAD: remove dir, skip branch
			}
			return branch, true
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return "", false
		}
		candidate = parent
	}
}

// worktreeMainPath returns the main worktree path for the repo containing dir,
// so a worktree is never removed from within itself. It falls back to dir.
func worktreeMainPath(dir string) string {
	// A stale linked worktree may no longer exist. Walk its parents to find the
	// repository that still owns its metadata, then remove it from there.
	candidate := dir
	for {
		output, err := (gitx.Repository{Path: candidate}).WorktreePorcelain()
		if err == nil {
			for _, raw := range strings.Split(output, "\n") {
				line := strings.TrimSpace(raw)
				if strings.HasPrefix(line, "worktree ") {
					return strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
				}
			}
			return candidate
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return dir
		}
		candidate = parent
	}
}

// detectDestroyPlan builds the destroy plan for an entry. It discovers linked
// worktrees from the entry path and every visible window directory, allowing a
// composed worktree session to remove all of its repositories together.
func detectDestroyPlan(e entry) destroyPlan {
	target := domain.DestroyTarget{
		Name: e.name, Saved: e.saved, Open: e.open, TabCount: len(e.tabs),
		Kind: e.kind, Path: e.path,
	}
	linked := make([]domain.LinkedWorktree, 0, 1)
	addLinked := func(path string) {
		if path == "" {
			return
		}
		candidate := filepath.Clean(path)
		for _, existing := range linked {
			if sameWorktreePath(candidate, existing.Path) {
				return
			}
		}
		branch, ok := linkedWorktreeBranch(candidate)
		if !ok {
			return
		}
		linked = append(linked, domain.LinkedWorktree{Path: candidate, Branch: branch})
	}
	if e.kind == "project" {
		addLinked(e.path)
	}
	for _, tab := range e.tabs {
		for _, window := range tab.windows {
			addLinked(window.cwd)
		}
	}
	target.LinkedWorktrees = linked
	plan := domain.PlanDestroy(target)
	return destroyPlan{
		entryName: plan.EntryName, closeSession: plan.CloseSession, tabCount: plan.TabCount,
		worktreePath: plan.WorktreePath, branch: plan.Branch, worktrees: plan.Worktrees, saved: plan.Saved,
	}
}

// runDestroy cascades a destroy plan: close the kitty session, close any
// windows still inside the worktree, force-removes the worktree (including
// local changes and untracked files), deletes the local branch, and deletes the
// saved record. Worktree removal runs from the repo's main worktree so a
// worktree is never removed from within itself.
func runDestroy(kitty, zoxide string, e entry, plan destroyPlan) tea.Cmd {
	return func() tea.Msg {
		if plan.closeSession {
			if args, err := closeArgs(e, row{tabIndex: -1, windowIndex: -1}); err == nil {
				if err := (kittyx.Client{Executable: kitty}).Run(args...); err != nil {
					return destroyMsg{err: fmt.Errorf("close session: %w", err)}
				}
			}
		}
		worktrees := plan.worktrees
		if len(worktrees) == 0 && plan.worktreePath != "" {
			worktrees = []domain.LinkedWorktree{{Path: plan.worktreePath, Branch: plan.branch}}
		}
		for _, worktree := range worktrees {
			ids, _ := worktreeWindowIDs(kitty, worktree.Path)
			for _, id := range ids {
				if err := (kittyx.Client{Executable: kitty}).CloseWindow(id); err != nil {
					return destroyMsg{err: fmt.Errorf("close worktree window %d: %w", id, err)}
				}
			}
			repoDir := worktreeMainPath(worktree.Path)
			repository := gitx.Repository{Path: repoDir}
			if err := repository.RemoveWorktree(worktree.Path, true); err != nil {
				return destroyMsg{err: err}
			}
			if worktree.Branch != "" {
				if err := repository.DeleteBranch(worktree.Branch); err != nil {
					return destroyMsg{err: err}
				}
			}
		}
		if plan.saved {
			if err := deleteSavedSession(e); err != nil {
				return destroyMsg{err: err}
			}
		}
		entries, err := loadEntries(kitty, zoxide)
		return destroyMsg{entries: entries, err: err}
	}
}

// runRemoveWorktree deletes the selected worktree via git. It first protects
// live Kitty windows from being left in a deleted working directory; force
// removal closes those windows, then removes the worktree.
func (m *model) runRemoveWorktree(force bool) tea.Cmd {
	r := m.closeRow
	sourceDir := m.worktreeDirectory(r)
	target, ok := m.worktreeForRow(r)
	if !ok {
		return func() tea.Msg {
			return worktreeRemoveMsg{dir: sourceDir, forceTried: force, err: fmt.Errorf("worktree is no longer available")}
		}
	}
	targetPath := target.path
	entryIndex, tabIndex, windowIndex := r.entryIndex, r.tabIndex, r.windowIndex
	kitty := m.kitty
	return func() tea.Msg {
		// The parent entry can be a stale saved or pruned worktree. Resolve the
		// repository inside the command so Git never runs during Update.
		repoDir := worktreeMainPath(targetPath)
		windowIDs, windowErr := worktreeWindowIDs(kitty, targetPath)
		if windowErr != nil {
			return worktreeRemoveMsg{dir: sourceDir, targetPath: targetPath, entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, forceTried: force, err: windowErr}
		}
		if len(windowIDs) > 0 && !force {
			return worktreeRemoveMsg{dir: sourceDir, targetPath: targetPath, entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, err: fmt.Errorf("%d Kitty window(s) are open in this worktree; press f to close them and force-remove", len(windowIDs))}
		}
		if force {
			for _, id := range windowIDs {
				if err := (kittyx.Client{Executable: kitty}).CloseWindow(id); err != nil {
					return worktreeRemoveMsg{dir: sourceDir, targetPath: targetPath, entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, forceTried: true, err: fmt.Errorf("close Kitty window %d: %w", id, err)}
				}
			}
		}
		if err := (gitx.Repository{Path: repoDir}).RemoveWorktree(targetPath, force); err != nil {
			return worktreeRemoveMsg{dir: sourceDir, targetPath: targetPath, entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, forceTried: force, err: err}
		}
		return worktreeRemoveMsg{dir: sourceDir, targetPath: targetPath, entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, forceTried: force}
	}
}

func (m model) entryIndexByKey(key string) int {
	for index := range m.entries {
		if m.entries[index].key == key {
			return index
		}
	}
	return -1
}

func (m model) resolveRenameTarget(target renameTarget) (entryIndex, tabIndex, windowIndex int) {
	if target.windowID != 0 {
		for entryIndex := range m.entries {
			for tabIndex := range m.entries[entryIndex].tabs {
				for windowIndex := range m.entries[entryIndex].tabs[tabIndex].windows {
					if m.entries[entryIndex].tabs[tabIndex].windows[windowIndex].id == target.windowID {
						return entryIndex, tabIndex, windowIndex
					}
				}
			}
		}
		return -1, -1, -1
	}
	if target.tabID != 0 {
		for entryIndex := range m.entries {
			for tabIndex := range m.entries[entryIndex].tabs {
				if m.entries[entryIndex].tabs[tabIndex].id == target.tabID {
					return entryIndex, tabIndex, -1
				}
			}
		}
		return -1, -1, -1
	}
	entryIndex = m.entryIndexByKey(target.entryKey)
	return entryIndex, -1, -1
}

//nolint:unparam // tab/window are intentionally -1: entry-level resolution. The triple mirrors resolveWorktreeTarget so all resolvers share one coordinate shape consumed uniformly by callers.
func (m model) resolveWorktreeDirectory(directory string) (entryIndex, tabIndex, windowIndex int) {
	for entryIndex := range m.entries {
		if m.entries[entryIndex].path == directory {
			return entryIndex, -1, -1
		}
	}
	return -1, -1, -1
}

// windowAt returns a pointer to the window at the given coordinates, or nil if
// any index is out of range or the coordinates do not address a window.
func (m *model) windowAt(entryIndex, tabIndex, windowIndex int) *windowItem {
	if entryIndex < 0 || entryIndex >= len(m.entries) {
		return nil
	}
	tabs := m.entries[entryIndex].tabs
	if tabIndex < 0 || tabIndex >= len(tabs) {
		return nil
	}
	windows := tabs[tabIndex].windows
	if windowIndex < 0 || windowIndex >= len(windows) {
		return nil
	}
	return &windows[windowIndex]
}

// closedEntryAt returns an entry-level worktree target only for a closed entry.
// Open entries must be inspected at window level to avoid guessing which repo a
// multi-tab session represents.
func (m *model) closedEntryAt(entryIndex, tabIndex, windowIndex int) *entry {
	if tabIndex >= 0 || windowIndex >= 0 || entryIndex < 0 || entryIndex >= len(m.entries) {
		return nil
	}
	e := &m.entries[entryIndex]
	if e.open {
		return nil
	}
	return e
}

// selectedWorktreeItems resolves the bulk-selected worktree paths to their
// worktreeItems via the tab project's full worktree list, so a search filter
// does not silently drop selections from a bulk action. Stale paths (worktrees
// removed since selection) are skipped.
func (m model) selectedWorktreeItems() []worktreeItem {
	if len(m.wtBulkSelected) == 0 {
		return nil
	}
	if m.worktreeFilterEntryIndex < 0 || m.worktreeFilterEntryIndex >= len(m.entries) {
		return nil
	}
	var items []worktreeItem
	for _, wt := range m.entries[m.worktreeFilterEntryIndex].worktrees {
		if m.wtBulkSelected[wt.path] {
			items = append(items, wt)
		}
	}
	return items
}

func (m model) focusedWorktreePath() string {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return ""
	}
	selected := m.rows[m.cursor]
	if selected.section == "wt-filter" && selected.wt >= 0 && selected.wt < len(m.worktreeFilterRows) {
		return m.worktreeFilterRows[selected.wt].worktree.path
	}
	return ""
}

func (m *model) restoreFocusedWorktree(path string) {
	if path == "" {
		return
	}
	for index, candidate := range m.rows {
		if candidate.section == "wt-filter" && candidate.wt >= 0 && candidate.wt < len(m.worktreeFilterRows) && m.worktreeFilterRows[candidate.wt].worktree.path == path {
			m.cursor = index
			return
		}
	}
}

// worktreeForRow resolves a Worktrees-surface row by its stable path. The
// visible list can be filtered or re-sorted independently of the full cache.
func (m model) worktreeForRow(r row) (worktreeItem, bool) {
	if r.worktreePath != "" && r.entryIndex >= 0 && r.entryIndex < len(m.entries) {
		for _, wt := range m.entries[r.entryIndex].worktrees {
			if wt.path == r.worktreePath {
				return wt, true
			}
		}
		// The list may have been refetched between the keypress and this call;
		// the path is still valid for removal, so synthesize a minimal item.
		return worktreeItem{path: r.worktreePath, branch: filepath.Base(r.worktreePath)}, true
	}
	return worktreeItem{}, false
}

func fetchWorktrees(dir string, entryIndex, tabIndex, windowIndex int) tea.Cmd {
	return func() tea.Msg {
		repository := gitx.Repository{Path: dir}
		output, err := repository.WorktreePorcelain()
		if err != nil {
			return worktreeListMsg{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, dir: dir, err: err}
		}
		worktrees := parseWorktreePorcelain(output)
		defaultBranch := repository.DefaultBranch()
		if defaultBranch == "" && len(worktrees) > 0 {
			defaultBranch = worktrees[0].branch
		}
		repoKey := repositoryCacheKey(dir)
		cachedStatuses, _ := loadPRStatusCache(repoKey)
		for i := range worktrees {
			if worktrees[i].path == dir {
				worktrees[i].current = true
			}
			worktrees[i].isDefault = worktrees[i].branch == defaultBranch
			worktrees[i].prRepoKey = repoKey
			pullRequest, exact := matchPullRequest(cachedStatuses, worktrees[i].branch, worktrees[i].head)
			worktrees[i].prStatus = pullRequest.Status
			worktrees[i].prURL = pullRequest.URL
			worktrees[i].prNumber = pullRequest.Number
			worktrees[i].prExact = exact
		}
		sortWorktreeItems(worktrees)
		return worktreeListMsg{entryIndex: entryIndex, tabIndex: tabIndex, windowIndex: windowIndex, dir: dir, worktrees: worktrees}
	}
}

// fetchWorktreeSyncStatuses enriches an already-visible worktree list with
// dirty/ahead/behind state. Keeping these per-worktree git status calls out of
// fetchWorktrees lets large repositories paint after one worktree-list command.
func fetchWorktreeSyncStatuses(dir string, worktrees []worktreeItem) tea.Cmd {
	paths := make([]string, 0, len(worktrees))
	for _, worktree := range worktrees {
		if worktree.path != "" {
			paths = append(paths, worktree.path)
		}
	}
	return func() tea.Msg {
		statuses := make(map[string]worktreeSyncState, len(paths))
		for _, path := range paths {
			dirty, ahead, behind, changes := worktreeSyncStatus(path)
			statuses[path] = worktreeSyncState{
				dirty: dirty, ahead: ahead, behind: behind, changes: changes,
			}
		}
		return worktreeSyncMsg{dir: dir, statuses: statuses}
	}
}

func parseWorktreePorcelain(output string) []worktreeItem {
	return fromDomainWorktrees(domain.ParseWorktreePorcelain(output))
}

// worktreeSyncStatus reports whether a worktree has uncommitted changes and how
// far its branch has diverged from its upstream. A single `git status -sb
// --porcelain` call yields both: the tracking header carries ahead/behind, and
// any following line is an uncommitted change.
func worktreeSyncStatus(path string) (dirty bool, ahead, behind int, changes []string) {
	output, err := (gitx.Repository{Path: path}).StatusPorcelain()
	if err != nil {
		return false, 0, 0, nil
	}
	return parseWorktreeStatus(output)
}

// parseWorktreeStatus decodes `git status -sb --porcelain` output: the tracking
// header yields ahead/behind, and every following line is an uncommitted change
// (kept verbatim so its status column survives for the detail panel).
func parseWorktreeStatus(output string) (dirty bool, ahead, behind int, changes []string) {
	return domain.ParseWorktreeStatus(output)
}

// parseAheadBehind decodes a "[ahead N, behind M]" tracking segment.
func parseAheadBehind(segment string) (ahead, behind int) {
	return domain.ParseAheadBehind(segment)
}
