package workspace

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/alienxp03/kesh/internal/workspace/git"
	"github.com/alienxp03/kesh/internal/workspace/run"
	"github.com/alienxp03/kesh/internal/worktrunk"
)

// WorktrunkClient adapts the headless runner without changing hook trust policy.
func WorktrunkClient(ctx context.Context, cwd string, runner run.Runner) worktrunk.Client {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	return worktrunk.Client{Path: cwd, Execute: func(dir string, args []string) (string, string, error) {
		result := runner.Run(ctx, "wt", args, run.Options{Cwd: dir})
		err := result.Err
		if err == nil && result.ExitCode != 0 {
			err = fmt.Errorf("exit status %d", result.ExitCode)
		}
		return result.Stdout, result.Stderr, err
	}}
}

func prepareWorktrunkCreate(ctx context.Context, opts CreateOptions) (CreateResult, selection, []worktreeWithSpec, error) {
	root, err := git.RepoRoot(ctx, opts.Cwd, opts.Runner)
	if err != nil {
		return CreateResult{}, selection{}, nil, err
	}
	path, err := WorktrunkClient(ctx, opts.Cwd, opts.Runner).Switch(opts.Branch, opts.From)
	if err != nil {
		return CreateResult{}, selection{}, nil, err
	}
	relative, err := filepath.Rel(root, opts.Cwd)
	if err != nil {
		return CreateResult{}, selection{}, nil, err
	}
	folder := filepath.Join(path, relative)
	spec := worktreeSpec{Name: filepath.Base(root), RepoRoot: root, WorkspaceRoot: opts.Cwd, WorkspaceRelPath: relative}
	wt := worktreeWithSpec{Spec: spec, Worktree: git.Worktree{Branch: opts.Branch, RepoRoot: root, WorktreePath: path}}
	result := CreateResult{Branch: opts.Branch, WorkspaceName: spec.Name, RepoRoot: root, WorktreePath: path, WorkspacePath: folder}
	return result, selection{}, []worktreeWithSpec{wt}, nil
}
