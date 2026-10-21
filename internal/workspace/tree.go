package workspace

import (
	"context"
	"path/filepath"

	"github.com/alienxp03/kesh/internal/workspace/git"
	"github.com/alienxp03/kesh/internal/workspace/run"
)

// FirstWorkspace is the legacy-named repository context for headless commands.
// Recipe fields remain empty: worktree actions do not read .kesh.yaml.
type FirstWorkspace struct {
	ConfigPath       string
	Name             string
	RepoRoot         string
	FolderRoot       string
	WorkspaceRelPath string
}

// LoadFirstWorkspace resolves the current Git repository, without a recipe.
func LoadFirstWorkspace(ctx context.Context, cwd string, _ map[string]string, runner run.Runner) (FirstWorkspace, error) {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	root, err := git.RepoRoot(ctx, cwd, runner)
	if err != nil {
		return FirstWorkspace{}, err
	}
	return FirstWorkspace{Name: filepath.Base(root), RepoRoot: root, FolderRoot: cwd}, nil
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
