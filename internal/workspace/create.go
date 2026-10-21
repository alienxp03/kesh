// Package workspace opens .kesh.yaml folder layouts and plain Worktrunk
// worktree sessions. Worktrunk alone owns worktree paths, setup, and hooks.
package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/alienxp03/kesh/internal/workspace/config"
	"github.com/alienxp03/kesh/internal/workspace/git"
	"github.com/alienxp03/kesh/internal/workspace/kitty"
	"github.com/alienxp03/kesh/internal/workspace/layout"
	"github.com/alienxp03/kesh/internal/workspace/paths"
	"github.com/alienxp03/kesh/internal/workspace/run"
	"github.com/alienxp03/kesh/internal/workspace/setup"
	"github.com/alienxp03/kesh/internal/workspace/zoxide"
)

// Config is the parsed .kesh.yaml project configuration.
type Config = config.Config

// Workspace and PaneCommand are re-exported so callers can render recipe
// details without importing the config sub-package directly.
type Workspace = config.Workspace
type PaneCommand = config.PaneCommand

// CreateOptions configures Worktrunk creation. Home, Mode, Selected, and
// ShellRunner remain for source compatibility but are ignored. Recipes apply
// only to OpenOptions folder layouts.
type CreateOptions struct {
	// Cwd is the repository to create the worktree in. Usually the project
	// under the kesh cursor.
	Cwd string
	// Branch is the new worktree's branch name.
	Branch string
	// From overrides Worktrunk's default base for a new branch.
	From string
	// Home is ignored: Worktrunk owns the path.
	Home string
	// Mode is ModeSingle, ModeAll, or ModeSelected.
	Mode string
	// Selected names the workspaces to create when Mode is ModeSelected.
	Selected []string
	// Env is the process environment used for Kitty remote control and cache
	// resolution. Nil falls back to the current process environment.
	Env map[string]string
	// Runner executes git/kitty/zoxide commands; nil uses run.DefaultRunner.
	Runner run.Runner
	// Stdout and Stderr receive setup progress output; nil defaults to the
	// process streams.
	Stdout, Stderr io.Writer
	// ShellRunner executes post-create hooks. Nil uses the default process
	// runner; headless callers can provide a stream-aware runner.
	ShellRunner setup.ShellRunner
}

// CreateResult describes a Worktrunk worktree and its selected project folder.
// ConfigPath stays empty because no recipe is read.
type CreateResult struct {
	Branch        string
	WorkspaceName string
	RepoRoot      string
	WorktreePath  string
	WorkspacePath string
	ConfigPath    string
}

// Load finds and parses the .kesh.yaml between cwd and its Git root. It returns
// the parsed config, the resolved config path, and an error. A missing config
// is not an error: it returns a zero Config and the default path.
func Load(cwd string, runner run.Runner) (*Config, string, error) {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	ctx := context.Background()
	repoRoot, err := git.RepoRoot(ctx, cwd, runner)
	if err != nil {
		return nil, "", err
	}
	configPath, _, err := config.FindProjectPath(cwd, repoRoot)
	if err != nil {
		return nil, "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	cfg, err := config.LoadProjectFile(configPath, home)
	if err != nil {
		return nil, "", err
	}
	return &cfg, configPath, nil
}

// Create switches/creates through wt and opens a plain Kitty session.
// Legacy recipe selection and setup options are ignored for worktrees.
func Create(ctx context.Context, opts CreateOptions) error {
	_, selection, worktrees, err := prepareSingleCreate(ctx, opts)
	if err != nil {
		return err
	}
	return openWorkspaceLayout(ctx, sessionName(selection, opts.Branch), "", kittyWindows(worktrees), opts.Env, opts.Runner)
}

// CreateAndOpen switches/creates through wt and opens a plain Kitty session.
func CreateAndOpen(ctx context.Context, opts CreateOptions) (CreateResult, error) {
	result, selection, worktrees, err := prepareSingleCreate(ctx, opts)
	if err != nil {
		return CreateResult{}, err
	}
	if err := openWorkspaceLayout(ctx, sessionName(selection, opts.Branch), "", kittyWindows(worktrees), opts.Env, opts.Runner); err != nil {
		return CreateResult{}, err
	}
	return result, nil
}

// CreateAndOpenWindow opens a wt worktree in the current Kitty tab.
func CreateAndOpenWindow(ctx context.Context, opts CreateOptions) (CreateResult, error) {
	result, _, worktrees, err := prepareSingleCreate(ctx, opts)
	if err != nil {
		return CreateResult{}, err
	}
	_, _, env := normalizeIO(nil, nil, opts.Env)
	runner := opts.Runner
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	if err := kitty.OpenWindow(ctx, workspacePath(worktrees[0].Spec, worktrees[0].Worktree.WorktreePath), env, runner); err != nil {
		return CreateResult{}, err
	}
	return result, nil
}

// CreateHeadless switches/creates through wt without opening Kitty.
func CreateHeadless(ctx context.Context, opts CreateOptions) (CreateResult, error) {
	result, _, _, err := prepareSingleCreate(ctx, opts)
	return result, err
}

func prepareSingleCreate(ctx context.Context, opts CreateOptions) (CreateResult, selection, []worktreeWithSpec, error) {
	if opts.Runner == nil {
		opts.Runner = run.DefaultRunner{}
	}
	return prepareWorktrunkCreate(ctx, opts)
}

// Mode values for CreateOptions.Mode.
const (
	ModeSingle   = "single"
	ModeAll      = "all"
	ModeSelected = "selected"
)

type worktreeSpec struct {
	Name             string
	RepoRoot         string
	WorkspaceRoot    string
	WorkspaceRelPath string
	Config           config.Workspace
}

type selection struct {
	Config         config.Config
	ConfigPath     string
	ConfigDir      string
	ConfigRepoRoot string
	ConfigRepoSlug string
	WorktreeHome   string
	Workspaces     []worktreeSpec
	AllWorkspaces  bool
}

type worktreeWithSpec struct {
	Spec     worktreeSpec
	Worktree git.Worktree
}

// resolveSelection mirrors wktree's resolveWorkspaceSelection: locate
// .kesh.yaml, pick workspaces per mode, resolve each repo root, and dedupe.
func resolveSelection(ctx context.Context, opts CreateOptions, allWorkspaces bool, selectedNames []string, requireConfig bool) (selection, error) {
	runner := opts.Runner
	configRepoRoot, err := git.RepoRoot(ctx, opts.Cwd, runner)
	if err != nil {
		return selection{}, err
	}
	configRepoSlug, err := git.RepoSlug(ctx, configRepoRoot, runner)
	if err != nil {
		return selection{}, err
	}
	homeDir, err := homeDir(opts.Env)
	if err != nil {
		return selection{}, err
	}
	configPath, configFound, err := config.FindProjectPath(opts.Cwd, configRepoRoot)
	if err != nil {
		return selection{}, err
	}
	if requireConfig && !configFound {
		return selection{}, fmt.Errorf("no .kesh.yaml found; run `kesh init` first")
	}
	projectConfig, err := config.LoadProjectFile(configPath, homeDir)
	if err != nil {
		return selection{}, err
	}
	configDir := filepath.Dir(configPath)
	if len(projectConfig.Workspaces) == 0 {
		if requireConfig {
			return selection{}, fmt.Errorf(".kesh.yaml has no workspaces")
		}
		projectConfig.Workspaces = []config.Workspace{{Name: defaultWorkspaceName(configRepoRoot, configRepoSlug, configDir)}}
	}

	workspaces := projectConfig.Workspaces
	switch {
	case len(selectedNames) > 0:
		workspaces = selectWorkspaces(workspaces, selectedNames)
		if missing := missingWorkspaceNames(workspaces, selectedNames); len(missing) > 0 {
			available := make([]string, 0, len(projectConfig.Workspaces))
			for _, ws := range projectConfig.Workspaces {
				available = append(available, ws.Name)
			}
			return selection{}, fmt.Errorf("unknown workspace: %s; available: %s", strings.Join(missing, ", "), strings.Join(available, ", "))
		}
	case allWorkspaces:
	default:
		workspaces = workspaces[:1]
	}
	multi := len(workspaces) > 1 || allWorkspaces

	selected := make([]worktreeSpec, 0, len(workspaces))
	seenRepos := map[string]string{}
	for _, ws := range workspaces {
		repoPath := configDir
		if ws.Repo != "" {
			repoPath, err = config.ExpandConfiguredPath(ws.Repo, homeDir, configDir)
			if err != nil {
				return selection{}, fmt.Errorf("%s repo: %w", ws.Name, err)
			}
		}
		repoRoot, err := git.RepoRoot(ctx, repoPath, runner)
		if err != nil {
			return selection{}, fmt.Errorf("%s repo: %w", ws.Name, err)
		}
		if previous, ok := seenRepos[cleanAbsPath(repoRoot)]; ok {
			return selection{}, fmt.Errorf("workspaces %q and %q resolve to the same repo: %s", previous, ws.Name, repoRoot)
		}
		seenRepos[cleanAbsPath(repoRoot)] = ws.Name
		spec, err := newWorkspaceSpec(ws.Name, repoRoot, repoPath, ws)
		if err != nil {
			return selection{}, fmt.Errorf("%s repo: %w", ws.Name, err)
		}
		selected = append(selected, spec)
	}

	worktreeHome := opts.Home
	if worktreeHome == "" {
		worktreeHome = projectConfig.Worktree.Dir
	}
	if worktreeHome != "" {
		worktreeHome, err = config.ExpandConfiguredPath(worktreeHome, homeDir, configDir)
		if err != nil {
			return selection{}, fmt.Errorf("worktree.dir: %w", err)
		}
	}

	return selection{
		Config:         projectConfig,
		ConfigPath:     configPath,
		ConfigDir:      configDir,
		ConfigRepoRoot: configRepoRoot,
		ConfigRepoSlug: configRepoSlug,
		WorktreeHome:   worktreeHome,
		Workspaces:     selected,
		AllWorkspaces:  multi,
	}, nil
}

// --- selection helpers ---

func addToZoxide(ctx context.Context, paths []string, runner run.Runner) {
	if zoxide.Available(ctx, runner) != nil {
		return
	}
	_ = zoxide.Add(ctx, paths, runner)
}

func selectWorkspaces(workspaces []config.Workspace, selectedNames []string) []config.Workspace {
	selected := make(map[string]bool, len(selectedNames))
	for _, name := range selectedNames {
		selected[name] = true
	}
	defaults := make([]config.Workspace, 0, len(selectedNames))
	for _, ws := range workspaces {
		if selected[ws.Name] {
			defaults = append(defaults, ws)
		}
	}
	return defaults
}

func missingWorkspaceNames(matched []config.Workspace, requested []string) []string {
	present := make(map[string]bool, len(matched))
	for _, ws := range matched {
		present[ws.Name] = true
	}
	seen := make(map[string]bool, len(requested))
	var missing []string
	for _, name := range requested {
		if present[name] || seen[name] {
			continue
		}
		seen[name] = true
		missing = append(missing, name)
	}
	return missing
}

func newWorkspaceSpec(name string, repoRoot string, workspaceRoot string, ws config.Workspace) (worktreeSpec, error) {
	absoluteRoot := cleanAbsPath(repoRoot)
	absoluteWorkspace := cleanAbsPath(workspaceRoot)
	relative, insideRoot, err := relativePathWithinRoot(absoluteRoot, absoluteWorkspace)
	if err != nil {
		return worktreeSpec{}, err
	}
	if !insideRoot {
		return worktreeSpec{}, fmt.Errorf("path must be inside git repo root: %s", workspaceRoot)
	}
	return worktreeSpec{
		Name:             name,
		RepoRoot:         repoRoot,
		WorkspaceRoot:    absoluteWorkspace,
		WorkspaceRelPath: relative,
		Config:           ws,
	}, nil
}

func relativePathWithinRoot(root string, path string) (string, bool, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return "", false, err
	}
	if relative == "." {
		return "", true, nil
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
		return relative, true, nil
	}
	parts := []string{}
	for current := path; ; current = filepath.Dir(current) {
		if samePath(current, root) {
			return filepath.Join(parts...), true, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false, nil
		}
		parts = append([]string{filepath.Base(current)}, parts...)
	}
}

func defaultWorkspaceName(repoRoot string, repoSlug string, configDir string) string {
	if !samePath(configDir, repoRoot) {
		name := strings.TrimSpace(filepath.Base(filepath.Clean(configDir)))
		if name != "" && name != "." && name != string(filepath.Separator) {
			return name
		}
	}
	return repoSlug
}

// --- layout helpers ---

func kittyWindows(worktrees []worktreeWithSpec) []layout.Window {
	windows := make([]layout.Window, 0, len(worktrees))
	for _, wt := range worktrees {
		windows = append(windows, workspaceWindow(
			wt.Spec.Name,
			workspacePath(wt.Spec, wt.Worktree.WorktreePath),
			config.WorkspacePanes(wt.Spec.Config),
		))
	}
	return windows
}

// workspaceWindow is shared by native folder launches and worktree launches so
// both flows use the workspace name for Kitty tabs and the same pane mapping.
func workspaceWindow(name, path string, commands []config.PaneCommand) layout.Window {
	return layout.Window{
		Name:         nameComponent(name),
		WorktreePath: path,
		Commands:     paneCommands(commands),
	}
}

// openWorkspaceLayout is the common terminal path for opening current folders
// and newly created worktrees. Callers only resolve the session name and each
// workspace directory; tab names and panes have identical semantics.
func openWorkspaceLayout(ctx context.Context, sessionName, sessionFile string, windows []layout.Window, env map[string]string, runner run.Runner) error {
	_, err := kitty.OpenLayout(ctx, layout.OpenOptions{
		Mode:        layout.ModeWindow,
		SessionName: sessionName,
		SessionFile: sessionFile,
		Windows:     windows,
		Env:         env,
		CacheDir:    cacheDir(env),
		Runner:      runner,
	})
	return err
}

func paneCommands(commands []config.PaneCommand) []layout.PaneCommand {
	converted := make([]layout.PaneCommand, 0, len(commands))
	for _, command := range commands {
		converted = append(converted, layout.PaneCommand{
			Commands:   append([]string(nil), command.Commands...),
			Split:      command.Split,
			Focus:      command.Focus,
			Zoom:       command.Zoom,
			Size:       command.Size,
			Percentage: command.Percentage,
		})
	}
	return converted
}

func workspacePath(ws worktreeSpec, worktreePath string) string {
	if ws.WorkspaceRelPath == "" {
		return worktreePath
	}
	return filepath.Join(worktreePath, ws.WorkspaceRelPath)
}

// --- session naming ---

func sessionName(_ selection, branch string) string {
	branchSlug, err := paths.BranchSlug(branch)
	if err != nil {
		branchSlug = "branch"
	}
	return nameComponent(branchSlug)
}

func repoName(repoSlug string) string {
	return nameComponent(filepath.Base(filepath.Clean(repoSlug)))
}

var unsafeNameChars = regexp.MustCompile(`[^A-Za-z0-9._/-]+`)

// nameComponent sanitizes a single path/name segment for use as a Kitty tab
// or session-name segment.
func nameComponent(value string) string {
	name := strings.ReplaceAll(value, ":", "-")
	name = unsafeNameChars.ReplaceAllString(name, "-")
	name = strings.Trim(name, "-_./ ")
	return name
}

// joinNameSegments sanitizes each "/"-separated segment of a session name.
func joinNameSegments(value string) string {
	parts := strings.Split(value, "/")
	for index, part := range parts {
		parts[index] = nameComponent(part)
	}
	return strings.Join(parts, "/")
}

// --- path/env helpers ---

func cleanAbsPath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(absolute)
}

func evalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return resolved
}

func samePath(left, right string) bool {
	leftAbs := cleanAbsPath(left)
	rightAbs := cleanAbsPath(right)
	if leftAbs == rightAbs {
		return true
	}
	leftEval := evalPath(leftAbs)
	rightEval := evalPath(rightAbs)
	if leftEval != "" && leftEval == rightEval {
		return true
	}
	leftInfo, leftErr := os.Stat(leftAbs)
	rightInfo, rightErr := os.Stat(rightAbs)
	return leftErr == nil && rightErr == nil && os.SameFile(leftInfo, rightInfo)
}

func homeDir(env map[string]string) (string, error) {
	if env != nil && env["HOME"] != "" {
		return env["HOME"], nil
	}
	return os.UserHomeDir()
}

func cacheDir(env map[string]string) string {
	if value := strings.TrimSpace(env["XDG_CACHE_HOME"]); value != "" {
		return value
	}
	if value := strings.TrimSpace(env["HOME"]); value != "" {
		return filepath.Join(value, ".cache")
	}
	return ""
}

func normalizeIO(stdout, stderr io.Writer, env map[string]string) (io.Writer, io.Writer, map[string]string) {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if env == nil {
		env = envMap(os.Environ())
	}
	return stdout, stderr, env
}

func envMap(environ []string) map[string]string {
	out := make(map[string]string, len(environ))
	for _, entry := range environ {
		if key, value, ok := strings.Cut(entry, "="); ok {
			out[key] = value
		}
	}
	return out
}
