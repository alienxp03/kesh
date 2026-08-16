package workspace

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/alienxp03/kesh/internal/workspace/git"
	"github.com/alienxp03/kesh/internal/workspace/run"
)

// FirstWorkspace is the repository and recipe context used by headless tree
// commands. It deliberately resolves only the first .kesh.yaml workspace.
type FirstWorkspace struct {
	ConfigPath       string
	Name             string
	RepoRoot         string
	FolderRoot       string
	WorkspaceRelPath string
}

// LoadFirstWorkspace requires a .kesh.yaml and resolves its first workspace.
// It is shared by headless create, destroy, and merge commands so they all
// agree on repository and configuration discovery.
func LoadFirstWorkspace(ctx context.Context, cwd string, env map[string]string, runner run.Runner) (FirstWorkspace, error) {
	selection, err := resolveSelection(ctx, CreateOptions{
		Cwd:    cwd,
		Env:    env,
		Runner: runner,
		Mode:   ModeSingle,
	}, false, nil, true)
	if err != nil {
		return FirstWorkspace{}, err
	}
	if len(selection.Workspaces) != 1 {
		return FirstWorkspace{}, fmt.Errorf("headless tree commands require exactly one workspace")
	}
	workspace := selection.Workspaces[0]
	return FirstWorkspace{
		ConfigPath:       selection.ConfigPath,
		Name:             workspace.Name,
		RepoRoot:         workspace.RepoRoot,
		FolderRoot:       workspace.WorkspaceRoot,
		WorkspaceRelPath: workspace.WorkspaceRelPath,
	}, nil
}

// WorktreeFolder returns the configured workspace folder inside a linked
// worktree. It is where the workspace-specific .kesh.env is written.
func (workspace FirstWorkspace) WorktreeFolder(worktreeRoot string) string {
	if workspace.WorkspaceRelPath == "" {
		return worktreeRoot
	}
	return filepath.Join(worktreeRoot, workspace.WorkspaceRelPath)
}

// MainWorktreePath returns the primary checkout for the repository containing
// cwd. Git operations that remove a linked worktree must run from this path,
// never from the linked worktree being removed.
func MainWorktreePath(ctx context.Context, cwd string, runner run.Runner) (string, error) {
	return git.MainWorktreePath(ctx, cwd, runner)
}
