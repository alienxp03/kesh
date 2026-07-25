// Package domain contains Kesh's pure entities and decision logic.
package domain

import "strings"

type PullRequest struct {
	Status string
	URL    string
	Number int
}

func PullRequestKey(branch, head string) string {
	return branch + "\x00" + head
}

// MatchPullRequest prefers an exact branch-and-head match. If the branch is
// known but its head changed, it returns the newest branch PR with exact=false.
func MatchPullRequest(pullRequests map[string]PullRequest, branch, head string) (PullRequest, bool) {
	if pullRequest, ok := pullRequests[PullRequestKey(branch, head)]; ok {
		return pullRequest, true
	}
	var latest PullRequest
	for key, pullRequest := range pullRequests {
		parts := strings.SplitN(key, "\x00", 2)
		if len(parts) == 2 && parts[0] == branch && pullRequest.Number > latest.Number {
			latest = pullRequest
		}
	}
	return latest, false
}
