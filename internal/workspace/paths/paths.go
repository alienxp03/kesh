package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	slashOrWhitespace = regexp.MustCompile(`[\/\s]+`)
	unsafePathChars   = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	repeatedDash      = regexp.MustCompile(`-+`)
	githubRemotes     = []*regexp.Regexp{
		regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
		regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?/?$`),
		regexp.MustCompile(`^ssh://git@github\.com/([^/]+)/([^/]+?)(?:\.git)?/?$`),
	}
	githubPullURL = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/pull/([0-9]+)(?:[/?#].*)?$`)
)

func StripOriginPrefix(branch string) string {
	return strings.TrimPrefix(branch, "origin/")
}

func BranchSlug(branch string) (string, error) {
	slug := StripOriginPrefix(branch)
	slug = slashOrWhitespace.ReplaceAllString(slug, "-")
	slug = unsafePathChars.ReplaceAllString(slug, "-")
	slug = repeatedDash.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-_.")
	if slug == "" {
		return "", fmt.Errorf("branch cannot produce a non-empty path slug")
	}
	return slug, nil
}

func ParseGitHubRemote(remoteURL string) (string, bool, error) {
	value := strings.TrimSpace(remoteURL)
	for _, pattern := range githubRemotes {
		matches := pattern.FindStringSubmatch(value)
		if matches == nil {
			continue
		}
		org, err := PathSlugPart(matches[1])
		if err != nil {
			return "", false, err
		}
		repo, err := PathSlugPart(matches[2])
		if err != nil {
			return "", false, err
		}
		return filepath.Join(org, repo), true, nil
	}
	return "", false, nil
}

func ParseGitHubPullURL(pullURL string) (string, int, bool, error) {
	matches := githubPullURL.FindStringSubmatch(strings.TrimSpace(pullURL))
	if matches == nil {
		return "", 0, false, nil
	}
	org, err := PathSlugPart(matches[1])
	if err != nil {
		return "", 0, false, err
	}
	repo, err := PathSlugPart(matches[2])
	if err != nil {
		return "", 0, false, err
	}
	number, err := strconv.Atoi(matches[3])
	if err != nil {
		return "", 0, false, err
	}
	return filepath.Join(org, repo), number, true, nil
}

func RepoDirectorySlug(repoRoot string, owner string) (string, error) {
	ownerSlug, err := PathSlugPart(owner)
	if err != nil {
		return "", err
	}
	repo, err := PathSlugPart(filepath.Base(repoRoot))
	if err != nil {
		return "", err
	}
	return filepath.Join(ownerSlug, repo), nil
}

func PathSlugPart(value string) (string, error) {
	slug := strings.TrimSuffix(value, ".git")
	slug = unsafePathChars.ReplaceAllString(slug, "-")
	slug = repeatedDash.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-_.")
	if slug == "" {
		return "", fmt.Errorf("value cannot produce a non-empty path slug")
	}
	return slug, nil
}

func WorktreeHome(homeOption string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	configured := homeOption
	if configured == "" {
		configured = filepath.Join(homeDir, "workspace", "worktrees")
	}
	expanded := ExpandHome(configured, homeDir)
	return filepath.Abs(expanded)
}

func ExpandHome(input string, homeDir string) string {
	if input == "~" {
		return homeDir
	}
	if strings.HasPrefix(input, "~/") {
		return filepath.Join(homeDir, input[2:])
	}
	return input
}

func WorktreePath(worktreeHome string, repoSlug string, branchSlug string) string {
	return filepath.Join(worktreeHome, repoSlug, branchSlug)
}
