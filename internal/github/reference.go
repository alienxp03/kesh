package github

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// PullRequestReference is the normalized result of user-entered PR text.
type PullRequestReference struct {
	Owner       string
	Repository  string
	Number      int
	UseSelected bool
}

// ParsePullRequestReference accepts a web URL, owner/repo#number, or a bare
// number whose repository will be resolved from the selected project.
func ParsePullRequestReference(value string) (PullRequestReference, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return PullRequestReference{}, fmt.Errorf("enter a pull request URL or number")
	}
	if strings.ContainsAny(value, "\r\n") {
		return PullRequestReference{}, fmt.Errorf("pull request reference cannot contain a line break")
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" {
			return PullRequestReference{}, fmt.Errorf("could not parse pull request URL %q", value)
		}
		segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		pullIndex := -1
		for index, segment := range segments {
			if segment == "pull" || segment == "pulls" {
				pullIndex = index
				break
			}
		}
		if pullIndex < 2 {
			return PullRequestReference{}, fmt.Errorf("could not find owner/repo/pull/<number> in %q", value)
		}
		number, err := parsePullRequestNumber(segments[pullIndex+1:])
		if err != nil {
			return PullRequestReference{}, err
		}
		return PullRequestReference{
			Owner: segments[pullIndex-2], Repository: strings.TrimSuffix(segments[pullIndex-1], ".git"), Number: number,
		}, nil
	}
	if strings.HasPrefix(value, "git@") || strings.Contains(value, "://") {
		return PullRequestReference{}, fmt.Errorf("paste the pull request's web URL, not a git URL")
	}
	if hash := strings.Index(value, "#"); hash >= 0 {
		left := strings.TrimSpace(value[:hash])
		number, err := parsePullRequestNumber([]string{strings.TrimSpace(value[hash+1:])})
		if err != nil {
			return PullRequestReference{}, err
		}
		parts := strings.Split(strings.Trim(left, "/"), "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return PullRequestReference{}, fmt.Errorf("owner/repo#<number> expected, got %q", value)
		}
		return PullRequestReference{
			Owner: parts[0], Repository: strings.TrimSuffix(parts[1], ".git"), Number: number,
		}, nil
	}
	number, err := parsePullRequestNumber([]string{value})
	if err != nil {
		return PullRequestReference{}, err
	}
	return PullRequestReference{Number: number, UseSelected: true}, nil
}

func parsePullRequestNumber(segments []string) (int, error) {
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		digits := segment
		for index, character := range segment {
			if character < '0' || character > '9' {
				digits = segment[:index]
				break
			}
		}
		number, err := strconv.Atoi(digits)
		if err != nil || number <= 0 {
			return 0, fmt.Errorf("invalid pull request number %q", segment)
		}
		return number, nil
	}
	return 0, fmt.Errorf("could not find a pull request number")
}
