package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alienxp03/kesh/internal/workspace/config"
	"github.com/alienxp03/kesh/internal/workspace/kitty"
	"github.com/alienxp03/kesh/internal/workspace/layout"
	"github.com/alienxp03/kesh/internal/workspace/run"
	"github.com/alienxp03/kesh/internal/workspace/setup"
	"github.com/alienxp03/kesh/internal/workspace/zoxide"
)

// OpenOptions configures a layout launch on existing folders. Unlike
// CreateOptions it carries no branch or worktree home: nothing is checked out,
// and each selected workspace's existing folder becomes the launch target.
type OpenOptions struct {
	// Cwd is the project directory whose .kesh.yaml selects the workspaces.
	Cwd string
	// Mode is ModeSingle, ModeAll, or ModeSelected. An empty Mode defers to
	// the recipe's workspace_mode, matching resolveSelection's default path.
	Mode string
	// Selected names the workspaces to launch when Mode is ModeSelected.
	Selected []string
	// SessionName overrides the Kitty session name. Empty defers to the
	// auto-derived name (recipe template, or the repo name).
	SessionName string
	// Env is the process environment used for Kitty remote control and cache
	// resolution. Nil falls back to the current process environment.
	Env map[string]string
	// Runner executes git/kitty/zoxide commands; nil uses run.DefaultRunner.
	Runner run.Runner
	// Stdout and Stderr receive setup progress output; nil defaults to the
	// process streams.
	Stdout, Stderr io.Writer
}

// Open launches the .kesh.yaml layout against each selected workspace's
// existing folder — no worktree is created. Only post_create hooks run; the
// copy/symlink, set_env, context-env, and randomize_ports steps are skipped
// because they assume a fresh worktree and would mutate the base checkout.
// Mode is "single", "all", "selected", or "" (defer to the recipe).
func Open(ctx context.Context, opts OpenOptions) error {
	if opts.Runner == nil {
		opts.Runner = run.DefaultRunner{}
	}
	stdout, stderr, env := normalizeIO(opts.Stdout, opts.Stderr, opts.Env)
	opts.Env = env

	allWorkspaces := opts.Mode == ModeAll
	var selectedNames []string
	if opts.Mode == ModeSelected {
		selectedNames = opts.Selected
	}

	sel, err := resolveSelection(ctx, openSelectionOpts(opts), allWorkspaces, selectedNames)
	if err != nil {
		return err
	}

	folders, err := openFolders(sel)
	if err != nil {
		return err
	}

	if err := runSetupForOpen(ctx, sel, folders, stdout, stderr); err != nil {
		return err
	}

	if sel.Config.Integrations.Zoxide {
		pathsToAdd := make([]string, 0, len(folders))
		for _, f := range folders {
			pathsToAdd = append(pathsToAdd, f.Path)
		}
		if err := zoxide.Add(ctx, pathsToAdd, opts.Runner); err != nil {
			return err
		}
	}

	windows := openWindows(folders)
	_, err = kitty.OpenLayout(ctx, layout.OpenOptions{
		Mode:        layout.ModeWindow,
		SessionName: sessionNameForOpen(sel, opts.SessionName),
		Windows:     windows,
		Env:         opts.Env,
		CacheDir:    cacheDir(opts.Env),
		Runner:      opts.Runner,
	})
	return err
}

// openSelectionOpts builds the CreateOptions shape resolveSelection consumes,
// limited to the fields it reads (Cwd, Env, Runner). Home and Branch are
// irrelevant when no worktree is created.
func openSelectionOpts(opts OpenOptions) CreateOptions {
	return CreateOptions{
		Cwd:    opts.Cwd,
		Env:    opts.Env,
		Runner: opts.Runner,
	}
}

type openFolder struct {
	Spec worktreeSpec
	Path string
}

// openFolders resolves each selected workspace's launch path — its existing
// folder (workspace root plus any configured relative path) — and rejects
// duplicates or missing directories so a typo in the recipe fails loudly.
func openFolders(sel selection) ([]openFolder, error) {
	folders := make([]openFolder, 0, len(sel.Workspaces))
	seen := map[string]string{}
	for _, ws := range sel.Workspaces {
		path := cleanAbsPath(workspacePath(ws, ws.WorkspaceRoot))
		if prev, ok := seen[path]; ok {
			return nil, fmt.Errorf("workspaces %q and %q resolve to the same folder: %s", prev, ws.Name, path)
		}
		seen[path] = ws.Name
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return nil, fmt.Errorf("%s: folder not found: %s", ws.Name, path)
		}
		folders = append(folders, openFolder{Spec: ws, Path: path})
	}
	return folders, nil
}

// runSetupForOpen writes the context env (.kesh.env) and runs post_create
// hooks against each workspace's existing folder. The context env is required:
// the launched panes source it. Files (copy/symlink), set_env, and
// randomize_ports are intentionally skipped — they target a fresh worktree and
// would mutate the base checkout. .kesh.env is a generated artifact that kesh's
// own git checks ignore, so the base repo stays clean.
func runSetupForOpen(ctx context.Context, sel selection, folders []openFolder, stdout, stderr io.Writer) error {
	contexts := openContexts(folders)
	baseLogger := setup.Logger{Stdout: stdout, Stderr: stderr}
	var problems []string
	for _, f := range folders {
		hooks := config.WorkspaceHooks(sel.Config, f.Spec.Config)
		logger := baseLogger
		if sel.AllWorkspaces {
			logger.Prefix = f.Spec.Name
		}
		plan := setup.NewPlan(
			f.Spec.WorkspaceRoot, f.Path, f.Spec.Name, "",
			config.Files{}, hooks, nil, nil, false, contexts[f.Spec.Name],
		)
		if status := setup.WriteContextEnvLogged(plan, logger); status != 0 {
			problems = append(problems, fmt.Sprintf("%s: context env failed", f.Spec.Name))
			continue
		}
		if status := setup.RunPostCreate(ctx, plan, logger, nil); status != 0 {
			problems = append(problems, fmt.Sprintf("%s: post_create failed", f.Spec.Name))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("setup failed: %s", strings.Join(problems, "; "))
	}
	return nil
}

func openContexts(folders []openFolder) map[string]setup.Context {
	paths := make(map[string]string, len(folders))
	for _, f := range folders {
		paths[f.Spec.Name] = f.Path
	}
	contexts := make(map[string]setup.Context, len(folders))
	for _, f := range folders {
		contexts[f.Spec.Name] = setup.Context{WorkspacePaths: paths}
	}
	return contexts
}

func openWindows(folders []openFolder) []layout.Window {
	windows := make([]layout.Window, 0, len(folders))
	for _, f := range folders {
		windows = append(windows, layout.Window{
			Name:         nameComponent(f.Spec.Name),
			WorktreePath: f.Path,
			Commands:     paneCommands(config.WorkspacePanes(f.Spec.Config)),
		})
	}
	return windows
}

// sessionNameOpen renders the Kitty session name without a branch segment.
// With no configured template it is just the repo name; a template keeps its
// shape but drops the empty ${branch} segment so "repo/" never appears.
func sessionNameOpen(sel selection) string {
	ownerName, repoName := repoSlugParts(sel.ConfigRepoSlug)
	configured := config.EffectiveTerminal(sel.Config)
	if strings.TrimSpace(configured.SessionName) == "" {
		return repoName
	}
	rendered := renderSessionName(configured.SessionName, sel.ConfigDir, ownerName, repoName, "")
	segments := strings.Split(rendered, "/")
	kept := segments[:0]
	for _, segment := range segments {
		if strings.TrimSpace(segment) != "" {
			kept = append(kept, segment)
		}
	}
	return joinNameSegments(strings.Join(kept, "/"))
}

// sessionNameForOpen picks the Kitty session name: a non-empty caller-supplied
// name wins (sanitized), otherwise the auto-derived name from sessionNameOpen.
// A supplied name that sanitizes away to nothing falls back to the derived name
// so the session file always has a valid identifier.
func sessionNameForOpen(sel selection, requested string) string {
	trimmed := strings.TrimSpace(requested)
	if trimmed == "" {
		return sessionNameOpen(sel)
	}
	sanitized := joinNameSegments(trimmed)
	if sanitized == "" {
		return sessionNameOpen(sel)
	}
	return sanitized
}
