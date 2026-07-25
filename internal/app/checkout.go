package app

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/alienxp03/kesh/internal/catalog"
	gitx "github.com/alienxp03/kesh/internal/git"
	githubx "github.com/alienxp03/kesh/internal/github"
	tea "github.com/charmbracelet/bubbletea"
)

func repositoryName(repository string) (string, error) {
	repository = strings.TrimSpace(strings.TrimRight(repository, "/"))
	if strings.ContainsAny(repository, "\r\n") {
		return "", fmt.Errorf("repository cannot contain a line break")
	}
	if repository == "" {
		return "", fmt.Errorf("repository is required")
	}
	if strings.HasPrefix(repository, "-") {
		return "", fmt.Errorf("repository cannot start with a dash")
	}
	source := repository
	if strings.Contains(repository, "://") {
		parsed, err := url.Parse(repository)
		if err != nil || parsed.Host == "" || strings.Trim(parsed.Path, "/") == "" {
			return "", fmt.Errorf("could not determine a repository name from %q", repository)
		}
		source = strings.Trim(parsed.Path, "/")
	}
	separator := max(strings.LastIndex(source, "/"), strings.LastIndex(source, ":"))
	name := strings.TrimSuffix(source[separator+1:], ".git")
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("could not determine a repository name from %q", repository)
	}
	if strings.ContainsAny(name, `/\\`) {
		return "", fmt.Errorf("invalid repository name %q", name)
	}
	return name, nil
}

// parsePullRequestInput accepts a pull request reference and returns the GitHub
// owner, repository, and PR number. It recognises three shapes:
//
//   - a full URL: https://github.com/owner/repo/pull/123 (optionally with a
//     trailing path such as /files); the host may differ for self-hosted GitHub
//   - owner/repo#123
//   - a bare number (123), in which case useSelected is true and the caller
//     resolves owner/repo from the project under the cursor
//
// An empty value, a non-numeric PR, or an SSH git URL is an error.
func parsePullRequestInput(value string) (owner, repo string, number int, useSelected bool, err error) {
	reference, err := githubx.ParsePullRequestReference(value)
	if err != nil {
		return "", "", 0, false, err
	}
	return reference.Owner, reference.Repository, reference.Number, reference.UseSelected, nil
}

func resolveCloneDestination(value, root string) (string, error) {
	destination, err := expandHomePath(value)
	if err != nil {
		return "", fmt.Errorf("invalid clone destination: %w", err)
	}
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(root, destination)
	}
	return filepath.Clean(destination), nil
}

func runClone(kitty, zoxide, repository, destination string) tea.Cmd {
	return func() tea.Msg {
		repository = strings.TrimSpace(repository)
		if _, err := repositoryName(repository); err != nil {
			return cloneMsg{err: err}
		}
		if _, err := os.Stat(destination); err == nil {
			return cloneMsg{err: fmt.Errorf("clone destination already exists: %s", destination)}
		} else if !os.IsNotExist(err) {
			return cloneMsg{err: fmt.Errorf("check clone destination: %w", err)}
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return cloneMsg{err: fmt.Errorf("create clone directory: %w", err)}
		}
		if err := gitx.Clone(repository, destination); err != nil {
			return cloneMsg{err: fmt.Errorf("clone repository: %w", err)}
		}
		if err := openProjectSession(kitty, zoxide, destination, false); err != nil {
			return cloneMsg{err: err}
		}
		return cloneMsg{}
	}
}

// runCheckoutPR turns a GitHub pull request into an open workspace. It resolves
// an existing local clone (the project under the cursor, or a candidate under
// the checkout root), cloning first when none exists; fetches the PR head into a
// local branch named after the PR's head ref; creates a worktree for it; and
// opens the workspace. If a worktree on that branch already exists it is focused
// instead, so re-checking out the same PR is a no-op.
// resolvePRPreview fetches only the head branch for the input preview. Its
// value is carried with the result so Update can ignore stale lookups.
func resolvePRPreview(value, owner, repo string, number int, checkoutRoot, cloneRoot string) tea.Cmd {
	return func() tea.Msg {
		branch, _ := lookupPRHeadBranch(owner, repo, number, "")
		repoPath := filepath.Join(checkoutRoot, owner, repo)
		newClone := !dirExists(repoPath)
		if newClone {
			repoPath = filepath.Join(cloneRoot, owner, repo)
		}
		return prPreviewMsg{value: value, branch: branch, repoPath: repoPath, newClone: newClone}
	}
}

func lookupPRHeadBranch(owner, repo string, number int, dir string) (string, error) {
	gh := findCommand("gh",
		filepath.Join(os.Getenv("HOME"), ".local", "share", "mise", "shims", "gh"),
		"/opt/homebrew/bin/gh",
		"/usr/local/bin/gh",
	)
	if gh == "" {
		return "", fmt.Errorf("gh was not found")
	}
	return (githubx.Client{Executable: gh}).PullRequestHead(owner, repo, number, dir)
}

func runCheckoutPR(kitty, zoxide, owner, repo string, number int, selectedRepoPath, checkoutRoot, cloneRoot string) tea.Cmd {
	cloneURL := "https://github.com/" + owner + "/" + repo + ".git"
	return func() tea.Msg {
		matchesRepo := func(path string) bool {
			remoteOwner, remoteRepo := getRepoOwner(path)
			return strings.EqualFold(remoteOwner, owner) && strings.EqualFold(remoteRepo, repo)
		}

		// 1. Resolve an existing local clone, preferring the selected project.
		repoPath := ""
		if selectedRepoPath != "" && matchesRepo(selectedRepoPath) {
			repoPath = selectedRepoPath
		}
		if repoPath == "" {
			if candidate := filepath.Join(checkoutRoot, owner, repo); dirExists(candidate) && matchesRepo(candidate) {
				repoPath = candidate
			}
		}

		// 2. Clone when no local clone is found.
		if repoPath == "" {
			destination := filepath.Join(cloneRoot, owner, repo)
			if _, err := os.Stat(destination); err == nil {
				return prCheckoutMsg{err: fmt.Errorf("clone destination already exists: %s", displayPath(destination, os.Getenv("HOME")))}
			} else if !os.IsNotExist(err) {
				return prCheckoutMsg{err: fmt.Errorf("check clone destination: %w", err)}
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return prCheckoutMsg{err: fmt.Errorf("create clone directory: %w", err)}
			}
			if err := gitx.Clone(cloneURL, destination); err != nil {
				return prCheckoutMsg{err: fmt.Errorf("clone repository: %w", err)}
			}
			repoPath = destination
		}

		// 3. Resolve the PR's head branch via gh.
		branch, err := lookupPRHeadBranch(owner, repo, number, repoPath)
		if err != nil {
			return prCheckoutMsg{err: err}
		}
		return checkoutPRBranch(kitty, zoxide, owner, repo, number, branch, repoPath, cloneURL, matchesRepo)
	}
}

// checkoutPRBranch is the worktree-creating half of runCheckoutPR, split out so
// the head branch is the only PR detail it needs.
func checkoutPRBranch(kitty, zoxide, owner, repo string, number int, branch, repoPath, cloneURL string, matchesRepo func(string) bool) tea.Msg {
	// 4. Idempotency: a worktree on this branch already exists → focus it.
	listOutput, err := (gitx.Repository{Path: repoPath}).WorktreePorcelain()
	if err != nil {
		return prCheckoutMsg{err: err}
	}
	for _, wt := range parseWorktreePorcelain(listOutput) {
		if wt.branch == branch {
			_ = (catalog.Zoxide{Executable: zoxide}).Add(wt.path)
			return prCheckoutMsg{err: openProjectSession(kitty, zoxide, wt.path, false)}
		}
	}

	// 5. Fetch the PR head into a local branch and create a worktree for it.
	worktreeRoot, err := loadWorktreeRoot()
	if err != nil {
		return prCheckoutMsg{err: err}
	}
	wtPath := filepath.Join(worktreeRoot, owner, repo, worktreeDirectoryName(branch))
	if _, err := os.Stat(wtPath); err == nil {
		return prCheckoutMsg{err: fmt.Errorf("worktree already exists at %s", displayPath(wtPath, os.Getenv("HOME")))}
	} else if !os.IsNotExist(err) {
		return prCheckoutMsg{err: fmt.Errorf("check worktree path: %w", err)}
	}
	if err := os.MkdirAll(filepath.Dir(wtPath), 0o755); err != nil {
		return prCheckoutMsg{err: fmt.Errorf("create worktree directory: %w", err)}
	}
	// Fork-origin checkouts still resolve the PR via the canonical GitHub URL.
	fetchSource := "origin"
	if !matchesRepo(repoPath) {
		fetchSource = cloneURL
	}
	// The local PR branch may have been fetched previously and rewritten since;
	// PR refs are not guaranteed to fast-forward, so update this managed ref
	// explicitly rather than rejecting a valid re-checkout.
	repository := gitx.Repository{Path: repoPath}
	if err := repository.FetchPullRequest(fetchSource, number, branch); err != nil {
		return prCheckoutMsg{err: err}
	}
	if err := repository.AddWorktree(wtPath, branch); err != nil {
		return prCheckoutMsg{err: err}
	}

	// 6. Open the workspace.
	_ = (catalog.Zoxide{Executable: zoxide}).Add(wtPath)
	return prCheckoutMsg{err: openProjectSession(kitty, zoxide, wtPath, false)}
}

// worktreeDirectoryName keeps a PR branch in one directory. Git branch names
// commonly contain slashes (for example fix/widget), which should not create
// an accidental nested directory tree beneath the worktree root.
