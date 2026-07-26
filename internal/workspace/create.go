// Package workspace is the recipe-driven worktree + Kitty layout engine,
// ported from wktree. It owns .kesh.yaml parsing, worktree creation, the
// file/env/hook setup pipeline, and Kitty session rendering. Create is the
// single entry point used by the rest of kesh.
package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

// CreateOptions configures a recipe-driven worktree creation.
type CreateOptions struct {
	// Cwd is the repository to create the worktree in. Usually the project
	// under the kesh cursor.
	Cwd string
	// Branch is the new worktree's branch name.
	Branch string
	// From is the optional start point (defaults to HEAD).
	From string
	// Home overrides the worktree root; empty means use .kesh.yaml's
	// worktree_dir or the provider default.
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

// Create runs the full recipe-driven flow for the Kitty provider: resolve the
// .kesh.yaml selection, create a worktree per selected workspace, run the
// setup pipeline (copy/symlink, ports, set_env, post_create hooks), optionally
// add to zoxide, and open the assembled Kitty session. Mode is "single",
// "all", or "selected"; selectedNames is honored only for "selected".
func Create(ctx context.Context, opts CreateOptions) error {
	if opts.Runner == nil {
		opts.Runner = run.DefaultRunner{}
	}
	stdout, stderr, env := normalizeIO(opts.Stdout, opts.Stderr, opts.Env)
	opts.Env = env

	allWorkspaces := opts.Mode == "all"
	var selectedNames []string
	if opts.Mode == "selected" {
		selectedNames = opts.Selected
	}

	selection, err := resolveSelection(ctx, opts, allWorkspaces, selectedNames)
	if err != nil {
		return err
	}

	worktrees, err := createWorktrees(ctx, selection, opts)
	if err != nil {
		return err
	}
	if err := runSetup(ctx, selection, worktrees, stdout, stderr); err != nil {
		return err
	}

	pathsToAdd := make([]string, 0, len(worktrees))
	for _, wt := range worktrees {
		pathsToAdd = append(pathsToAdd, workspacePath(wt.Spec, wt.Worktree.WorktreePath))
	}
	addToZoxide(ctx, pathsToAdd, opts.Runner)

	windows := kittyWindows(worktrees)
	_, err = kitty.OpenLayout(ctx, layout.OpenOptions{
		Mode:        layout.ModeWindow,
		SessionName: sessionName(selection, opts.Branch),
		Windows:     windows,
		Env:         opts.Env,
		CacheDir:    cacheDir(opts.Env),
		Runner:      opts.Runner,
	})
	return err
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
func resolveSelection(ctx context.Context, opts CreateOptions, allWorkspaces bool, selectedNames []string) (selection, error) {
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
	configPath, _, err := config.FindProjectPath(opts.Cwd, configRepoRoot)
	if err != nil {
		return selection{}, err
	}
	projectConfig, err := config.LoadProjectFile(configPath, homeDir)
	if err != nil {
		return selection{}, err
	}
	configDir := filepath.Dir(configPath)
	if len(projectConfig.Workspaces) == 0 {
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
		worktreeHome = projectConfig.WorktreeDir
	}
	if worktreeHome != "" {
		worktreeHome, err = config.ExpandConfiguredPath(worktreeHome, homeDir, configDir)
		if err != nil {
			return selection{}, fmt.Errorf("worktree_dir: %w", err)
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

func createWorktrees(ctx context.Context, sel selection, opts CreateOptions) ([]worktreeWithSpec, error) {
	worktrees := make([]worktreeWithSpec, 0, len(sel.Workspaces))
	for _, ws := range sel.Workspaces {
		target, err := git.ResolveCreateWorktree(ctx, git.CreateOptions{
			Branch:     opts.Branch,
			From:       opts.From,
			HomeOption: sel.WorktreeHome,
			Cwd:        ws.RepoRoot,
			Runner:     opts.Runner,
		})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ws.Name, err)
		}
		worktree, err := git.CreateResolvedWorktree(ctx, target, opts.Runner)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ws.Name, err)
		}
		worktrees = append(worktrees, worktreeWithSpec{Spec: ws, Worktree: worktree})
	}
	return worktrees, nil
}

// runSetup executes the copy/symlink, port-randomization, set_env, context-env,
// and post_create pipeline. Failures are surfaced as an error containing the
// captured stderr so the caller can report what went wrong.
func runSetup(ctx context.Context, sel selection, worktrees []worktreeWithSpec, stdout io.Writer, stderr io.Writer) error {
	contexts := workspaceContexts(worktrees)
	baseLogger := setup.Logger{Stdout: stdout, Stderr: stderr}
	var problems []string
	for _, wt := range worktrees {
		files := config.WorkspaceFiles(sel.Config, wt.Spec.Config)
		hooks := config.WorkspaceHooks(sel.Config, wt.Spec.Config)
		logger := baseLogger
		if sel.AllWorkspaces {
			logger.Prefix = wt.Spec.Name
		}
		plan := setup.NewPlan(
			wt.Spec.WorkspaceRoot,
			workspacePath(wt.Spec, wt.Worktree.WorktreePath),
			wt.Spec.Name, "", files, hooks,
			wt.Spec.Config.RandomizePorts, wt.Spec.Config.SetEnv,
			wt.Worktree.Reused, contexts[wt.Spec.Name],
		)
		if status := setup.RunPrepare(plan, logger); status != 0 {
			problems = append(problems, fmt.Sprintf("%s: prepare failed", wt.Spec.Name))
		}
		if status := setup.SetEnvFiles(plan, logger); status != 0 {
			problems = append(problems, fmt.Sprintf("%s: set_env failed", wt.Spec.Name))
		}
		if status := setup.WriteContextEnvLogged(plan, logger); status != 0 {
			problems = append(problems, fmt.Sprintf("%s: context env failed", wt.Spec.Name))
		}
		if status := setup.RunPostCreate(ctx, plan, logger, nil); status != 0 {
			problems = append(problems, fmt.Sprintf("%s: post_create failed", wt.Spec.Name))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("setup failed: %s", strings.Join(problems, "; "))
	}
	return nil
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

func workspaceContexts(worktrees []worktreeWithSpec) map[string]setup.Context {
	workspacePaths := map[string]string{}
	for _, wt := range worktrees {
		workspacePaths[wt.Spec.Name] = workspacePath(wt.Spec, wt.Worktree.WorktreePath)
	}
	contexts := map[string]setup.Context{}
	for _, wt := range worktrees {
		contexts[wt.Spec.Name] = setup.Context{WorkspacePaths: workspacePaths}
	}
	return contexts
}

func kittyWindows(worktrees []worktreeWithSpec) []layout.Window {
	windows := make([]layout.Window, 0, len(worktrees))
	for _, wt := range worktrees {
		windows = append(windows, layout.Window{
			Name:         nameComponent(wt.Spec.Name),
			WorktreePath: workspacePath(wt.Spec, wt.Worktree.WorktreePath),
			Commands:     paneCommands(config.WorkspacePanes(wt.Spec.Config)),
		})
	}
	return windows
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

func sessionName(sel selection, branch string) string {
	branchSlug, err := paths.BranchSlug(branch)
	if err != nil {
		branchSlug = "branch"
	}
	ownerName, repoName := repoSlugParts(sel.ConfigRepoSlug)
	branchName := nameComponent(branchSlug)
	configured := sel.Config.Terminal
	if strings.TrimSpace(configured.SessionName) == "" {
		return repoName + "/" + branchName
	}
	rendered := renderSessionName(configured.SessionName, sel.ConfigDir, ownerName, repoName, branchName)
	return joinNameSegments(rendered)
}

func repoSlugParts(repoSlug string) (string, string) {
	cleaned := filepath.Clean(repoSlug)
	repo := nameComponent(filepath.Base(cleaned))
	owner := nameComponent(filepath.Base(filepath.Dir(cleaned)))
	if owner == "" {
		owner = "kesh"
	}
	return owner, repo
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

func renderSessionName(template, configDir, ownerName, repoName, branchName string) string {
	remaining := template
	var output strings.Builder
	for {
		start := strings.Index(remaining, "${")
		if start < 0 {
			output.WriteString(remaining)
			return output.String()
		}
		output.WriteString(remaining[:start])
		afterStart := remaining[start+2:]
		end := strings.Index(afterStart, "}")
		if end < 0 {
			output.WriteString(remaining[start:])
			return output.String()
		}
		reference := afterStart[:end]
		switch {
		case reference == "owner":
			output.WriteString(ownerName)
		case reference == "repo":
			output.WriteString(repoName)
		case reference == "branch":
			output.WriteString(branchName)
		case reference == "dir":
			output.WriteString(dirName(configDir, 0))
		case strings.HasPrefix(reference, "dir:"):
			if depth, err := strconv.Atoi(strings.TrimPrefix(reference, "dir:")); err == nil {
				output.WriteString(dirName(configDir, depth))
			} else {
				output.WriteString("${" + reference + "}")
			}
		default:
			output.WriteString("${" + reference + "}")
		}
		remaining = afterStart[end+1:]
	}
}

func dirName(configDir string, depth int) string {
	dir := filepath.Clean(configDir)
	for index := 0; index < depth; index++ {
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	return nameComponent(filepath.Base(dir))
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
