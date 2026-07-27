package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alienxp03/kesh/internal/workspace/config"
	"github.com/alienxp03/kesh/internal/workspace/layout"
	"github.com/alienxp03/kesh/internal/workspace/run"
	"github.com/alienxp03/kesh/internal/workspace/setup"
)

// OpenOptions configures a layout launch on existing folders. Unlike
// CreateOptions it carries no branch or worktree home: nothing is checked out,
// and each selected workspace's existing folder becomes the launch target.
type OpenOptions struct {
	// Cwd is the project directory whose .kesh.yaml selects the workspaces.
	Cwd string
	// Mode is ModeSingle, ModeAll, or ModeSelected. An empty Mode uses the
	// first workspace by default.
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
// existing folder — no worktree is created. Worktree hooks do not run; the
// copy/symlink, set_env, context-env, and randomize_ports steps are skipped
// because they assume a fresh worktree and would mutate the base checkout.
// Mode is "single", "all", "selected", or "" (use the first workspace).
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

	pathsToAdd := make([]string, 0, len(folders))
	for _, f := range folders {
		pathsToAdd = append(pathsToAdd, f.Path)
	}
	addToZoxide(ctx, pathsToAdd, opts.Runner)

	return openWorkspaceLayout(ctx, sessionNameForOpen(sel, opts.SessionName), openWindows(folders), opts.Env, opts.Runner)
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

// runSetupForOpen writes the context env (.kesh.env) for each existing folder.
// The context env is required because launched panes source it. Worktree setup
// files, environment mutations, port randomization, and hooks are skipped so
// opening a current checkout has no setup side effects.
func runSetupForOpen(ctx context.Context, sel selection, folders []openFolder, stdout, stderr io.Writer) error {
	contexts := openContexts(folders)
	baseLogger := setup.Logger{Stdout: stdout, Stderr: stderr}
	var problems []string
	for _, f := range folders {
		logger := baseLogger
		if sel.AllWorkspaces {
			logger.Prefix = f.Spec.Name
		}
		plan := setup.NewPlan(
			f.Spec.WorkspaceRoot, f.Path, f.Spec.Name, "",
			config.Files{}, config.Hooks{}, nil, nil, false, contexts[f.Spec.Name],
		)
		if status := setup.WriteContextEnvLogged(plan, logger); status != 0 {
			problems = append(problems, fmt.Sprintf("%s: context env failed", f.Spec.Name))
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
		windows = append(windows, workspaceWindow(
			f.Spec.Name,
			f.Path,
			config.WorkspacePanes(f.Spec.Config),
		))
	}
	return windows
}

// sessionNameOpen derives the fallback Kitty session name for an existing
// repository when the launch form's Session field is empty.
func sessionNameOpen(sel selection) string {
	return repoName(sel.ConfigRepoSlug)
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
