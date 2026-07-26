// Package git owns Git repository discovery and mutation commands.
package git

import (
	"fmt"
	"strings"

	"github.com/alienxp03/kesh/internal/system"
)

type Repository struct {
	Path string
}

func (r Repository) output(args ...string) ([]byte, error) {
	return system.Command("git", append([]string{"-C", r.Path}, args...)...).Output()
}

func (r Repository) combinedOutput(args ...string) ([]byte, error) {
	return system.Command("git", append([]string{"-C", r.Path}, args...)...).CombinedOutput()
}

func CommandError(action string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message != "" {
		err = fmt.Errorf("%s: %s", err, message)
	}
	return fmt.Errorf("%s: %w", action, err)
}

func (r Repository) Root() (string, error) {
	output, err := r.output("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (r Repository) RemoteURL() (string, error) {
	output, err := r.output("remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (r Repository) WorktreePorcelain() (string, error) {
	output, err := r.combinedOutput("worktree", "list", "--porcelain")
	if err != nil {
		return "", CommandError("git worktree list", output, err)
	}
	return string(output), nil
}

func (r Repository) DefaultBranch() string {
	output, err := r.output("symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(output)), "origin/")
}

func (r Repository) OriginDefaultBranch() (string, error) {
	output, err := r.combinedOutput("symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}
	ref := strings.TrimSpace(string(output))
	if strings.HasPrefix(ref, "refs/remotes/origin/") {
		return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
	}
	return "main", nil
}

func (r Repository) CurrentBranch() (string, error) {
	output, err := r.combinedOutput("branch", "--show-current")
	if err != nil {
		return "", CommandError("git branch --show-current", output, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (r Repository) CheckedOutBranch() (string, error) {
	output, err := r.output("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func (r Repository) HeadAndBranch() (head, branch string, err error) {
	output, err := r.output("rev-parse", "--show-toplevel", "HEAD", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 3 {
		return "", "", fmt.Errorf("unexpected git rev-parse output")
	}
	return lines[1], lines[2], nil
}

func (r Repository) MergedBranches() (string, error) {
	output, err := r.combinedOutput("branch", "--merged", "HEAD", "--format=%(refname:short)")
	if err != nil {
		return "", CommandError("git branch --merged", output, err)
	}
	return string(output), nil
}

func (r Repository) FetchPrune() error {
	output, err := r.combinedOutput("fetch", "--prune")
	if err != nil {
		return CommandError("git fetch", output, err)
	}
	return nil
}

func (r Repository) PullRebase() error {
	output, err := r.combinedOutput("pull", "--rebase")
	if err != nil {
		return CommandError("git pull", output, err)
	}
	return nil
}

func (r Repository) StatusPorcelain() (string, error) {
	output, err := r.output("status", "-sb", "--porcelain")
	return string(output), err
}

func (r Repository) BranchExistsOnOrigin(branch string) (bool, error) {
	output, err := r.combinedOutput("ls-remote", "--heads", "origin", branch)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(output)) != "", nil
}

func (r Repository) FetchPullRequest(source string, number int, branch string) error {
	refspec := fmt.Sprintf("+refs/pull/%d/head:refs/heads/%s", number, branch)
	output, err := r.combinedOutput("fetch", "--", source, refspec)
	if err != nil {
		return CommandError("git fetch", output, err)
	}
	return nil
}

func (r Repository) AddWorktree(path, revision string) error {
	output, err := r.combinedOutput("worktree", "add", path, revision)
	if err != nil {
		return CommandError("git worktree add", output, err)
	}
	return nil
}

// CreateBranchWorktree creates a worktree at path on a new branch, basing the
// branch off the repository's current HEAD. It fails if the branch already
// exists. Use AddWorktree to check out an existing remote branch instead.
func (r Repository) CreateBranchWorktree(path, branch string) error {
	output, err := r.combinedOutput("worktree", "add", "-b", branch, path)
	if err != nil {
		return CommandError("git worktree add", output, err)
	}
	return nil
}

func (r Repository) RemoveWorktree(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	output, err := r.combinedOutput(args...)
	if err != nil {
		return CommandError("git worktree remove", output, err)
	}
	return nil
}

func (r Repository) DeleteBranch(branch string) error {
	output, err := r.combinedOutput("branch", "-D", "--", branch)
	if err != nil {
		return CommandError("git branch -D", output, err)
	}
	return nil
}

func Clone(repository, destination string) error {
	output, err := system.Command("git", "clone", "--", repository, destination).CombinedOutput()
	if err != nil {
		return CommandError("git clone", output, err)
	}
	return nil
}
