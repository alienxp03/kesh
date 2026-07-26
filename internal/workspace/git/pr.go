package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alienxp03/kesh/internal/workspace/paths"
	"github.com/alienxp03/kesh/internal/workspace/run"
)

type PullRequestOptions struct {
	Value      string
	HomeOption string
	Cwd        string
	Force      bool
	Runner     run.Runner
}

type PullRequest struct {
	Number      int
	HeadRefName string
	HeadRefOID  string
	URL         string
	RepoSlug    string
}

type PullRequestTarget struct {
	Worktree     Worktree
	PullRequest  PullRequest
	FetchRef     string
	BranchExists bool
}

type pullRequestView struct {
	Number      int    `json:"number"`
	HeadRefName string `json:"headRefName"`
	HeadRefOID  string `json:"headRefOid"`
	URL         string `json:"url"`
}

type pullRequestListItem struct {
	HeadRefName string `json:"headRefName"`
	URL         string `json:"url"`
}

type mergedPullRequestListItem struct {
	Number      int    `json:"number"`
	HeadRefName string `json:"headRefName"`
	HeadRefOID  string `json:"headRefOid"`
	URL         string `json:"url"`
	MergeCommit struct {
		OID string `json:"oid"`
	} `json:"mergeCommit"`
}

func PullRequestURLsByBranch(ctx context.Context, repoRoot string, branches []string, runner run.Runner) (map[string]string, error) {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	wanted := map[string]bool{}
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch != "" {
			wanted[branch] = true
		}
	}
	if len(wanted) == 0 {
		return map[string]string{}, nil
	}

	args := []string{"pr", "list", "--state", "open", "--limit", "100", "--json", "headRefName,url"}
	result := runner.Run(ctx, "gh", args, run.Options{Cwd: repoRoot})
	if result.Err != nil || result.ExitCode != 0 {
		return nil, fmt.Errorf("gh is required for PR lookup: %s", run.FailureMessage("gh", args, result))
	}

	var items []pullRequestListItem
	if err := json.Unmarshal([]byte(result.Stdout), &items); err != nil {
		return nil, fmt.Errorf("failed to parse gh pr list output: %w", err)
	}
	urls := map[string]string{}
	for _, item := range items {
		branch := strings.TrimSpace(item.HeadRefName)
		url := strings.TrimSpace(item.URL)
		if branch == "" || url == "" || !wanted[branch] {
			continue
		}
		if _, exists := urls[branch]; !exists {
			urls[branch] = url
		}
	}
	return urls, nil
}

func PullRequestMergedForBranch(ctx context.Context, repoRoot string, branch string, runner run.Runner) (bool, error) {
	merged, _, err := pullRequestMergedForBranch(ctx, repoRoot, branch, runner)
	return merged, err
}

func pullRequestMergedForBranch(ctx context.Context, repoRoot string, branch string, runner run.Runner) (bool, string, error) {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false, "", nil
	}
	headOID, err := branchOID(ctx, repoRoot, branch, runner)
	if err != nil {
		return false, "", err
	}

	args := []string{"pr", "list", "--head", branch, "--state", "merged", "--limit", "20", "--json", "number,headRefName,headRefOid,url,mergeCommit"}
	result := runner.Run(ctx, "gh", args, run.Options{Cwd: repoRoot})
	if result.Err != nil || result.ExitCode != 0 {
		return false, "", fmt.Errorf("gh is required for merged PR lookup: %s", run.FailureMessage("gh", args, result))
	}

	var items []mergedPullRequestListItem
	if err := json.Unmarshal([]byte(result.Stdout), &items); err != nil {
		return false, "", fmt.Errorf("failed to parse gh pr list output: %w", err)
	}
	var mergedPRHeadOID string
	for _, item := range items {
		if strings.TrimSpace(item.HeadRefName) != branch {
			continue
		}
		itemHeadOID := strings.TrimSpace(item.HeadRefOID)
		if !strings.EqualFold(itemHeadOID, headOID) {
			localHeadInPR, err := commitMerged(ctx, repoRoot, headOID, itemHeadOID, runner)
			if err != nil && item.Number > 0 {
				if fetchErr := fetchMergedPullRequestHead(ctx, repoRoot, item.Number, runner); fetchErr == nil {
					localHeadInPR, err = commitMerged(ctx, repoRoot, headOID, itemHeadOID, runner)
				}
			}
			if err != nil {
				return false, "", err
			}
			if !localHeadInPR {
				if mergedPRHeadOID == "" {
					mergedPRHeadOID = itemHeadOID
				}
				continue
			}
		}
		mergeOID := strings.TrimSpace(item.MergeCommit.OID)
		if mergeOID == "" {
			continue
		}
		merged, err := commitMerged(ctx, repoRoot, mergeOID, "HEAD", runner)
		if err != nil {
			return false, "", err
		}
		if merged {
			return true, "", nil
		}
	}
	if mergedPRHeadOID != "" {
		return false, fmt.Sprintf("branch is not merged into current HEAD: %s (GitHub has a merged PR at %s, but the local branch is at %s)", branch, shortOID(mergedPRHeadOID), shortOID(headOID)), nil
	}
	return false, "", nil
}

func shortOID(oid string) string {
	oid = strings.TrimSpace(oid)
	if len(oid) <= 7 {
		return oid
	}
	return oid[:7]
}

func fetchMergedPullRequestHead(ctx context.Context, repoRoot string, number int, runner run.Runner) error {
	ref := fmt.Sprintf("refs/wktree/merged-pr/%d/head", number)
	source := fmt.Sprintf("+refs/pull/%d/head:%s", number, ref)
	args := []string{"fetch", "origin", source}
	result := runGit(ctx, runner, repoRoot, args, false)
	if result.Err != nil || result.ExitCode != 0 {
		return errors.New(run.FailureMessage("git", args, result))
	}
	return nil
}

func ResolvePullRequestWorktree(ctx context.Context, options PullRequestOptions) (PullRequestTarget, error) {
	runner := options.Runner
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	if strings.TrimSpace(options.Value) == "" {
		return PullRequestTarget{}, fmt.Errorf("pull request is required")
	}

	repoRoot, err := RepoRoot(ctx, options.Cwd, runner)
	if err != nil {
		return PullRequestTarget{}, err
	}
	pr, err := PullRequestInfo(ctx, repoRoot, options.Value, runner)
	if err != nil {
		return PullRequestTarget{}, err
	}
	if err := ensureBranchName(ctx, repoRoot, pr.HeadRefName, runner); err != nil {
		return PullRequestTarget{}, err
	}

	worktree, err := buildWorktree(ctx, repoRoot, pr.HeadRefName, options.HomeOption, runner)
	if err != nil {
		return PullRequestTarget{}, err
	}
	target := PullRequestTarget{
		Worktree:    worktree,
		PullRequest: pr,
		FetchRef:    fmt.Sprintf("refs/wktree/pr/%d/head", pr.Number),
	}

	exists, err := refExists(ctx, repoRoot, "refs/heads/"+pr.HeadRefName, runner)
	if err != nil {
		return PullRequestTarget{}, err
	}
	if !exists {
		if err := ensureTargetPathAvailable(worktree.WorktreePath); err != nil {
			return PullRequestTarget{}, err
		}
		return target, nil
	}

	worktreePath, ok, err := checkedOutWorktree(ctx, repoRoot, pr.HeadRefName, runner)
	if err != nil {
		return PullRequestTarget{}, err
	}
	if !ok && !options.Force {
		return PullRequestTarget{}, fmt.Errorf("local branch already exists but is not checked out by a wktree PR worktree, use --force to reset it: %s", pr.HeadRefName)
	}
	if ok {
		worktree.Reused = true
		worktree.WorktreePath = worktreePath
	} else if err := ensureTargetPathAvailable(worktree.WorktreePath); err != nil {
		return PullRequestTarget{}, err
	}
	target.Worktree = worktree
	target.BranchExists = true
	return target, nil
}

func PullRequestInfo(ctx context.Context, repoRoot string, value string, runner run.Runner) (PullRequest, error) {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	prValue, err := normalizePullRequestValue(value)
	if err != nil {
		return PullRequest{}, err
	}

	args := []string{"pr", "view", prValue, "--json", "number,headRefName,headRefOid,url"}
	result := runner.Run(ctx, "gh", args, run.Options{Cwd: repoRoot})
	if result.Err != nil || result.ExitCode != 0 {
		return PullRequest{}, fmt.Errorf("gh is required for --pr: %s", run.FailureMessage("gh", args, result))
	}

	var view pullRequestView
	if err := json.Unmarshal([]byte(result.Stdout), &view); err != nil {
		return PullRequest{}, fmt.Errorf("failed to parse gh pr view output: %w", err)
	}
	if view.Number <= 0 {
		return PullRequest{}, fmt.Errorf("gh pr view did not return a pull request number")
	}
	if strings.TrimSpace(view.HeadRefName) == "" {
		return PullRequest{}, fmt.Errorf("gh pr view did not return a PR head branch")
	}
	if strings.TrimSpace(view.HeadRefOID) == "" {
		return PullRequest{}, fmt.Errorf("gh pr view did not return a PR head SHA")
	}
	repoSlug, numberFromURL, ok, err := paths.ParseGitHubPullURL(view.URL)
	if err != nil {
		return PullRequest{}, err
	}
	if !ok {
		return PullRequest{}, fmt.Errorf("gh pr view returned an unsupported PR URL: %s", view.URL)
	}
	if numberFromURL != view.Number {
		return PullRequest{}, fmt.Errorf("gh pr view returned mismatched PR number %d for URL %s", view.Number, view.URL)
	}

	originSlug, err := githubOriginSlug(ctx, repoRoot, runner)
	if err != nil {
		return PullRequest{}, err
	}
	if !sameRepoSlug(originSlug, repoSlug) {
		return PullRequest{}, fmt.Errorf("PR repo %s does not match current repo %s", repoSlug, originSlug)
	}

	return PullRequest{
		Number:      view.Number,
		HeadRefName: view.HeadRefName,
		HeadRefOID:  view.HeadRefOID,
		URL:         view.URL,
		RepoSlug:    repoSlug,
	}, nil
}

func OpenPullRequestWorktree(ctx context.Context, target PullRequestTarget, runner run.Runner) (Worktree, error) {
	if runner == nil {
		runner = run.DefaultRunner{}
	}
	if err := fetchPullRequestRef(ctx, target, runner); err != nil {
		return Worktree{}, err
	}
	if target.Worktree.Reused {
		if err := resetWorktree(ctx, target.Worktree.WorktreePath, target.FetchRef, runner); err != nil {
			return Worktree{}, err
		}
		return target.Worktree, nil
	}
	if err := ensureTargetPathAvailable(target.Worktree.WorktreePath); err != nil {
		return Worktree{}, err
	}
	if err := os.MkdirAll(filepath.Dir(target.Worktree.WorktreePath), 0o755); err != nil {
		return Worktree{}, err
	}

	args := []string{"worktree", "add", "-b", target.Worktree.Branch, target.Worktree.WorktreePath, target.FetchRef}
	if target.BranchExists {
		args = []string{"worktree", "add", target.Worktree.WorktreePath, target.Worktree.Branch}
	}
	result := runGit(ctx, runner, target.Worktree.RepoRoot, args, false)
	if result.Err != nil || result.ExitCode != 0 {
		return Worktree{}, errors.New(run.FailureMessage("git", args, result))
	}
	if target.BranchExists {
		if err := resetWorktree(ctx, target.Worktree.WorktreePath, target.FetchRef, runner); err != nil {
			return Worktree{}, err
		}
	}
	return target.Worktree, nil
}

func fetchPullRequestRef(ctx context.Context, target PullRequestTarget, runner run.Runner) error {
	source := fmt.Sprintf("+refs/pull/%d/head:%s", target.PullRequest.Number, target.FetchRef)
	args := []string{"fetch", "origin", source}
	result := runGit(ctx, runner, target.Worktree.RepoRoot, args, false)
	if result.Err != nil || result.ExitCode != 0 {
		return errors.New(run.FailureMessage("git", args, result))
	}
	return nil
}

func resetWorktree(ctx context.Context, worktreePath string, ref string, runner run.Runner) error {
	args := []string{"reset", "--hard", ref}
	result := runGit(ctx, runner, worktreePath, args, false)
	if result.Err != nil || result.ExitCode != 0 {
		return errors.New(run.FailureMessage("git", args, result))
	}
	return nil
}

func githubOriginSlug(ctx context.Context, repoRoot string, runner run.Runner) (string, error) {
	remote := OriginRemoteURL(ctx, repoRoot, runner)
	slug, ok, err := paths.ParseGitHubRemote(remote)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("repo has no supported GitHub origin remote required for --pr")
	}
	return slug, nil
}

func sameRepoSlug(left string, right string) bool {
	return strings.EqualFold(filepath.ToSlash(left), filepath.ToSlash(right))
}

func normalizePullRequestValue(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "https://") || strings.HasPrefix(trimmed, "http://") {
		return trimmed, nil
	}
	number, err := strconv.Atoi(trimmed)
	if err != nil || number <= 0 {
		return "", fmt.Errorf("pull request must be a number or GitHub PR URL: %s", value)
	}
	return trimmed, nil
}
