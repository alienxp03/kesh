// Package zoxide integrates worktree paths with the zoxide directory database.
package zoxide

import (
	"context"
	"errors"

	"github.com/alienxp03/kesh/internal/workspace/run"
)

func Available(ctx context.Context, runner run.Runner) error {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	args := []string{"--version"}
	result := runner.Run(ctx, "zoxide", args, run.Options{})
	if result.Err != nil || result.ExitCode != 0 {
		return errors.New(run.FailureMessage("zoxide", args, result))
	}
	return nil
}

func Add(ctx context.Context, paths []string, runner run.Runner) error {
	if len(paths) == 0 {
		return nil
	}
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	args := append([]string{"add", "--"}, paths...)
	result := runner.Run(ctx, "zoxide", args, run.Options{})
	if result.Err != nil || result.ExitCode != 0 {
		return errors.New(run.FailureMessage("zoxide", args, result))
	}
	return nil
}
